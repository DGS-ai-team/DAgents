# turn 模块参考

## orchestrator.go

| 符号 | 说明 |
|------|------|
| `State` | turn 生命周期：`idle` / `model_streaming` / `awaiting_tool` |
| `StateSetter` | 阶段切换回调（session 写入 `runtime.state`） |
| `SkillAccess` | orchestrator 读写 session `loaded_skills`（Catalog + Get/Set） |
| `Orchestrator` | LLM + 工具循环 + SSE |
| `NewOrchestrator` | 构造；`policy` 默认加载、`maxToolLoops` 默认 16 |
| `SetChildAgentManager` | 父 session 注入临时 Agent 管理器 |
| `SetChildSession` | 子 session 标记；禁止管理类工具与 `ask_user` |
| `SetToolResultEnqueuer` | 工具步结束后入队 `tool_result` |
| `RunHumanMessageTurn` | 追加 user（含 `name` 来源标识）+ 单步 |
| `RunToolMessageTurn` | 单步 tool_message 续跑 |
| `ContinueAfterResume` | resume 后写 tool 结果并 `ScheduleToolResult` |
| `InterruptPending` | 用户新消息打断 pending，补 interrupted tool_result |
| `PublishSideEffectCallback` / `PublishSideEffectApplied` / `PublishSideEffectsCleared` | 旁路 Produce/Apply UX SSE |
| `publishDone` | `done` SSE；含 **`tool_context_metrics`**（WS5） |

SSE 推送统一见 `sse_publish.go`（`publishAssistant` / `publishToolCall` / `publishToolResult` / `publishError` / `publishUsage` 等）。

内部主要方法：`runOneStep`、`buildSystemPrompt`（`tool_router.go` / `cancel_partial.go` / `history_write.go`）。单测内联多步见 `orchestrator_test.go` → `runMessageTurnInline`。

## context_metrics.go（WS5）

| 符号 | 说明 |
|------|------|
| `TurnContextMetrics` | 单用户任务内工具链指标快照 |
| `recordToolCall` / `recordToolResult` / `recordToolLoop` | 编排器内埋点 |
| `logTurnContextMetrics` | 指标结构化日志（非 SSE） |
| `snapshot()` | 序列化为 `done.tool_context_metrics` |

## sse_publish.go

| 符号 | 说明 |
|------|------|
| `publishAssistant` / `publishReasoning` | 流式 delta |
| `publishError` | `error` SSE |
| `publishUserInformationRequired` / `publishApprovalRequired` | A2A 中继等仍使用的 HITL SSE |
| `publishHITLRequired` | 本地 turn 统一 HITL SSE |
| `publishToolCall` / `publishToolResult` | 工具 SSE |
| `PublishSideEffectCallback` / `PublishExternalSideEffectDeferred` | 旁路 Produce SSE |
| `PublishSideEffectApplied` / `PublishSideEffectsCleared` | Apply / ClearContext UX SSE |
| `publishDone` | `done` SSE |
| `publishUsage` / `publishUsageIfAccumulated` | `usage` SSE |

## tool_router.go

| 符号 | 说明 |
|------|------|
| `processToolCalls` | 工具分流与 HITL 暂停 |
| `executeAutoBatch` / `invokeTool` / `executeTool` | 免审批工具执行 |
| `executeSkillTool` | skills 工具与 loaded 状态写回 |
| `parseJSONArgs` / `buildUserInformationPayload` | HITL 载荷辅助 |

## cancel_partial.go

| 符号 | 说明 |
|------|------|
| `persistCancelledStream` | cancel 时保留部分 assistant |
| `appendMissingToolResponses` | 未响应 tool_call 补位 |
| `assistantMessageFromResult` | `ChatResult` → history assistant |

## history_write.go

| 符号 | 说明 |
|------|------|
| `appendHistory` / `insertHistory` | 规范化后写入 history + JSONL |

## prompt.go

| 符号 | 说明 |
|------|------|
| `staticSystemPrompt` | 固定 system 前缀（未导出常量） |
| `SystemPromptInput` | `BuildSystemPrompt` 入参 |
| `DefaultMaxToolLoops` | 工具循环默认上限（16） |
| `BuildSystemPrompt` | 拼接完整 system prompt |
| `formatWorkspaceSubdirsSection` | 工作区子目录约定；`includeHistoryJournal` 为 true 时含 `history/YYYYMMDD/` JSONL 说明 |
| `RunTurnPhase` | `State` → Python 兼容 phase 名 |

## pending.go

| 符号 | 说明 |
|------|------|
| `ToolUserInterruptedMessage` | 用户打断工具时的 tool 结果文案 |
| `PendingHITLItem` | 单条待 HITL tool call（含可选 `DuplicateMeta`） |
| `PendingHITL` | 暂停时的待处理批次（`Items[]`）；JSON 兼容旧 `kind`/`tool_calls` |
| `PendingHITL.AllToolCalls` | 用于 `InterruptPending` 的 call 列表 |

## step.go

| 符号 | 说明 |
|------|------|
| `RuntimeToolMessageContent` | tool_message 回合占位 content（`"tool_message"`） |
| `StepOutcome` | 单步结果：Pending、LoopCount、ScheduleToolResult、Err |

## tool_result_messages.go

| 符号 | 说明 |
|------|------|
| `AsyncToolResultInput` | async 旁路回灌输入（job_id、status、content 等） |
| `buildAsyncToolMessages` | async 旁路消息 bundle；经 `tool.after_each` 拆分 SSE 全文 / history 摘要 |
| `splitToolResult` | 同步 tool：调用 `RunToolAfterEach` |
| `ForClientContent` | async SSE 全文字段 |
| `classifyToolResultTail` | 判断 history 尾部形态 |
| `shouldContinueAfterAsyncTool` | Apply 后是否 schedule `side_effect_continue` |

## side_effect_messages.go

| 符号 | 说明 |
|------|------|
| `SideEffectKind` / `SideEffectMessages` | async / external 旁路 bundle |
| `BuildSideEffectMessages` | Produce/Apply 用消息预构建 |
| `ResolveSideEffectInsertSite` / `PlanSingleSideEffectApply` | 按 tail 解析 Apply 插入点 |
| `BuildMergedCallbackBatch` | ≥2 条合并为 `get_callback` tool 消息 |
| `ContinueAfterSideEffects` | 被动 LLM 续跑（`RunToolMessageTurn`） |

## task_complete.go

| 符号 | 说明 |
|------|------|
| `TaskPhase` | `open_batch` / `bridge` / `complete` |
| `TaskComplete` | 尾部形态 + pending 判定任务是否已完成（可 Apply/Continue） |

## approval_payload.go

| 符号 | 说明 |
|------|------|
| `buildApprovalToolItem` | 构造 HITL item（`execute_tool`）展示字段 |
| `describeApprovalMeta` | 按工具名生成 reason / risk 文案 |
| `firstNonEmpty` | 从 args map 取首个非空字符串字段 |
