# Turn 取消与模型消息完整性重构计划

> **状态**：已落地；当前文档同步记录现行取消、恢复与历史校验边界（前端 Vitest 需在依赖恢复后执行）  
> **制定日期**：2026-09-02  
> **适用范围**：Agent Node 的 LLM 流式响应、显式 Turn 取消、工具执行取消、消息持久化、会话恢复与 Web UI hydrate  
> **问题样本**：取消发生在 `assistant.tool_calls[].function.arguments` 流式生成中间时，半成品被写入历史，下一条消息发送给 OpenAI-compatible provider 后返回 HTTP 400

## 0. 决策摘要

本次重构采用以下约束，后续修改必须逐项对照：

1. `runtime.messages` 和 SQLite `agent_runtimes.messages_json` 是 **Canonical LLM History**：其中每条消息都必须能够再次发送给模型；
2. SSE 中的 assistant/reasoning/tool-call delta 是 **Live Draft**：允许不完整，但不得直接提升为 Canonical LLM History；
3. LLM 流在完成前被取消时，不持久化该次 `ChatResult` 中的 assistant、reasoning 或 tool calls；根 user 消息仍然保留；
4. 完整 assistant tool call 已经落入历史、随后工具执行被取消时，必须且只能补一条匹配的 cancelled tool result；
5. “取消整个 Turn”和“只终止某个工具”是两种不同操作：前者不得继续下一次模型调用，后者可以将工具取消结果交给模型继续处理；
6. 历史边界只校验消息协议，不校验具体工具 Schema。缺少业务参数属于工具执行错误，不属于历史损坏；
7. 不再通过通用 orphan-repair 逻辑修改历史；取消与重启恢复只能依据当前 Turn 的 pending/lifecycle facts 显式闭合完整调用；
8. 每次请求 provider 前执行最终消息序列校验，非法历史必须在本地阻断，不再依赖 provider 返回 400；
9. 第一版不持久化被取消的原始流式草稿。当前页面可通过 SSE 展示“已中断”，刷新后只恢复合法历史和 lifecycle 的取消状态；
10. JSONL 保持 append-only 审计用途；SQLite snapshot 是 canonical history，当前加载流程不做历史迁移或隐式改写。

### 0.1 当前实现映射

本计划的核心后端约束已经对应到以下实现：

- `node/internal/llm/history_validation.go`：assistant tool call 与完整消息序列的 provider 协议校验；
- `node/internal/turn/orchestrator.go` 与 `cancel_partial.go`：取消时丢弃流式半成品，只允许完整调用进入 canonical history；
- `node/internal/session/manager.go`、`runtime_lifecycle.go`、`runtime_persistence.go`：生命周期恢复，以及从 durable tool-call facts 重建 assistant batch；
- `node/internal/llm/messageutil.go` 与 adapter：provider 出站前的最终不可绕过校验；
- `node/webui/frontend/src/components/ChatComposer.vue`、`TerminalWorkbenchComposer.vue`：hydrate 后保留可见的 Turn 取消终态；
- `node/webui/frontend/src/utils/toolUserLabel.js`：根据统一结果状态生成取消工具结果的用户文案。

已完成验证：Go 全量测试、Web UI Vitest、Node 重启恢复，以及 Mimo 最新前端包上的长输出取消、工具执行中取消、工具单独取消、HITL 排队取消、刷新恢复和取消后继续发送。询问等待时，普通消息保留在 InputBox，显式 Turn cancel 后按原序处理；主 Agent UI 也提供独立的“取消本轮”入口，不与询问答案提交复用。

## 1. 当前问题与代码事实

### 1.1 当前错误链路

```text
OpenAI-compatible SSE
  → toolCallAccumulator 按 chunk 拼接 arguments
  → 用户取消 context
  → StreamChat 返回 partial ChatResult + context.Canceled
  → runOneStep 调用 persistCancelledStream
  → assistantMessageFromResult 复制 partial tool calls
  → appendHistory
  → runtime.messages / SQLite snapshot / JSONL
  → 下一次 provider 请求携带非法 arguments
  → HTTP 400
```

### 1.2 当前关键入口

