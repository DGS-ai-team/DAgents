# session 模块参考

## manager.go

| 符号 | 说明 |
|------|------|
| `TurnOptions` | turn 编排配置（FS_ROOT、MaxToolLoops、skills、压缩、journal） |
| `Manager` | 会话表；每 session 独立 runtime + 队列 |
| `NewManager` | 绑定 agent、Hub、LLM、Registry、policy、store、TurnOptions |
| `SetChildAgentManager` | 注入 `childagent.Manager` 并 `BindHost` |
| `SetTriggerDeliveryTracker` | trigger Apply/ClearSession 时清除 pending delivery |
| `Stop` | 取消 Manager 上下文并停止所有 runtime |
| `Create` | 创建或恢复 session；`newRuntime` + `attachUserChildTools` + `start` |
| `Get` / `ListActive` / `ListPersisted` | 查询活跃或持久化会话 |
| `RuntimeInfo` | 队列深度、是否有活跃 turn、turn 状态 |
| `GetContextView` / `ContextSummary` | 上下文聚合视图 |
| `LoadedSkills` / `ListSessionSkills` / `LoadSessionSkill` / `UnloadSessionSkill` | skills 读写 |
| `ClearContext` / `Delete` / `Release` | 清空消息、删除 session，或卸内存保留 DB（F-NM1） |
| `EnqueueMessage` | user / resume / 高优先级消息入队 |
| `RunInboxTurn` | 历史 A2A inbox 兼容入口；新跨机协作不使用，改走 Workgroup Session |
| `EnqueueAsyncToolResult` / `EnqueueToolResult` | 工具续跑与异步回灌 |
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
| `handleSideEffectProduceAsync` / `handleSideEffectProduceExternal` | 旁路 Produce（缓冲 + SSE） |
| `handleSideEffectContinue` | 旁路 Apply + 被动 LLM 续跑 |
| `handleResume` | HITL resume 续跑 |
| `applyStepOutcome` | 同步当前 Step 的 messages；Pending 与步序从 Coordinator 投影读取 |
| `persist` / `clearMessages` | SQLite 持久化 |
| `enqueue` | 带优先级入队；高优先级项先出队（见 `queue/README.md`）；`human` 先于 `resume` |
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

## release.go / idle_auto_compress.go

| 符号 | 说明 |
|------|------|
| `Release` | persist → stop consumer → 移出 `m.sessions`；不删 SQLite（F-NM1） |
| `scanIdleSessionMaintenance` | idle 扫描：可选压缩 → Release（F-NM2–NM5） |
| `StartIdleAutoCompressScanner` | 启动 idle 维护后台循环 |

## triggers.go

| 符号 | 说明 |
|------|------|
| `TriggerSubmitter` | trigger 模块用的 session 创建与入队适配器 |
| `EnsureSession` | `Manager.Create` 包装 |
| `SubmitTriggerMessage` | 入队 trigger 渲染后的 user 消息 |
| `EnqueueTriggerMessage` | 校验 content → EnsureSession → `PriorityOther` 入队 |

## side_effects.go / runtime_side_effects.go

| 符号 | 说明 |
|------|------|
| `sideEffectStore` | 旁路 Produce 缓冲（FIFO seq）；`Produce` / `ApplyReady` / `ReconcileAfterStep` |
| `handleSideEffectProduceAsync` / `handleSideEffectProduceExternal` | consumeLoop → Produce + SSE |
| `handleSideEffectContinue` | Apply + `ContinueAfterSideEffects` 被动 LLM |
| `runTurnStepWithSideEffects` | 步首 `ApplyReady`、步末 `ReconcileAfterStep` |
| `maybeScheduleContinueAfterCancel` | Cancel 三分法：无 pending 且有缓冲 → continue |
