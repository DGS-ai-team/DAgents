# 临时子 Agent 工具与运行模型

本文描述 Node 内部临时子 Agent 的当前契约。临时子 Agent 是同一 Node 进程内受父 Agent 委派的短生命周期 runtime，不等同于 Manage 层的外部 A2A Agent。

## 1. 设计目标

- 父 Agent 使用一个普通的 ToolCall 委派自包含任务。
- 创建工具只允许同步执行：工具返回前，子 Agent 必须进入明确终态。
- 子 Agent 的实时进度通过父 session SSE 展示，刷新时从 ChildRun 快照恢复。
- 审批仍属于 Turn 链条；用户通过 resume 恢复，取消必须走 Turn cancel 或显式 cancel 工具。
- 子 Agent 完整 transcript 留在子 runtime，父侧只保存控制面快照和摘要。

## 2. 对外工具

### `create_temporary_agent`

必填：`task`、`purpose`。

可选：`allowed_tools`、`skill_names`、`ttl_seconds`、`max_turns`。

`task` 必须自包含，`purpose` 是 UI/日志摘要。`skill_names` 在子 runtime 创建时预加载，子运行期间不能修改 skills。

返回统一终态结果：

```json
{
  "kind": "result",
  "child_agent_id": "child-abc123",
  "status": "completed",
  "summary": "检查完成",
  "turn_count": 3,
  "artifacts": [],
  "error": ""
}
```

不存在 `wait` 参数，也不存在通过模型二次查询结果的等待/状态工具。TTL 到期、父 Turn 取消、子 runtime 异常都会直接收敛为终态并返回结果。

### `cancel_temporary_agent`

接收 `child_agent_id` 和可选 `reason`，取消父 session 下仍处于 `creating`/`active` 的子 Agent。取消返回统一结果；已完成的子 Agent 不会被重新打开。

## 3. 状态机

```text
creating → active → completed
                 ├→ failed
                 ├→ cancelled
                 ├→ expired
                 └→ interrupted
```

`interrupted` 只在 Node 重启恢复时使用：持久化记录显示仍在运行，但进程内 runtime 已不存在，不能伪造为继续运行。

状态迁移由 `childagent.Manager.finishWithEvent` 统一处理。任何迁移都更新进度 revision、摘要/错误、完成时间，持久化快照并发布父 session 事件。

## 4. 同步创建时序

```text
父模型 ToolCall
  → Turn 记录 assistant tool_call
  → Tool policy: deny / approval / auto
  → approval: 提交 PendingHITL，再发布 hitl_required
  → resume: 仍在同一个父 Turn 内执行 create
  → Manager 创建 creating 记录
  → Host.SpawnChild + EnqueueChildTask
  → active + temporary_agent_created
  → 子 runtime 执行并发布 progress
  → completed/failed/cancelled/expired
  → create 返回 tool result
  → 父模型收到 tool result 并决定下一步
```

`messageQueue` 只承载明确的输入、resume、cancel 和 Turn continuation。它不承载“异步子 Agent 完成回调”，也不需要维护一个等待句柄协议。

## 5. 子 runtime 与工具权限

`session.Manager.SpawnChild` 使用 `RestrictedRegistry` 创建子 runtime。`allowed_tools` 必须是父 Agent 可下放工具的子集；管理工具、skills 变更、询问工具以及 trigger 等 parent-only 工具不能下放。

子 runtime 的首条输入由 `FormatChildTask` 包装。完成条件是：当前 Turn 空闲、没有 pending HITL、输入队列为空，且最后一条消息是 assistant 文本。LLM、工具链、生命周期或回合上限错误走 `OnChildFailed`，不得依赖某个正常 assistant 事件才能回收。

## 6. 审批

### 父侧创建审批

父模型提出 `create_temporary_agent` 后，如果策略要求审批，Turn runtime 先持久化 `PendingHITL`，再发布 `hitl_required`。这样前端收到卡片后立即 resume 不会出现 `no_pending_hitl` 竞态。

### 子侧工具审批

子 runtime 调用受审批工具时，`RelayHub` 将事件转发到父 session，并附加 `child_agent_id`、`hitl_scope=temporary_agent` 和 child purpose。resume 仍发送给父 session，由 `Manager.RouteResume` 校验归属和待审批状态后投递给子 runtime。

普通 human message 不会抢占活动 Turn。需要打断执行时，调用 Turn cancel；取消后的旧 continuation 受 Turn/epoch 校验保护，不得继续写入新的消息序列。

## 7. 进度与事件

父 session 可能收到以下子 Agent 事件：

| 事件 | 作用 |
|---|---|
| `temporary_agent_created` | 子 runtime 已创建并开始接收 task |
| `temporary_agent_progress` | 当前 phase、工具、回合数、最近输出或审批快照 |
| `temporary_agent_completed` | 正常完成或 TTL 终态结果 |
| `temporary_agent_cancelled` | 显式取消或父级取消 |

子 runtime 的 assistant、reasoning、tool_call、tool_result 事件仍可通过 relay 关联到父 session；UI 默认在工具卡片展开区展示进度，避免把子 transcript 平铺到父消息流。

## 8. 持久化、刷新与重启

`child_runs` 表保存以下控制面字段：父子 ID、工具调用 ID、purpose、权限、预加载 skills、状态、phase、Progress JSON、回合数、TTL、摘要/错误、时间戳和 revision。

`childagent` 依赖 `RunRepository` 接口，SQLite 适配位于 `node/internal/session/child_run_repository.go`。保存以 `child_agent_id` 幂等 upsert，读取按父 ID 和更新时间排序。

`GET /v1/agents/{id}/child-agents` 和 hydrate 都走 `Manager.ListSnapshots`，合并：

1. 当前活跃 runtime；
2. 当前进程最近终态；
3. SQLite 中的终态/中断记录。

因此刷新不会把已完成任务重置成 active。若 `Progress.pending_approval_data` 存在，hydrate 会重建统一 HITL 卡片；真正的 resume 仍由后端重新校验。

## 9. 前端投影

`remoteWorkers` 是单一子 Agent UI 投影：

- `temporary_agent_created/progress` 更新运行态；
- `completed/cancelled` 保留终态卡片，供用户查看结果；
- hydrate/API 是权威补偿源，SSE 只提供低延迟更新；
- revision 倒退的事件被忽略；
- 工具卡片先按 `tool_call_id` 关联，结果到达后按 `child_agent_id` 兜底。

这套投影不再维护另一个“活跃子 Agent 列表”，也不要求前端轮询状态工具。

## 10. 关键实现入口

| 路径 | 作用 |
|---|---|
| `node/internal/childagent/manager.go` | 生命周期、同步等待、快照和事件 |
| `node/internal/childagent/progress.go` | 子事件到 Progress 的映射 |
| `node/internal/childagent/tools_handler.go` | create/cancel 工具入口 |
| `node/internal/session/manager_child.go` | Host 与 API 视图 |
| `node/internal/session/runtime_child.go` | 子 runtime 创建与完成判断 |
| `node/internal/session/child_run_repository.go` | store 适配 |
| `node/internal/turn/tool_router.go` | 父 ToolCall 的审批、执行和 ToolResult 接回 |
| `node/webui/frontend/src/stores/remoteWorkers.js` | 前端实时/刷新投影 |
| `node/webui/frontend/src/stores/hydrate.js` | hydrate 与审批恢复 |

## 11. 验证

```bash
go test ./node/internal/childagent -run Child
go test ./node/internal/session -run ChildAgent
go test ./node/internal/api -run ChildAgent
npm test --prefix node/webui/frontend -- --run src/stores/remoteWorkers.test.js
```