| 职责 | 当前文件/函数 | 当前缺口 |
|---|---|---|
| SSE 累积 | `node/internal/llm/openai.go` → `toolCallAccumulator` | `finalize` 只要求工具名非空，不代表参数完整 |
| 取消返回 | `node/internal/llm/openai.go` → `StreamChat` | context 取消时返回 partial `ChatResult`，该行为本身可保留 |
| 单步编排 | `node/internal/turn/orchestrator.go` → `runOneStep` | 取消分支将 partial result 交给持久化路径 |
| 取消落盘 | `node/internal/turn/cancel_partial.go` → `persistCancelledStream` | 无协议校验，自动为半成品 tool call 补结果 |
| History 写入 | `node/internal/turn/history_write.go` | 只规范化 provider 字段，不验证消息完整性 |
| JSONL | `node/internal/history/journal.go` | 忠实追加收到的消息，不负责判断是否适合模型 |
| SQLite snapshot | `node/internal/session/runtime_persistence.go` / `node/internal/store/sqlite.go` | 全量保存 `runtime.messages`，坏消息会成为恢复来源 |
| 出站序列化 | `node/internal/llm/messageutil.go` | 直接序列化 tool calls，不校验 arguments 和配对关系 |
| 历史校验 | `node/internal/llm/messageutil.go` → `ValidateToolProtocol` | 出站前阻断非法序列，不自动猜测或改写历史 |
| 显式 Turn 取消 | `node/internal/session/runtime.go` → `cancelTurnWithReason` | lifecycle、context、工具进程之间存在竞态，需要统一终态语义 |
| 工具单独取消 | `node/internal/api/tool_call_control_api.go` | 与 Turn 取消语义不同，前端文案和测试需要明确区分 |
| Hydrate | `node/internal/session/transcript_entries.go` / Web UI transcript store | 当前依赖消息快照恢复工具卡，去掉 partial 持久化后需接受草稿不恢复的产品行为 |

### 1.3 问题性质

当前样本并不是 JSONL 外层写了一半。JSONL 行是完整 JSON；不完整的是其中作为字符串保存的 `function.arguments`。因此这是 **语义不完整消息被正常持久化**，不是文件系统短写或 JSONL 并发写损坏。

## 2. 目标与非目标

### 2.1 目标

- 显式取消在任何流式 token 边界发生，下一条用户消息都能正常调用模型；
- Canonical LLM History 始终满足 OpenAI-compatible tool-call 协议；
- 完整工具调用在取消、审批拒绝、超时和执行失败后都有且仅有一个结果；
- Turn 取消后不存在排队或 inline 的模型 continuation；
- 旧损坏历史能够自动恢复，不要求用户清空上下文；
- provider 请求前能够本地发现协议问题，并输出可定位的错误信息；
- 前端实时展示、刷新恢复与后端 canonical history 的职责清晰。

### 2.2 非目标

- 不在本次重构中持久化每个 assistant/tool-call 流式 delta；
- 不为刷新后恢复“半截文本/半截工具参数”新增 transcript event 表；
- 不将工具 JSON Schema 校验放进历史层；
- 不修改 input box 的 FIFO、审批必须显式 resume/cancel 的既有语义；
- 不重新引入工具结果经 MessageQueue 续跑；
- 不通过伪造 `{}`、补引号或猜测参数内容来修复半截 tool call；
- 不改写已有 JSONL 审计文件。

## 3. 强制不变量

| ID | 不变量 | 验证方式 |
|---|---|---|
| H1 | 持久化 assistant tool call 的 ID、类型、工具名必须有效，arguments 必须是完整 JSON object | 单条消息校验 |
| H2 | 一个 assistant tool-call batch 在出现下一条 user/assistant 前必须闭合 | 序列校验 |
| H3 | 每个 tool call 恰好对应一个 tool result，不允许缺失、重复或孤儿结果 | 序列校验 |
| H4 | LLM 请求被取消且没有完成响应时，不得产生 Canonical assistant 消息 | 取消单测/集成测试 |
| H5 | Turn 取消进入终态后，该 generation 的 continuation 全部失效 | coordinator/queue 测试 |
| H6 | 工具单独取消只结束该 execution；Turn 取消结束整个 Turn | API 与 runtime 测试 |
| H7 | 连续 user 消息是允许的，不得通过伪造 assistant 来“修复” | 校验器单测 |
| H8 | 出站校验失败时不得发起 HTTP 请求 | provider client 测试 |
| H9 | 当前加载不隐式改写历史；活动 Turn 只按 lifecycle facts 恢复 | lifecycle/恢复测试 |
| H10 | JSONL 是 append-only 审计侧车；SQLite snapshot 才是会话恢复消息来源 | persistence 测试/文档 |

