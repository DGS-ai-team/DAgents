# Turn / Step Runtime 重构实现状态

本文记录 `turn-step-runtime-refactor.md` 在 DAgents 当前工作树中的实现落点，作为后续继续重构和验收的基线。

## 已落地

- `TurnCoordinator` 统一维护 Turn、Step、ModelAttempt、ToolBatch、ToolExecution、PendingInteraction、快照、Context Epoch 和多维用量。
- Runtime 不再保存第二份 `pending`、Turn identity、generation、continuation 或执行 state；所有交互等待、Step 序号和 fencing 均从 Coordinator 投影读取。Orchestrator 通过 `TurnExecutionContext` 获取当前 Step，不再接收独立的 loop counter。
- Coordinator 的普通 Dispatch、Durable Dispatch 和事件 Restore 都具备失败回滚；工具执行事实按 proposed/running/known-terminal/unknown 约束转移，避免乱序或未对账结果推进 Step。
- 队列消费统一经过 `runtime.dispatchTurnRequest`，旧的 `message`、`tool_result`、`resume`、副作用续跑仍作为兼容输入，生命周期事实全部进入协调器。
- 生命周期事件写入 `turn_events`；`DispatchDurable` 在事件持久化失败时回滚内存 Projection，避免“状态已前进但事件丢失”。相同 `command_id` 在协调器和 SQLite 两层幂等。
- Node 重启时，无法证明结果的工具执行会进入 `unknown`；续跑被恢复栅栏阻止。已有完整 tool message 的情况可以从历史补齐结果事实；其余通过
  `POST /v1/agents/{agent_id}/turns/{turn_id}/steps/{step_id}/tool-executions/{execution_id}/reconcile` 对账。
- `GET /v1/agents/{agent_id}/timeline?after_seq=&limit=` 提供基于 `session_seq` 的生命周期游标读取，可用于 SSE 断线后的补偿。
- 模型请求重试已拆到 `model_attempt_runner.go`，同一 Step 内生成新的 ModelAttempt；模型 token 用量按 attempt 增量累计，支持输入、输出、总 token、成本和墙钟预算检查。
- 工具重试只对 Executor 明确声明的读取型工具开启，复用原 ToolCall/ToolExecution ID，并记录 `tool.execution.retrying`；bash、写入、传输等副作用工具不会自动重试。
- 并发工具仍按完成顺序发布实时事件，但按 ToolCall 原始顺序写入模型 history。
- Context 压缩记录 Context Epoch、压缩前后 history digest 以及消息范围；Turn 内 system prompt 和 tools snapshot 不动态改变。
- Step 记录 provider request attempt ID 和 assistant message digest；该关联随生命周期事件恢复，并通过 SSE 元数据暴露，减少对 history 扫描的依赖。
- `ReserveFinalSummary` 可为预算耗尽保留一次独立的无工具 Summary Step；它单独计入 `summary_steps`，不会静默突破普通 Step 预算。
- SSE 生命周期元数据包含 `turn_id`、`step_id`、`step_index`、`context_epoch`、`event_seq`、终止原因和恢复状态。
- 活动 HITL 的唯一正常运行来源是 `Coordinator` 的 `WaitingInteraction + InteractionPayload`；`handleResume`、新 human、hydrate、context 和 notification 不再从普通历史消息反推 pending。旧历史反推只保留在启动时的一次性 legacy migration 桥接中。
- 工具事实按边界分层：只有模型刚进入 `AssistantReceived` 的响应可以注册新的 `ToolCall`；后续历史观察只接受当前 `ToolBatch` 已拥有的调用。因此异步/外部回灌产生的 `tool_callback` / `get_callback` 只能作为上下文消息，不能再触发第二个 `ToolExecution`。
- 旁路 Apply 在生命周期中写入 `external.fact.recorded`，以 `kind + sequence` 幂等记录 async job / trigger 等外部事实；这与模型可读的 history bridge 分离，重启回放不依赖 callback 工具名过滤。
- 重复 resume 在第一个 resume 已出队但仍处于活动 Turn 时允许入队，由 runtime 按生命周期状态幂等丢弃；Turn 已终态后仍严格返回 `no_pending_hitl`。

## 当前验证

- `go test ./node/... -count=1`：Node 全包通过。
- `go test -race ./node/internal/session ./node/internal/turn ./node/internal/api -count=1`：Turn/Step、session、API race 回归通过。
- `go test ./client/... ./shared/config/... -count=1`：Client、Shared 全部通过。
- `npm test --prefix node/webui/frontend -- --run`：Node Web UI 42 个测试文件、252 个测试通过。
- `npm run build --prefix node/webui/frontend`：Node Web UI 构建通过。
- `npm run build --prefix manage/console/frontend`：Manage Console 构建通过。
- `py -3 -m unittest discover -s tests -p "test_*.py" -v`：Python 185 个测试通过。
- 新增冷 session 投影回归：Context / Hydrate / Notification 在旧 RuntimeState 镜像与事件投影冲突时均以 Coordinator 重放结果为准。
- 真实 Node 进程重启 E2E 覆盖 HITL resume 与 unknown tool reconciliation；全量复测通过。
- Node + Web UI 浏览器冒烟覆盖：Mock 对话往返、Context / Timeline 终态断言，以及刷新后的 transcript / SSE 恢复；详见 [ui-e2e-smoke-checklist.md](./ui-e2e-smoke-checklist.md)。

## 后续演进项（不阻塞本轮 Turn / Step 重构）

1. 将高频执行/消息内容关联进一步从兼容 history 推断迁移到独立 Projection 表或等价的事件投影缓存；当前活动 HITL 和 external fact 已先完成该迁移，剩余主要是大体量 tool output 的内容索引。
2. 在真实 Node 重启、HITL 恢复和子 Agent 工作流上补齐端到端验收；当前已有协调器、Runtime、Store、API 回归覆盖。
3. 清理持久化 `RuntimeState` 中仅用于旧版本恢复的 `Pending` / `ToolLoopCount` 字段；当前它们只作为一次性输入兼容旧数据库，运行时和生命周期事件已不再依赖它们。
