# node/internal/api

Agent Node 对本地 Client 暴露的 HTTP/SSE 端点（composition root）。

## 职责

| 文件 | 说明 |
|------|------|
| `server.go` | 路由注册、`NewServer`、依赖装配（session / stream / store / triggers / llm / tools） |
| `*_test.go` | HTTP 集成单测（agents、SSE、child agent、triggers） |

**边界**：本包只做请求解析、JSON 映射、错误码；turn 执行与队列消费委托 `session.Manager`。

## 主要路由（摘要）

| 方法 | 路径 | 委托 |
|------|------|------|
| CRUD | `/v1/agents` | Agent 主契约（ensure / hydrate / context / cancel / skills / child-agents / media / policy） |
| POST | `/v1/messages` | 入队 user / resume |
| GET | `/v1/streams` | SSE 订阅 `stream.Hub` |
| CRUD | `/v1/triggers` | `triggers` 调度与 fire |

> `/v1/sessions*` 路由已删除（404）。

完整契约见 [`docs/architecture/agent-node-api.md`](../../../docs/architecture/agent-node-api.md)。

## 相关文档

- Go Node 内部结构：[`docs/architecture/go-node-internals.md`](../../../docs/architecture/go-node-internals.md)
- Session 队列：[`../session/README.md`](../session/README.md)
## Terminal WebSocket

`GET /v1/agents/{agent_id}/terminals/ws` opens one PTY per WebSocket connection. The first frame is `open`; later frames are `input`, `resize`, `terminate`, or `close`. JSON `data` fields use base64. `resize` is an internal UI/PTY operation, not an Agent tool. `terminate` sends Ctrl+C first, then closes the PTY if the process does not exit within the grace period.

Interactive terminal sessions are reaped after 10 minutes without input or PTY output. Reading an unchanged snapshot does not refresh this idle timer; Node restart always starts with an empty in-memory session registry.