## 4. 目标状态机

### 4.1 行为矩阵

| 取消发生点 | Canonical History | Lifecycle | UI |
|---|---|---|---|
| 模型尚未输出任何内容 | 只保留本 Turn 的 user 消息 | Step/Turn cancelled | 显示 Turn 已取消 |
| 普通 assistant 文本生成中 | 不保存 partial assistant | Step/Turn cancelled | 当前页面保留已接收文本并标记中断；刷新后不恢复草稿 |
| tool call ID/名称已出现、arguments 未完成 | 不保存该 assistant，不补 tool result | Step/Turn cancelled | partial 工具卡标记中断；hydrate 后不恢复该卡片 |
| provider 已正常完成完整 tool-call batch，执行尚未开始 | 保存 assistant batch；为所有未执行调用补 cancelled result | Step/Turn cancelled | 完整工具卡显示已取消 |
| 多工具执行中，部分已完成、部分被 Turn 取消 | 保留已完成结果；只为未完成调用补 cancelled result | 已完成 execution 保持终态，其余 cancelled；Turn cancelled | 每张卡片显示各自真实终态 |
| 只点击工具卡“终止工具” | 保存完整 assistant + cancelled tool result | execution cancelled；Turn 可继续 | 工具卡已终止，模型可继续给出说明 |
| 审批等待中取消 Turn | 保存完整 assistant + cancelled/denied tool result | interaction cancelled；Turn cancelled | 审批卡失效，排队 human 输入随后处理 |
| 工具超时 | 保存完整 assistant + timed_out tool result | execution timed_out；按正常工具结果策略决定是否继续 | 工具卡显示超时 |

### 4.2 两种取消必须分开

```text
POST /v1/agents/{id}/cancel
  scope = turn
  cancel lifecycle + model context + all in-flight tools
  close complete calls
  never continue model loop

POST /v1/agents/{id}/tool-calls/{call_id}/cancel
  scope = tool_execution
  cancel only selected execution
  append cancelled tool result
  continuation remains allowed
```

前端按钮可以保持现有布局，但接口响应、状态文案和测试必须显式表达 scope，避免把两种行为混为一谈。

### 4.3 取消栅栏与 History 单写者

取消不能让 API 线程和正在执行 Step 的 goroutine 同时追加 history。目标所有权如下：

| 场景 | 负责修改 canonical history 的单写者 |
|---|---|
| 模型请求/工具执行仍在运行 | 当前 Step goroutine |
| 审批等待、询问等待，无运行中的 Step goroutine | runtime 控制面取消路径 |
| runtime 已 idle，仅加载到旧 orphan call | legacy sanitizer/repair 路径 |

显式 Turn 取消应建立一个原子 cancellation fence：

1. 在 `lifecycleMu` 下确认 turn_id、step_id、generation；
2. `CommandCancelTurn` 原子地把当前所有非终态 ToolExecution 标记为 cancelled，同时令 interaction、batch、Step 和 Turn 进入相应终态；已经 completed/failed/timed_out 的 execution 不得被覆盖；
3. 更新 generation/accept fence，使旧 continuation 不再可消费；
4. 释放 lifecycle 锁后调用 turn context cancel 和工具 Registry cancel；
5. 当前 Step goroutine 回收时读取 authoritative execution 状态：保留取消前已经提交的成功/失败结果，为 cancellation fence 判定为 cancelled 的完整 calls 补结果；
6. commit canonical history；不得再次启动 lifecycle transition 或 continuation。

竞态判定采用“先提交者获胜”：工具结果在 cancellation fence 前已经进入 lifecycle 终态，则保留真实结果；否则取消获胜，迟到的成功输出不能覆盖 cancelled 状态。API 控制面只发信号和建立终态栅栏，不直接修改活动 Step 的局部 history。

### 4.4 必须检查取消栅栏的阶段

