# session 模块参考

## manager.go

| 符号 | 说明 |
|------|------|
| `TurnOptions` | turn 编排配置（workspace root、MaxToolLoops、skills、压缩、journal、memory v2） |
| `Manager` | 会话表；每 session 独立 runtime + InputBox + 控制队列 |
| `NewManager` | 绑定 agent、Hub、LLM、Registry、policy、store、TurnOptions |
| `SetChildAgentManager` | 注入 `childagent.Manager` 并 `BindHost` |
| `SetTriggerDeliveryTracker` | trigger 输入被消费或 session 清理时清除 pending delivery |
| `Stop` | 取消 Manager 上下文并停止所有 runtime |
| `Create` | 创建或恢复 session；`newRuntime` + `attachUserChildTools` + `start` |
| `Get` / `ListActive` / `ListPersisted` | 查询活跃或持久化会话 |
| `RuntimeInfo` | 队列深度、是否有活跃 turn、turn 状态 |
| `GetContextView` / `ContextSummary` | 上下文聚合视图 |
| `LoadedSkills` / `ListSessionSkills` / `LoadSessionSkill` / `UnloadSessionSkill` | skills 读写 |
| `ClearContext` / `Delete` / `Release` | 清空消息、删除 session，或卸内存保留 DB（F-NM1） |
| `EnqueueMessage` | user 进入 InputBox；resume 进入控制队列 |
| `EnqueueAsyncToolResult` | 异步工具完成事实回灌 |
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
| `runtime` | 单 session 内部状态（未导出；InputBox 是外部输入入口） |
| `newRuntime` | 父 session 构造入口 → `newRuntimeWithPublisher` |
| `newRuntimeWithPublisher` | 创建 `runtime` + `InputBox` + `turn.NewOrchestrator` |
| `start` / `consumeLoop` | 启动 InputBox/控制队列消费 goroutine |
| `handleInputMessage` | user/trigger 输入启动新 Turn；活动 Turn 期间不抢占 |
| `handleTurnContinuation` | 恢复/重启后的 tool message 单步续跑 |
| `handleSideEffectProduceAsync` | 异步工具结果 Produce（缓冲 + SSE） |
| `handleSideEffectContinue` | 旁路 Apply + 被动 LLM 续跑 |
| `handleResume` | HITL resume 续跑 |
| `lifecycleAfterModelStep` / `lifecycleAfterResume` | 将单步结果映射为 Coordinator 生命周期状态，并同步当前 Step 的 messages |
| `persist` / `clearMessages` | SQLite 持久化 |
| `enqueue` | 仅传输 resume、异步事实和恢复控制项；外部输入不再经过优先级队列 |
| `candidatePipeline` | 可选的有界后台记忆候选管线；只接收压缩快照，不向 Turn/MessageQueue 注入消息 |
| `cancelTurn` / `stop` | 取消或停止 consumer |
| `contextView` | 组装 `ContextView` |
| `runTurnStepAtEpoch` / `finishTurnIdle` | 单步 turn 脚手架；`finishTurnIdle` 在生命周期收尾后触发子 Agent 结算 |
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
| `EnqueueTriggerMessage` | 校验 content → EnsureSession → InputBox FIFO |

## side_effects.go / runtime_side_effects.go

| 符号 | 说明 |
|------|------|
| `sideEffectStore` | 旁路 Produce 缓冲（FIFO seq）；`Produce` / `ApplyReady` / `ReconcileAfterStep` |
| `handleSideEffectProduceAsync` | consumeLoop → Produce + SSE |
| `handleSideEffectContinue` | Apply + `ContinueAfterSideEffects` 被动 LLM |
| `runTurnStepWithSideEffects` | 步首 `ApplyReady`、步末 `ReconcileAfterStep` |
| `maybeScheduleContinueAfterCancel` | Cancel 三分法：无 pending 且有缓冲 → continue |
