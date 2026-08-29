# node/internal/turn

Go Node **单 session turn 编排**：一次模型请求 + 工具批处理 + 分阶段 HITL，经 `stream.Publisher` 推送 SSE。由 [`session`](../session/README.md) 的 `runtime` 按队列类型调用；**不**管理会话表或持久化。

符号索引见 [`REFERENCE.md`](./REFERENCE.md)。与 `runtime`、队列的协作见 [`../../../docs/architecture/go-node-internals.md`](../../../docs/architecture/go-node-internals.md)。

---

## 职责边界

| 本包负责 | 本包不负责 |
|----------|------------|
| `Orchestrator`：LLM 流式、tool schema、工具执行、HITL 暂停 | 消息队列消费（`session.consumeLoop`） |
| `BuildSystemPrompt`：每步 system prompt 拼接 | SQLite / 压缩触发时机（runtime 在步前调用 compression） |
| 临时 Agent 工具转交 `childagent.Manager` | 子 runtime spawn（`session.SpawnChild`） |
| 异步工具回灌 message 形态（`tool_result_messages.go`） | skills 目录配置（由 runtime 注入 `SkillAccess`） |

---

## Orchestrator 生命周期

```mermaid
sequenceDiagram
    participant RT as session.runtime
    participant O as Orchestrator
    participant LLM as llm.Client
    participant T as tools.Executor
    participant H as stream.Publisher

    RT->>O: RunHumanMessageTurn / RunToolMessageTurn
    O->>O: buildSystemPrompt(sessionID)
    O->>LLM: StreamChat(system + history)
    LLM-->>H: assistant / reasoning / usage SSE
    alt 有 tool_calls
        O->>O: processToolCalls (policy 分流)
        alt auto
            O->>T: Execute / StartBackground
            O-->>H: tool_call / tool_result SSE
            O-->>RT: ScheduleToolResult=true
        else HITL
            O-->>RT: PendingHITL
        end
    else 无 tool_calls
        O-->>H: turn_finished SSE
    end
```

**构造**：`NewOrchestrator(agentID, fsRoot, hub, client, toolExec, policy, skillAccess, maxToolLoops, promptCtx, journal, logger)`  
**事后注入**（由 `session.newRuntimeWithPublisher` 完成）：

- `SetChildAgentManager(mgr)`：父 session 注入临时 Agent 管理器
- `SetChildSession(true)`：子 session 禁止管理类工具与 `ask_user`

`policy == nil` 时加载默认策略文件；`maxToolLoops <= 0` 时用默认 **16**。超过上限时对后续 tool_calls 写入 soft `tool` 结果（见 `ToolLoopLimitExceededMessage`），不硬失败。

---

## 公开 turn API（runtime 使用）

| 方法 | 场景 |
|------|------|
| `RunHumanMessageTurn` | 追加 user 消息后**单步**模型回合；Step 序号来自 `TurnExecutionContext` |
| `RunToolMessageTurn` | history 已含 tool 结果后**单步**续跑 |
| `ContinueAfterResume` | HITL resume 写入 tool 结果并返回 inline 续跑结果 |
| `CancelPendingToolCalls` | 显式 `CancelTurn` 时闭合 pending tool calls；普通输入不调用 |

旁路 side-effect（Produce/Apply/Continue）见 [`side_effect_messages.go`](./side_effect_messages.go)、[`task_complete.go`](./task_complete.go)；session 侧 [`../session/side_effects.go`](../session/side_effects.go)。规格：[`../../../docs/design/turn-side-effects-refactor.md`](../../../docs/design/turn-side-effects-refactor.md)。

每步返回 `StepOutcome`：`Pending`、`StepIndex`、`ScheduleToolResult`、`Err`。生产调用不再传递或维护独立的 tool-loop 计数。

---

## System prompt

**文件**：[`prompt.go`](./prompt.go)  
**调用**：`orchestrator.buildSystemPrompt` → `BuildSystemPrompt(SystemPromptInput{...})`

拼接顺序（对齐 Python `get_system_prompt`）：

1. `staticSystemPrompt`（行为准则、保密说明；**不含**各工具用法，见 tool schema）
2. 工作区子目录约定（`data/`、`memory/`、`externaltools/` 外置 CLI 等；path 相对工作区根）
3. **外置 CLI 与工具**（`externaltools_menu.md` + `externaltools/` 可执行文件扫描，见 [`../externaltools/`](../externaltools/)）
4. Skills 目录元数据（仅使用 context boundary 固定的 Catalog view；实时变化由 `list_available_skills` 查询）

主机环境、Agent/session 身份与 `prompt_context` 由 request-only `ContextInjection` 注入；
已加载 skill 的完整正文由独立的 `role=user`、`source=plugin`、`form=instructions` 持久化上下文消息注入，不属于 system prompt。

启用 skills 工具组时，context boundary 会把目录元数据快照追加到 system prompt；模型也可通过 `list_available_skills` 查询最新目录，再通过 `load_skills` 选择技能。实时查询不会改写当前 system prompt。已加载 skill 正文按会话状态写入独立 context message。目录 revision 在下一个 human turn 或上下文重建边界观察，避免磁盘变化中途修改 prompt 快照。`load_skills` / `unload_skills` 会立即更新 session 和 hooks，显式变更在下一个模型 Step 创建新的 context segment，工具结果明确返回模型上下文的生效边界。压缩后下一步会按持久化 loaded 集合重新附加当前 skill 正文，并刷新目录元数据快照。