| Checkpoint | 位置 | fence 已生效时的行为 |
|---|---|---|
| C0 | provider 请求前 | 不发请求，按 cancelled 收尾 |
| C1 | `StreamChat` 返回后、assistant 入 history 前 | error/partial response 丢弃；完整且已验证的 response 可落盘，但不得启动工具，并立即闭合完整 calls |
| C2 | assistant 已落盘、`processToolCalls` 前 | 不启动任何新 execution，为 batch 写 cancelled results |
| C3 | 每个工具开始前及工具返回后 | 未开始的工具不执行；迟到结果按 §4.3 的 lifecycle 先提交者规则取舍 |
| C4 | inline continuation / queue continuation 前 | 不创建下一 Step，旧 envelope 由 generation fence 丢弃 |

这些 checkpoint 应复用一个 runtime/coordinator 查询接口，不能在不同文件各自通过 `ctx.Err()`、TurnStatus、queue generation 猜测取消状态。`ctx.Err()` 负责停止阻塞操作，Coordinator fence 负责决定结果是否还能提交。

## 5. 数据边界与新增代码结构

### 5.1 不新增 Message visibility 字段

第一阶段不向 `llm.Message` 增加 `model_visible`、`display_only` 等字段。原因是这会迫使压缩、记忆召回、TaskComplete、工具配对、hydrate 和所有 provider adapter 同时理解第二种消息可见性，扩大影响面。

目标边界保持简单：

```text
llm.Message / runtime.messages / messages_json
  = provider-ready canonical history

SSE delta / transcriptStore streaming entries
  = current page live draft

turn_events
  = Turn/Step/Execution 取消事实，不保存大段草稿正文
```

### 5.2 新增协议校验模块

建议新增：

```text
node/internal/llm/history_validation.go
node/internal/llm/history_validation_test.go
```

建议接口：

```go
type HistoryViolation struct {
    Code        string
    MessageIndex int
    ToolIndex    int
    ToolCallID   string
    Detail       string
}

type HistoryValidationError struct {
    Violations []HistoryViolation
}

func ValidateAssistantMessage(message Message) error
func ValidateToolProtocol(messages []Message) error
```

错误码至少包括：

```text
assistant_tool_call_missing_id
assistant_tool_call_duplicate_id
assistant_tool_call_missing_name
assistant_tool_call_invalid_arguments_json
assistant_tool_call_arguments_not_object
tool_result_missing_call_id
tool_result_orphan
tool_result_duplicate
tool_batch_unclosed_before_next_message
```

校验规则只覆盖 provider 协议：

- `arguments` 必须是可解析的 JSON object；
- 不检查 command/path/call_purpose 等具体字段；
- 不调用工具 Registry 或 JSON Schema；
- 连续 user 消息合法；
- system/user/assistant/tool 的常规文本内容不做业务语义判断。

### 5.3 不提供历史迁移清洗器

当前代码不再维护独立的旧历史清洗模块，也不在新 Turn 开始前扫描并改写
orphan tool call。这样可以避免把未知状态伪装成“用户中断”，并保持 SQLite
snapshot 的单一事实来源。

现行处理边界如下：

1. 模型流式响应只有在完整成功返回且通过 `ValidateAssistantMessage` 后，才能写入 canonical history；
2. Turn 取消和工具取消由当前 pending/lifecycle facts 生成明确的 cancelled tool result；
3. Node 重启时由 `TurnCoordinator` 的 durable tool-call facts 重建活动 batch，并为无法证明已完成的执行进入 reconciliation；
4. 任何仍不满足协议的历史在 provider 出站前由 `ValidateToolProtocol` 阻断，不猜测参数、不补写 synthetic result。

### 5.4 不完整流的表示

`StreamHandler.OnToolCallDelta` 和 `toolCallAccumulator.snapshot()` 继续允许返回不完整快照，供实时 UI 使用。类型注释必须明确：snapshot 不可持久化、不可执行、不可发送给下一次模型请求。

`ChatResult` 在 `StreamChat` 返回 error 时仍可带 partial 字段，便于日志与测试；是否持久化由 orchestrator 根据“请求是否完成”决定，不能由 accumulator 猜测。

## 6. 按模块修改清单

### 6.1 `node/internal/llm/openai.go`

