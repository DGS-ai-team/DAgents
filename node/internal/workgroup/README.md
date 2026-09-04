# Workgroup AgentRef Worker

Node 侧工作组成员执行器。成员不是 Node 的第二套 Agent，也不会在 Manage
侧复制 Agent 的工具、提示词或权限配置；Manage 只保存 AgentRef，并通过
持久 WebSocket outbox 驱动一个隔离的工作组 Session。

## 当前职责

| 能力 | 实现 |
| --- | --- |
| session open/close | `dispatch.go` |
| turn start/cancel/resume | `dispatch.go` |
| Agent session 与 turn 事件上报 | `worker.go`、`ws_client.go` |
| 断线重连与 outbox resume | `dialer.go`、`session.go` |
| 连接世代 fencing | `session.go` |
| 工具取消 | `agent.tool.cancel` |

成员 Agent 的真实工具注册、LLM、审批和本地策略全部复用 Node 现有
AgentRuntime；Workgroup 只接收脱敏的 Timeline/Realtime 事件，并把
AgentRef 的最终结果返回给 Manage。

## 事件边界

- `timeline.event` 是 Manage 写入并可靠投递的公共事实。
- `workgroup.realtime` 是临时进度广播，断线后以 Timeline/hydrate 为准。
- `agent.*` 是 Manage 与 Node 之间的 session/turn 控制和结果事件。

不在这里维护成员工具目录、成员 prompt、成员工作区绑定或独立成员 LLM
循环。需要查看当前协议时，直接阅读 `types.go`、`dispatch.go` 和
`ws_client.go`，避免以历史 D0.5 文档推断生产行为。