父与子 session **同一套** prompt 逻辑，暂无子专用分支。压缩摘要使用独立 system prompt，见 [`../compression/coordinator.go`](../compression/coordinator.go)。

---

## HITL 与工具分流

`processToolCalls` 按 `policy.DecideTool` 将 tool calls 分为：

- **auto**：`executeAutoBatch` 同步 `Execute`（`bash_run` 始终同步；超时直接失败，不转后台；长期任务使用 `terminal_open`）
- **HITL pending**：`ask_user_information` 与 `require_approval` 工具合并为 **`PendingHITL.Items[]`**（不再区分 `HITLKind`）
- **SSE**：本地 turn 发 **`hitl_required`**（`items[]` 每项 `hitl_type`：`user_information` \| `execute_tool`）；HITL 暂停不发终态事件。真正结束时发送 **`turn_finished`**，其 payload 含 `finish_reason`、`turn_complete=true` 与工具上下文指标。

### 工具结果状态

所有 `tool_result` SSE 都经过 `tools.ClassifyToolResult`，统一包含 `status`；失败时包含
`error.code`、`error.message`、`error.retryable`。`rejected` 仅代表策略拒绝，不能再用于
泛化判断执行失败。原始 `content` 不做全量 JSON 重包，以避免大输出额外消耗 token 并保持
历史正文兼容；终端、后台 job、浏览器、MCP、Linux SSH 等工具的专有证据仍在 `content`
中。模型继续请求时，tool history 的请求副本会在正文前增加 `[TOOL_RESULT_METADATA]`，因此模型
也能读取统一状态；hydrate/UI 仍只展示原始正文。异步 side-effect 若客户端内容已被清洗，则使用
`async_status` 覆盖事件状态。

`usage` SSE 同时携带 `prompt_cache_hit_tokens`、`prompt_cache_miss_tokens` 和
`prompt_cache_available`。后者为 false 表示 provider 没有返回 cache 指标，不能解释为
0% 命中。相同字段会写入 `StepUsage`/`TurnUsage` 以及 `model.usage.recorded` 生命周期事件，
供回放和质量评估使用。

**分步 resume**：Client 按 item 类型提交 `resume`（`type=user_information` 或 `type=approval|selection`）；Node `ContinueAfterResume` 部分消 pending，全部 resolved 后 `ScheduleToolResult`。

**中继事件路径**：A2A caller 中继（`approval_required` / `user_information_required`）、子 Agent 审批 relay（`approval_required` + `child_agent_id`）。

临时 Agent 四类工具在父 session 转 `childagent.Manager.HandleParentTool`；子 session 调用管理类工具会被拒绝。

`pending.go` 定义 `PendingHITL`、`PendingHITLItem`；`hitl_payload.go` / `approval_payload.go` 构造 SSE 展示字段。

---

## Turn 状态

| `State` | 含义 | Python `run_turn_phase` |
|---------|------|---------------------------|
| `idle` | 无活跃模型/工具步 | `idle` |
| `model_streaming` | 正在流式请求 LLM | `model_streaming` |
| `awaiting_tool` | 等待工具执行 | `awaiting_tool_execution` |

映射函数：`RunTurnPhase`。

---

## 文件一览

| 文件 | 说明 |
|------|------|
| `orchestrator.go` | `Orchestrator`、单步 LLM 循环、usage |
| `sse_publish.go` | 全部 SSE `publish*` 入口 |
| `tool_router.go` | 工具分流（policy/childagent/skills/HITL）、并行执行 |
| `cancel_partial.go` | 流式 cancel 部分 assistant 落库与 tool 补位 |
| `history_write.go` | `appendHistory` / `insertHistory` |
| `prompt.go` | `BuildSystemPrompt`、`staticSystemPrompt`、`DefaultMaxToolLoops` |
| `pending.go` | `PendingHITL`、`PendingHITLItem`、`ToolUserInterruptedMessage` |
| `hitl_payload.go` | `hitl_required` SSE 载荷 |
| `pending_resume.go` | `ContinueAfterResume` 分步消 pending |
| `step.go` | `StepOutcome`、`RuntimeToolMessageContent` |
| `side_effect_messages.go` | 旁路 Apply 计划、合并 `get_callback` batch |
| `task_complete.go` | `TaskComplete` / `TaskPhase` 判定 |
| `tool_result_messages.go` | async 消息 bundle 构建、tail 分类 |
| `approval_payload.go` | HITL 审批 SSE 载荷辅助 |
| `*_test.go` | 单测 |

---

## 相关文档

- Session 队列与父子 runtime：[`../session/README.md`](../session/README.md)
- 临时 Agent：[`../childagent/README.md`](../childagent/README.md)
- 工具执行：[`../tools/README.md`](../tools/README.md)
- Prompt 侧车：[`../promptcontext/README.md`](../promptcontext/README.md)