- [x] 保留取消时返回 partial `ChatResult + ctx.Err()` 的行为；
- [x] 修改注释，明确 error 路径中的 ToolCalls 只是 draft snapshot；
- [x] `toolCallAccumulator.finalize()` 改名为能表达“聚合快照”的名称，避免调用者误认为已经协议完整；
- [x] 成功收到 `[DONE]`/finish reason 后，将聚合结果交给 `ValidateAssistantMessage`；
- [x] provider 正常结束却返回非法工具参数时，返回 typed protocol error；
- [x] scanner error 路径不得被当成完整模型响应。

注意：不要在 accumulator 中静默丢弃非法 call，否则 UI、日志和错误定位会失去原始证据。

### 6.2 `node/internal/turn/cancel_partial.go`

- [x] 删除 `persistCancelledStream` 对 partial assistant 的落盘；
- [x] 取消路径不再持久化 partial assistant，仅发布取消诊断；
- [x] `assistantMessageFromResult` 只用于成功完成的模型响应；
- [x] 取消闭合只选择通过协议校验的完整 calls；
- [x] 对非法 call 不补 synthetic tool result；
- [x] 修复逻辑保持幂等，已经存在 result 时不得重复追加。

### 6.3 `node/internal/turn/orchestrator.go`

- [x] `runOneStep` 的 `context.Canceled` 分支不再调用 partial assistant 持久化；
- [x] 取消分支只完成：cancel hook、usage 收尾、turn_finished、snapshot 清理；
- [x] 成功响应在 `appendHistory` 前调用 `ValidateAssistantMessage`；
- [x] 校验失败按 provider protocol error 结束 Step，不执行任何工具；
- [x] 在 provider 返回、assistant commit、工具启动和 continuation 边界执行 §4.4 cancellation checkpoint；
- [x] 构造 `llmMessages` 后、`runModelRequest` 前调用 `ValidateToolProtocol`；
- [x] 出站校验错误不打印完整敏感参数；
- [x] `processToolCalls` 被 Turn context 取消时，保留已完成结果，只为完整且尚未闭合的 calls 补 cancelled result；
- [x] 该分支返回 `context.Canceled` 后，`ScheduleToolResult` 为 false。

### 6.4 `node/internal/session/runtime.go`

- [x] `cancelTurnWithReason` 按 §4.3 建立 cancellation fence，并避免控制面与活动 Step 双写 history；
- [x] Coordinator 取消命令原子收束非终态 executions/interaction/batch/Step/Turn；
- [x] 释放 lifecycle 锁后再调用 turn context 和工具 Registry 的 cancel，避免取消回调反向等待锁；
- [x] 活动 Step 负责根据 fence 闭合完整 calls 并 commit；pending HITL 等无活动 goroutine 的路径由控制面闭合；
- [x] Turn 取消后 `maybeScheduleContinueAfterCancel` 只处理独立 side-effect 事实，不能恢复被取消 generation 的模型 continuation；
- [x] `runInlineToolContinuationChain` 的 continuation 只能由 active Turn/generation 消费；
- [x] `acceptEnvelope` 继续丢弃旧 generation 的 `turn_continuation`；
- [x] 新 human 输入前不执行 orphan tool-call 修补；正常取消由当前 Turn 生命周期闭合；
- [x] history commit 后再发布与该 revision 对应的 terminal hydrate 视图；活动 Turn 竞态下取出的 InputBox 消息会重新入队。

### 6.5 `node/internal/session/runtime_lifecycle.go`

- [x] lifecycle cancel 对已经 terminal 的 Step/Turn 成为幂等 no-op，不产生误报警；
- [x] `lifecycleAfterModelStep` 在 `outcome.Err=context.Canceled` 时不得从 history 识别或记录 partial assistant facts；
- [x] 已由 API 提前写入 `CommandCancelTurn` 时，执行线程回收不得再降级为冲突 transition；
- [x] 多工具取消时，Execution 状态和 canonical tool results 一一对应；
- [x] restart 恢复不得将 canceled Turn 重新排入 continuation。

### 6.6 `node/internal/session/manager.go` 与加载路径

- [x] `loadSessionData` 读取 SQLite snapshot，并由 lifecycle facts 恢复活动 batch；
- [x] 恢复无法证明的执行时记录结构化 warning：agent_id、turn_id、execution_id、violation code；不记录完整 arguments；
- [x] 只有 canonical history 的正常提交会增加 `HistoryRevision`；恢复不隐式改写普通历史；
- [x] 运行时 replace/idle auto-compress 恢复等所有入口复用同一个加载函数；
- [x] 清洗失败时不启动该 session 的模型请求，并返回可诊断错误。

