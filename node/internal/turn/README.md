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
        O-->>H: done SSE
    end
```

**构造**：`NewOrchestrator(agentID, fsRoot, hub, client, toolExec, policy, skillAccess, maxToolLoops, promptCtx, journal, logger)`  
**事后注入**（由 `session.newRuntimeWithPublisher` 完成）：

- `SetToolResultEnqueuer`：工具步结束后入队 `tool_result`（生产必须）
- `SetChildAgentTools(mgr, isChild)`：父 `false` 可创建临时 Agent；子 `true` 禁止

`policy == nil` 时加载默认策略文件；`maxToolLoops <= 0` 时用默认 **16**。

---

## 公开 turn API（runtime 使用）

| 方法 | 场景 |
|------|------|
| `RunHumanMessageTurn` | 追加 user 消息后**单步**模型回合 |
| `RunToolMessageTurn` | history 已含 tool 结果后**单步**续跑 |
| `ContinueAfterResume` | HITL resume 写入 tool 结果并调度续跑 |
| `HandleAsyncToolResult` | 后台 job 完成，按尾部形态补 history 并可选续跑 |
| `InterruptPending` | 新 user 消息打断 pending tool calls |
| `RunMessageTurn` | 测试用：内联多步直到 pending 或结束 |

每步返回 `StepOutcome`：`Pending`、`LoopCount`、`ScheduleToolResult`、`Err`。

---

## System prompt

**文件**：[`prompt.go`](./prompt.go)  
**调用**：`orchestrator.buildSystemPrompt` → `BuildSystemPrompt(SystemPromptInput{...})`

拼接顺序（对齐 Python `get_system_prompt`）：

1. `staticSystemPrompt`（行为准则、保密说明；**不含**各工具用法，见 tool schema）
2. 主机环境快照（`hostsnapshot`）+ Agent ID / FS_ROOT / session_id
3. 工作区（FS_ROOT）目录约定（仅目录结构，不含工具名）
4. `prompt_context` 稳定段（soul / user / long_term）
5. 已加载 skills 正文（动态会话状态，非工具 catalog）
6. `custom.md`

skills **目录元数据**不再写入 system prompt；启用 `load_skills` 时注入 **`load_skills` 工具 description**（`Registry.SetSkillsCatalog`）。

父与子 session **同一套** prompt 逻辑，暂无子专用分支。压缩摘要使用独立 system prompt，见 [`../compression/coordinator.go`](../compression/coordinator.go)。

---

## HITL 与工具分流

`processToolCalls` 按 `policy.DecideTool` 将 tool calls 分为：

- **auto**：`executeAutoBatch` 同步或 `StartBackground`
- **approval**：`PendingHITL{Kind: approval}`，SSE `approval_required`
- **user_information**：`ask_user_information`，SSE `user_information_required`

临时 Agent 四类工具在父 session 转 `childagent.Manager.HandleParentTool`；子 session 调用管理类工具会被拒绝。

`pending.go` 定义 `PendingHITL`、`HITLKind`；`approval_payload.go` 构造审批展示字段。

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
| `orchestrator.go` | `Orchestrator`、单步 LLM 循环、SSE、usage |
| `tool_router.go` | 工具分流（policy/childagent/skills/HITL）、并行执行 |
| `cancel_partial.go` | 流式 cancel 部分 assistant 落库与 tool 补位 |
| `history_write.go` | `appendHistory` / `insertHistory` |
| `prompt.go` | `BuildSystemPrompt`、`staticSystemPrompt`、`DefaultMaxToolLoops` |
| `pending.go` | `PendingHITL`、`HITLKind`、`ToolUserInterruptedMessage` |
| `step.go` | `StepOutcome`、`RuntimeToolMessageContent` |
| `tool_result_messages.go` | 异步工具回灌 history 形态与截断 |
| `approval_payload.go` | HITL 审批 SSE 载荷辅助 |
| `*_test.go` | 单测 |

---

## 相关文档

- Session 队列与父子 runtime：[`../session/README.md`](../session/README.md)
- 临时 Agent：[`../childagent/README.md`](../childagent/README.md)
- 工具执行：[`../tools/README.md`](../tools/README.md)
- Prompt 侧车：[`../promptcontext/README.md`](../promptcontext/README.md)
