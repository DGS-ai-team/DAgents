# session 模块参考

## manager.go

| 符号 | 说明 |
|------|------|
| `TurnOptions` | turn 编排配置（FS_ROOT、MaxToolLoops、skills、压缩、journal） |
| `Manager` | 会话表；每 session 独立 runtime + 队列 |
| `NewManager` | 绑定 agent、Hub、LLM、Registry、policy、store、TurnOptions |
| `SetChildAgentManager` | 注入 `childagent.Manager` 并 `BindHost` |
| `SetTriggerDeliveryTracker` | trigger 出队后清除 pending delivery |
| `Stop` | 取消 Manager 上下文并停止所有 runtime |
| `Create` | 创建或恢复 session；`newRuntime` + `attachUserChildTools` + `start` |
| `Get` / `ListActive` / `ListPersisted` | 查询活跃或持久化会话 |
| `RuntimeInfo` | 队列深度、是否有活跃 turn、turn 状态 |
| `GetContextView` / `ContextSummary` | 上下文聚合视图 |
| `LoadedSkills` / `ListSessionSkills` / `LoadSessionSkill` / `UnloadSessionSkill` | skills 读写 |
| `ClearContext` / `Delete` | 清空消息或删除 session |
| `EnqueueMessage` | user / resume / 高优先级消息入队 |
| `RunInboxConsultation` | A2A inbox：单 Task 跑完整 turn（订阅 SSE 聚合 assistant） |
| `EnqueueAsyncToolResult` / `EnqueueToolResult` / `EnqueueBackgroundToolResult` | 工具续跑与后台回灌 |
| `CancelTurn` | 取消当前 turn 上下文 |
| `attachUserChildTools` | 父 runtime 上 `SetChildAgentManager(mgr)` |

## manager_child.go

| 符号 | 说明 |
|------|------|
| `ParentSessionActive` | `Host`：父 session 存在且非子 session |
| `SpawnChild` | 创建子 runtime 并 `start` |
| `StopChild` | 停止子 runtime consumer |
| `EnqueueChildTask` | 向子 session 入队首条任务（human message） |
| `ChildHasPendingHITL` / `ParentHasPendingHITL` | HITL 状态查询 |
| `DeliverChildResume` / `DeliverParentResume` | resume 路由到对应 session 队列 |
| `ListActiveUser` | 活跃且非子 session 的列表 |
| `ListChildAgents` / `CancelChildAgent` | 父下子 Agent 视图与取消 |
| `ChildAgentView` | 子 Agent 对外展示字段 |

## runtime.go

| 符号 | 说明 |
|------|------|
| `Session` | 对外可见的 `ID` + `AgentID` |
| `runtime` | 单 session 内部状态（未导出） |
| `newRuntime` | 父 session 构造入口 → `newRuntimeWithPublisher` |
| `newRuntimeWithPublisher` | 创建 `runtime` + `turn.NewOrchestrator` + `SetToolResultEnqueuer` |
| `start` / `consumeLoop` | 启动队列消费 goroutine |
| `handleHumanMessage` | human_message 单步 + 可选入队 tool_result |
| `handleToolResult` | tool_message 单步续跑 |
| `handleAsyncToolResult` | 异步工具完成回灌 |
| `handleResume` | HITL resume 续跑 |
| `applyStepOutcome` | 同步 messages / pending / toolLoopCount |
| `persist` / `clearMessages` | SQLite 持久化 |
| `enqueue` | 带优先级入队；高优先级可打断当前 turn |
| `cancelTurn` / `stop` | 取消或停止 consumer |
| `contextView` | 组装 `ContextView` |
| `runTurnStep` / `finishTurnIdle` | 单步 turn 脚手架；`finishTurnIdle` 在 `applyStepOutcome` 后触发子 Agent 结算 |
| `getLoadedSkills` / `setLoadedSkills` / `setLoadedSkillsByName` 等 | skills 内存状态 |

## runtime_child.go

| 符号 | 说明 |
|------|------|
| `newChildRuntime` | 子 session：`RelayHub` + `RestrictedRegistry` + `SetChildSession(true)` |
| `childRuntimeMeta` | 父 ID、childMgr、completing 标志 |
| `isChildSession` | 是否子 runtime |
| `tryCompleteChildIfIdle` | 队列空且无 pending 时通知 `OnChildSettled` |

## context_view.go

| 符号 | 说明 |
|------|------|
| `ContextView` | GET context 响应结构 |
| `estimateMessageTokens` | messages token 粗算 |
| `pendingToolCallsCount` | 从 `PendingHITL` 统计待处理 tool call 数 |

## triggers.go

| 符号 | 说明 |
|------|------|
| `TriggerSubmitter` | trigger 模块用的 session 创建与入队适配器 |
| `EnsureSession` | `Manager.Create` 包装 |
| `SubmitTriggerMessage` | 入队 trigger 渲染后的 user 消息 |
| `EnqueueTriggerMessage` | 校验 content → EnsureSession → `PriorityOther` 入队 |
