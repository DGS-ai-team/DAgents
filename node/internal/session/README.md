# node/internal/session

Go Node **会话运行时**：维护 `Manager` 会话表，每个 session 一个独立 `runtime`（队列 + consumer goroutine + `turn.Orchestrator`），负责消息入队、turn 调度、持久化、压缩与临时 Agent 子 runtime 的 spawn/stop。

符号索引见 [`REFERENCE.md`](./REFERENCE.md)。总览见 [`../../../docs/architecture/go-node-internals.md`](../../../docs/architecture/go-node-internals.md)。临时 Agent 见 [`../childagent/README.md`](../childagent/README.md)；turn 编排见 [`../turn/README.md`](../turn/README.md)。

---

## 在整体架构中的位置

```mermaid
flowchart TB
    API["HTTP / CLI 入队"] --> Mgr["Manager"]
    Mgr --> RT["runtime × N"]
    RT --> Q["MessageQueue"]
    Q --> Loop["consumeLoop"]
    Loop --> Orch["turn.Orchestrator"]
    Orch --> LLM["llm.Client"]
    Orch --> Tools["tools.Executor"]
    Orch --> Hub["stream.Publisher"]
    RT --> Store["store.SQLiteStore"]
    RT --> Comp["compression.Coordinator"]
    Mgr --> CM["childagent.Manager / Host"]
    CM --> ChildRT["子 runtime（runtime_child）"]
```

| 层 | 职责 |
|----|------|
| **`Manager`** | 会话 CRUD、入队 API、skills 管理、实现 `childagent.Host` |
| **`runtime`** | 单 session 队列与执行边界：messages、loaded skills、消费队列；Turn/Step 生命周期由 `turn.Coordinator` 维护 |
| **`turn.Orchestrator`** | 每步 LLM + 工具；由 runtime 按队列类型调用 |
| **`compression`** | 仅**父** session 在 human/tool 步前可能触发摘要压缩 |
| **`childagent`** | 父 runtime 可创建临时 Agent；子 runtime 经 `RelayHub` 把 SSE 挂到父 `session_id` |

---

## 父 session 与 子 session

父、子 **共用** `newRuntimeWithPublisher` → `turn.NewOrchestrator`，差异在构造参数与事后配置：

| 项 | 父 runtime | 子 runtime |
|----|------------|------------|
| 入口 | `newRuntime`（`manager.Create`） | `newChildRuntime`（`SpawnChild`） |
| SSE `Publisher` | `*stream.Hub` | `*childagent.RelayHub`（转发到父） |
| 工具 | 完整 `*tools.Registry` | `RestrictedRegistry`（白名单） |
| SQLite | 有 | `nil`（不持久化） |
| 上下文压缩 | 有（若启用） | **跳过** |
| `SetChildAgentManager` | 注入管理器（可 `create_temporary_agent`） | 不调用 |
| `SetChildSession` | 不调用 | `true`（禁止管理类临时 Agent 工具与 `ask_user`） |
| 终态 | 用户持久化 / 恢复 | `tryCompleteChildIfIdle` → `OnChildSettled` |

建议跟读路径：`Manager.Create` → `consumeLoop` → `handleHumanMessage` / `handleToolResult` / `handleResume`；子 Agent：`SpawnChild` → `newChildRuntime` → `EnqueueChildTask`。

---

## 队列与 turn 调度

每个 `runtime` 启动 `consumeLoop`，按 `queue.Envelope.RequestType` 分流：

| RequestType | 处理函数 | 说明 |
|-------------|----------|------|
| `message` / 空 | `handleHumanMessage` | 新 user 消息；若有 pending HITL 先 `InterruptPending`；步首 Apply 缓冲 |
| `tool_result` | `handleToolResult` | 工具批执行后的续跑（`RunToolMessageTurn`） |
| `async_tool_result` | `handleSideEffectProduceAsync` | 后台 job **Produce**（SSE + 缓冲，不 inline 改 history） |
| `trigger_message` | `handleSideEffectProduceExternal` | trigger **Produce** |
| `side_effect_continue` | `handleSideEffectContinue` | 步首 Apply 缓冲 + `ContinueAfterSideEffects` |
| `resume` | `handleResume` | HITL 审批 / `ask_user_information` 恢复 |

旁路缓冲见 `side_effects.go` / `runtime_side_effects.go`：`ApplyReady` 在 `runTurnStepWithSideEffects` 步首；`ReconcileAfterStep` 在步末于 `TaskComplete` 时 schedule continue。Trigger delivery 在 **Apply 成功**时清除，不在 dequeue 时清除。

生产路径下，orchestrator 工具步结束后通过 `SetToolResultEnqueuer` 入队 `tool_result`，**单步执行 + 队列续跑**（对齐 Python 语义）。单测可用 `orchestrator_test.go` 的 `runMessageTurnInline` 内联多步。

`handleHumanMessage` / `handleToolResult` 在步前对**非子** session 调用 `compression.MaybeHandle`；步末 `persist`（子 session 的 `store` 为 nil 时 no-op）。

---

## 配置：`TurnOptions`

由 `Manager` 持有，传入 `newRuntimeWithPublisher`，影响 orchestrator 与 runtime 外围能力：

| 字段 | 作用 |
|------|------|
| `FSRoot` | 工具沙箱根目录（不在 system prompt 中暴露绝对路径；子目录约定见 prompt 与 tool schema） |
| `MaxToolLoops` | 单条 human message 内工具循环上限（子 Agent 创建时用 `SpawnSpec.MaxTurns` 覆盖） |
| `SkillsRoot` / `SkillsEnabled` / `SkillsMaxInPrompt` | skills 目录与 prompt 元数据 |
| `RuntimeDir` | `promptcontext.Reader` 根目录 |
| `CompressionSilent` / `CompressionBlocking` | 压缩阈值 |
| `IdleAutoCompressSeconds` / `IdleAutoCompressPollSeconds` / `IdleAutoCompressMinTokens` | idle 维护：无动作自动压缩 + **卸内存**（见 `idle_auto_compress.go`、`release.go`） |
| `RawMessageHistoryEnabled` / `RawMessageHistoryDir` | 原始 message journal |

---

## 文件一览

| 文件 | 说明 |
|------|------|
| `manager.go` | `Manager`、`TurnOptions`、会话 CRUD、入队、skills、上下文 API |
| `idle_auto_compress.go` | idle 维护扫描：可选压缩 + `Release` 卸内存（F-NM2–NM5） |
| `release.go` | `Manager.Release`：persist → stop → 移出 map，保留 SQLite（F-NM1） |
| `runtime.go` | `runtime` 结构体、构造、`consumeLoop`、human/tool/resume 处理、持久化 |
| `runtime_turn.go` | `runTurnStep` 单步 turn 脚手架 |
| `runtime_child.go` | `newChildRuntime`、子 session 元数据、`tryCompleteChildIfIdle` |
| `manager_child.go` | `SpawnChild` / `StopChild`、`childagent.Host`、子任务入队与 resume 路由 |
| `context_view.go` | `ContextView`（`GET /context`）、token 粗算 |
| `triggers.go` | `TriggerSubmitter`、`EnqueueTriggerMessage` |
| `*_test.go` | 单测 |

---

## 相关文档

- 临时 Agent 协议：[`docs/architecture/child-agent-tools.md`](../../../docs/architecture/child-agent-tools.md)
- System prompt 拼接：[`../turn/prompt.go`](../turn/prompt.go)
- 压缩：[`../compression/README.md`](../compression/README.md)