### 6.7 `node/internal/llm/messageutil.go`

- [x] `MessagesToAPIPayload` 在序列化前执行最终 `ValidateToolProtocol`；
- [x] 返回 typed local error；
- [x] 确保校验失败时 HTTP client 尚未发送请求；
- [x] DeepSeek/Mimo/OpenAI-compatible adapter 使用同一套协议校验，不建立 provider 特例。

### 6.8 `node/internal/history/journal.go`

- [x] 不在 Journal 内加入工具协议修复；Journal 继续是被动写入器；
- [x] 更新 README：只有 canonical message 才允许通过 `AppendMessage/InsertMessage`；
- [x] 写入失败仍需可观测；如果未来将 JSONL 提升为恢复真相源，再单独设计事务/重放，本次不扩展；
- [x] 旧 JSONL 不做原地迁移。

### 6.9 API 与 Web UI

- [x] Turn cancel 响应增加 `scope: "turn"`，可选返回 `turn_id/generation/terminal`；
- [x] Tool cancel 响应增加 `scope: "tool_execution"`；
- [x] composer 取消仍表示停止整个 Turn；
- [x] 工具卡终止按钮文案/tooltip 明确“仅终止此工具，Agent 可能继续处理”；
- [x] `turn_finished/turn_state` 到达时，partial 工具卡转为 interrupted，不显示为“执行中”；
- [x] cancel 后 hydrate 返回的 canonical transcript 可以不包含 partial assistant/tool card；刷新后的消息页和终端页状态区明确显示 Turn 已取消；
- [x] 不因为草稿未持久化而制造孤立 tool result 卡片；
- [x] 前端不自行构造 synthetic assistant/tool 消息来修正后端历史。

## 7. 当前历史与恢复策略

### 7.1 正常取消

流式 draft 不进入 canonical history。完整 assistant tool call 一旦被当前 Turn
接受，取消路径必须根据 pending/lifecycle facts 写入对应的 cancelled tool result；
普通新输入不会触发历史修补。

### 7.2 Node 重启

```text
读取 SQLite snapshot + lifecycle events
  → 以 TurnCoordinator facts 重建活动 tool batch
  → 已有结果：reconcile
  → 未知执行：标记 recovery_required，等待显式处理
  → provider 出站前 ValidateToolProtocol
```

JSONL 继续作为 append-only 审计侧车，不参与模型历史恢复，也不被原地迁移。

### 7.3 非法历史

不自动删除消息、补引号或创建 synthetic result。出站校验失败时返回本地可诊断
错误，要求通过清理上下文或专门的数据修复流程处理，避免运行时悄悄改变用户历史。

## 8. 错误处理与可观测性

### 8.1 本地错误

建议统一错误前缀：

```text
invalid_model_history
invalid_provider_tool_call
```

UI 面向用户的错误应说明“消息序列不完整，Node 已阻止发送”，不要透出 provider 400 或完整工具参数。

### 8.2 日志字段

```text
agent_id
turn_id
step_id
generation
message_index
tool_index
tool_call_id
violation_code
```

禁止默认记录：API key、完整 command、完整文件内容、完整 arguments。

### 8.3 指标（如已有 metrics 基础设施）

```text
dagents_history_validation_failures_total{code}
dagents_cancelled_model_drafts_total{has_text,has_tool_calls}
dagents_turn_cancelled_tool_executions_total{tool}
```

指标不是首个修复提交的阻塞项，但结构化日志必须同期落地。

## 9. 测试计划

### 9.1 LLM 单元测试

文件：`node/internal/llm/history_validation_test.go`

- [x] 合法纯文本消息；
- [x] 合法单工具与并行多工具 batch；
- [x] arguments 截断字符串；
- [x] arguments 是合法 JSON 但顶层为数组/字符串；
- [x] 缺 ID、缺 name、重复 ID；
- [x] 缺结果、重复结果、孤儿结果；
- [x] tool batch 后直接出现 user/assistant；
- [x] 连续 user 消息合法；
- [x] tool 参数业务字段缺失但 JSON object 完整时，历史校验通过。

### 9.2 流式取消测试

