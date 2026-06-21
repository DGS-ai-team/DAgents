# node/internal/stream

进程内 SSE 事件总线（Hub）。

## 职责

| 文件 | 说明 |
|------|------|
| `hub.go` | `Hub`：按 session 订阅、广播、回放（`after_seq`） |
| `publisher.go` | `Publisher` 接口（`turn.Orchestrator` 注入） |
| `hub_test.go` | 订阅与回放单测 |

## 事件流

```
turn.Orchestrator / session / childagent
        → Hub.Publish(sessionID, agentID, type, data)
        → GET /v1/streams 长连接推送给 Client
```

常见 `type`：`assistant`、`reasoning`、`tool_call`、`tool_result`、`usage`、`done`、`hitl_required` 等。A2A / 子 Agent 路径仍可能出现 `approval_required`、`user_information_required`。

## 相关文档

- API 契约：[`docs/architecture/agent-node-api.md`](../../../docs/architecture/agent-node-api.md)
- Turn SSE：`done` 语义见 [`../turn/README.md`](../turn/README.md)
