# turn 模块参考

## orchestrator.go

| 符号 | 说明 |
|------|------|
| `State` | turn 生命周期：`idle` / `model_streaming` / `awaiting_tool` |
| `StateSetter` | 阶段切换回调（session 写入 `runtime.state`） |
| `SkillAccess` | orchestrator 读写 session `loaded_skills`（Catalog + Get/Set） |
| `Orchestrator` | LLM + 工具循环 + SSE |
| `NewOrchestrator` | 构造；`policy` 默认加载、`maxToolLoops` 默认 16 |
| `SetChildAgentTools` | 注入临时 Agent 管理器；`isChild` 禁止管理工具 |
| `SetToolResultEnqueuer` | 工具步结束后入队 `tool_result` |
| `RunMessageTurn` | 测试：内联多步直到 pending/完成/入队 |
| `RunHumanMessageTurn` | 追加 user + 单步 |
| `RunToolMessageTurn` | 单步 tool_message 续跑 |
| `HandleAsyncToolResult` | 异步工具完成写 history 并可选续跑 |
| `ContinueAfterResume` | resume 后写 tool 结果并 `ScheduleToolResult` |
| `InterruptPending` | 用户新消息打断 pending，补 interrupted tool_result |

内部主要方法：`runOneStep`、`buildSystemPrompt`、`processToolCalls`、`executeAutoBatch`、`invokeTool`、`publishTurnIdleDone` 等。

## prompt.go

| 符号 | 说明 |
|------|------|
| `staticSystemPrompt` | 固定 system 前缀（未导出常量） |
| `SystemPromptInput` | `BuildSystemPrompt` 入参 |
| `DefaultMaxToolLoops` | 工具循环默认上限（16） |
| `BuildSystemPrompt` | 拼接完整 system prompt |
| `formatRuntimeWorkspaceSection` | `.runtime` 目录约定段落 |
| `RunTurnPhase` | `State` → Python 兼容 phase 名 |

## pending.go

| 符号 | 说明 |
|------|------|
| `ToolUserInterruptedMessage` | 用户打断工具时的 tool 结果文案 |
| `HITLKind` | `approval` / `user_information` |
| `PendingHITL` | 暂停时的待处理 tool call |
| `PendingHITL.AllToolCalls` | 用于 `InterruptPending` 的 call 列表 |

## step.go

| 符号 | 说明 |
|------|------|
| `RuntimeToolMessageContent` | tool_message 回合占位 content（`"tool_message"`） |
| `StepOutcome` | 单步结果：Pending、LoopCount、ScheduleToolResult、Err |

## tool_result_messages.go

| 符号 | 说明 |
|------|------|
| `modelContentMaxChars` | 写入模型的 tool 结果截断上限 |
| `AsyncToolResultInput` | 异步回灌输入（job_id、status、content 等） |
| `buildAsyncToolMessages` | 按尾部形态生成 assistant/tool/user 消息 |
| `classifyToolResultTail` | 判断 history 尾部形态 |
| `shouldContinueAfterAsyncTool` | 回灌后是否继续 `RunToolMessageTurn` |
| `packageToolResult` / `clipMiddle` | 工具结果打包与截断 |

## approval_payload.go

| 符号 | 说明 |
|------|------|
| `buildApprovalToolItem` | 构造 `approval_required` 中单条 tool 展示 |
| `describeApprovalMeta` | 按工具名生成 reason / risk 文案 |
| `firstNonEmpty` | 从 args map 取首个非空字符串字段 |