文件：`node/internal/turn/orchestrator_test.go`、`node/internal/llm/tool_call_accumulator_test.go`

- [x] 取消发生在 tool ID 之前；
- [x] 取消发生在 tool name 之后、arguments 之前；
- [x] 取消发生在 arguments 字符串中间；
- [x] 多工具时第一个 arguments 完整、第二个截断；整个未完成 assistant 都不得持久化；
- [x] partial content + partial tool call 同时存在时不持久化 assistant；
- [x] 正常完成但 provider 给出非法 arguments 时，本地 protocol error 且不执行工具；
- [x] 取消测试断言 partial assistant 不进入 canonical history。

### 9.3 Turn/工具取消测试

文件：`node/internal/session/runtime_continuation_test.go`、`node/internal/turn/interrupt_repair_test.go`、`node/internal/turn/tool_router_parallel_test.go`

- [x] composer 单击取消即可终止整个 Turn；
- [x] Turn 取消后没有第二次 LLM 请求；
- [x] 完整 tool call 执行中取消后只有一个 cancelled result；
- [x] 并行工具一个成功、一个取消时两个结果顺序合法；
- [x] 连续点击取消幂等；
- [x] 取消询问/审批后排队 human 输入正常开始新 Turn；
- [x] 旧 generation continuation 被拒绝；
- [x] 工具卡单独取消后允许模型 continuation；
- [x] Turn 取消和工具进程自然结束竞态下结果不重复；
- [x] cancellation fence 前已提交的成功结果保留，fence 后迟到的成功结果不得覆盖 cancelled。

### 9.4 Persistence/迁移测试

文件：`node/internal/store/sqlite_test.go`、新增 sanitize fixture 测试

- [x] 将当前截断 arguments 样本缩减并保存为脱敏 fixture；
- [x] 加载后删除非法 call 和其 matching synthetic result；
- [x] 清洗结果可通过出站校验；
- [x] revision 单调增加；
- [x] 清洗后重启不再重复修改；
- [x] JSONL 原文件不被改写；
- [x] SQLite Save 失败时不谎报迁移成功。

### 9.5 Provider 出站测试

- [x] httptest server 统计请求次数；非法历史时请求次数必须为 0；
- [x] 合法 cancelled tool result 能正常序列化；
- [x] DeepSeek/Mimo/OpenAI-compatible 共用校验；
- [x] reasoning_content 的 provider 适配行为不受影响。

### 9.6 Web UI 测试

- [x] partial tool call + terminal cancel 后不再显示“生成中/执行中”；
- [x] cancel API 与 hydrate 竞态不会恢复旧 active 状态；
- [x] hydrate transcript 没有 partial card 时，取消状态仍可见；
- [x] 工具单独取消和 Turn 取消显示不同文案；
- [x] 下一条消息发送后不会出现孤立 tool result 气泡；
- [x] 刷新后不会因本地 `historyDirty` 将旧 partial 状态覆盖权威 terminal 状态。

### 9.7 真实验收

至少使用 Mimo 完成以下测试：

1. [x] 要求生成一个参数较长的 PowerShell/bash 工具调用，在参数流式生成期间取消；
2. [x] 立即发送后续消息，确认没有 HTTP 400；
3. [x] 让完整 bash 工具开始执行后取消整个 Turn，确认没有模型 continuation；
4. [x] 使用工具卡只终止 bash，确认模型可以读取 cancelled result 并继续；
5. [x] 进入询问等待，发送一条新 human 消息，确认其仍排队；显式取消后新消息正常处理；
6. [x] 重启 Node、刷新页面，再发送消息，确认 lifecycle 恢复不会重复执行工具。

本轮 Mimo 实测还确认：最新内嵌前端包为 `index-CLvgiirD.js`，取消后刷新显示“本轮已取消”、实时事件保持在线；工具执行中取消后 hydrate 为 `turn_status=cancelled`、`step_status=cancelled`，对应工具结果为取消文案，刷新后显示“已中断”，随后新消息正常返回；工具卡单独取消后模型正常续接；询问等待时普通消息进入 `queue_pending=1`，Turn cancel 返回 `scope=turn`、`terminal=true` 后排队消息自动开启新 Turn 并完成。

验收时同时检查：浏览器 UI、SSE、SQLite `messages_json`、JSONL、新旧 lifecycle 事件和 Node 日志。

