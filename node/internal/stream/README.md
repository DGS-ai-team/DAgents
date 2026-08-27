# node/internal/stream

进程内 SSE 事件总线（Hub）。

## 职责

| 文件 | 说明 |
|------|------|
| `hub.go` | `Hub`：按 Agent 订阅、广播、回放（`after_agent_seq`） |
| `publisher.go` | `Publisher` 接口（`turn.Orchestrator` 注入） |
| `hub_test.go` | 订阅与回放单测 |

## 事件流

```
turn.Orchestrator / session / childagent
        → Hub.Publish(agentID, type, data)
        → GET /v1/streams 长连接推送给 Client
```

常见 `type`：`assistant`、`reasoning`、`tool_call`、`tool_result`、`usage`、`turn_state`、`hitl_required`、`turn_finished` 等。

`turn_state` 是回合生命周期的权威快照；`hitl_required` 是需要用户交互的事实；`turn_finished` 只表示真正的终态。HITL 暂停不会发送 `turn_finished`，也不会借用终态事件表达“等待用户”。

每条事件带有 `event_version`、`stream_epoch`、进程级 `seq`、可重放的 Agent 级 `agent_seq` 与 `delivery`。过滤到单个 Agent 的客户端重连使用 `after_agent_seq`；游标落后于 Hub 保留历史时，HTTP 流先发送 `resync_required`，客户端必须 hydrate。

## 相关文档

- API 契约：[`docs/architecture/agent-node-api.md`](../../../docs/architecture/agent-node-api.md)
- Turn SSE：事件分工见 [`../turn/README.md`](../turn/README.md)
