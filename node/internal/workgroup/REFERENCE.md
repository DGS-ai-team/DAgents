# Workgroup AgentRef Reference

## 包职责

| 文件 | 职责 |
| --- | --- |
| `types.go` | AgentRef、session、Assign/Turn identity、outbox 和事件载荷 |
| `worker.go` | Node 侧 WS 会话与事件出口 |
| `dispatch.go` | `agent.session.*`、`agent.turn.*`、`agent.tool.cancel` 分发 |
| `ws_client.go` | Manage outbox 读取、ACK 与 AgentRef 事件回传 |
| `dialer.go` | WSS 连接、重连与 resume |
| `session.go` | delivery cursor 与 connection generation fencing |

## 当前控制帧

Manage → Node：

- `agent.session.open`
- `agent.session.close`
- `agent.turn.start`
- `agent.turn.cancel`
- `agent.tool.cancel`
- `agent.turn.resume`

Node → Manage：

- `agent.session.ready` / `agent.session.error` / `agent.session.closed`
- `agent.turn.accepted` / `agent.turn.event` / `agent.turn.result`
- `agent.turn.cancelled` / `agent.tool.cancelled`

所有可靠控制帧都经 outbox、delivery sequence 和 ACK；实时事件不承担恢复
职责。事件类型与分发逻辑分别集中在 `types.go` 和 `dispatch.go`，Node 的
AgentStore/AgentRuntime 是工具和模型能力的唯一来源。

## Turn identity 与事件恢复

`agent.turn.start` 必须携带：

- `assign_id`：Manage 侧唯一委派；
- `source`：`leader_tool` 或 `direct_member`；
- `parent_turn_id`：发起委派的父 Turn；
- `child_turn_id`：本次成员 Agent Turn；
- `attempt_id`：本次执行尝试。

`assign_id` 不再充当 Turn ID，也不再发送旧的 `turn_id` 字段。`agent.turn.event`
还携带 `event_id`、`event_seq`、`stream_epoch`。Node 重连时继续发送原 child Turn 的
事件；Manage 以 `stream_epoch + event_seq` 去重，迟到的旧 epoch 事件不能覆盖当前
Assign 状态。若 Node 进程仍保留成员 Session，重放 start 只重新订阅事件，不重新
追加一条 human message。

工具审批与用户询问也严格区分：前者使用 `tool_approval`，后者使用
`user_question`；两者恢复时都必须回到原 Assign/child Turn/attempt，不能由 Manage
伪造成员的 tool result。