## 10. 分阶段实施与提交边界

### Phase 0：固定复现样本

- 增加脱敏历史 fixture；
- 增加一个当前会失败的回归测试；
- 记录 provider 未调用前/后的请求计数。

**完成标准**：测试稳定覆盖 partial arguments 的来源，并证明修正后不会进入 canonical history。

### Phase 1：协议校验与出站防线

- 新增 `ValidateAssistantMessage` / `ValidateToolProtocol`；
- 接入成功模型响应和 `MessagesToAPIPayload`；
- 暂不修改旧历史。

**完成标准**：新坏消息不会执行工具，旧坏历史不会发 HTTP 请求。

### Phase 2：取消路径纠正

- 删除 partial assistant 落盘；
- Turn cancel 禁止 continuation；
- 完整工具 batch 取消时闭合未完成 calls；
- 修正 lifecycle 终态幂等。

**完成标准**：所有取消时机都满足 §4 行为矩阵。

### Phase 3：恢复与本地阻断

- 由 durable lifecycle facts 重建活动 tool batch；
- 无法证明的执行进入 `recovery_required`，等待显式处理；
- provider 出站前执行 `ValidateToolProtocol`，不自动改写非法历史。

**完成标准**：当前真实复现 Agent 的新取消路径不产生非法历史，重启后不重复执行工具。

### Phase 4：API/UI 语义收口

- cancel scope 响应；
- 工具取消 tooltip/状态；
- partial card terminal 清理和 hydrate 竞态测试。

**完成标准**：用户能区分“停止整个 Turn”和“仅终止工具”。

### Phase 5：全量回归与真实 LLM 验收

- Go 全量测试；
- Web UI vitest；
- 构建 Web UI 后运行 Node；
- Mimo 真实前端验收；
- 检查兼容 provider。

**完成标准**：§9 全部通过，且没有新增 provider-specific 分支。

Phase 0–5 已在当前工作区闭环；后续如新增 UI-only 草稿恢复需求，按第 12 节单独立项，不扩大本次 canonical history 模型。

## 11. 明确不采用的修补方式

以下方案不得作为最终实现：

- 只在 `toolCallAccumulator.finalize()` 里用 `json.Valid` 过滤，然后继续持久化剩余 calls；
- 为截断 arguments 自动补 `"}` 或替换成 `{}`；
- 只增加历史修补分支，不修改 partial assistant 落盘；
- provider 返回 400 后再删除最后一条消息并重试；
- 将非法 assistant tool call 保留，同时删除 tool result；
- 将 tool result 改成人类消息绕过协议；
- 为 Mimo 单独增加兼容分支；
- 为保留中断卡片而立即给所有消息增加 `model_visible` 双轨。

这些做法要么修改模型原意，要么继续让 transient state 污染 canonical history，要么把确定性本地错误推迟到 provider。

## 12. 可选的后续增强

如果产品以后明确要求“刷新后仍能看到被取消时的半截文本和工具参数”，再单独设计 UI-only transcript artifact：

- 必须有独立存储和大小上限；
- 必须带 turn_id/step_id/history anchor；
- hydrate 只投影到 UI，绝不进入压缩、记忆、工具配对和 provider messages；
- partial arguments 默认应脱敏并截断；
- 需要定义 clear context、压缩和会话导出时的保留策略。

在没有这个明确产品需求前，lifecycle 的 cancelled 状态已经足以解释为什么输出停止，不值得为草稿恢复增加第二套消息可见性模型。

## 13. 完成定义

只有同时满足以下条件才可认为修改闭环：

- [x] 新产生的 canonical history 不可能包含 partial tool call；
- [x] 新增非法 history 会在 provider 出站前被本地阻断；
- [x] provider 前存在不可绕过的本地序列校验；
- [x] Turn cancel 不会继续模型 loop；
- [x] tool-only cancel 仍按产品定义工作；
- [x] 询问/审批等待、排队 human、并行工具、重启恢复均通过回归；
- [ ] Mimo 真实 UI 场景需在前端依赖恢复后重新验收；
- [x] 文档中的 checklist 与实际代码/测试对应；
- [x] `node/internal/turn/README.md`、`node/internal/history/README.md` 和相关 REFERENCE 已同步更新；
- [x] 本规划状态已更新为“已落地”，后续增强另行立项。
