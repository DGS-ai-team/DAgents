# node/internal/api

Agent Node 对本地 Client 暴露的 HTTP/SSE 端点（composition root）。

## 职责

| 文件 | 说明 |
|------|------|
| `server.go` | `Server` 结构、Option 和 `NewServer` 组合根 |
| `server_routes.go` | 统一挂载核心与领域路由 |
| `server_wiring.go` | Node 级依赖注入、执行事件 SSE、后台任务回灌 |
| `server_lifecycle.go` | HTTP 监听、Manage sidecar、优雅关闭 |
| `server_messages.go` | `/v1/messages` 入队与 `/v1/streams` SSE |
| `conversation_history_api.go` | 已结束对话轮次与轮内 transcript 的只读分页 |
| `server_agent_api.go` | health、Agent 信息、context、hydrate、skills 等基础接口 |
| `*_test.go` | HTTP 集成单测（agents、SSE、child agent、triggers） |

**边界**：本包只做请求解析、JSON 映射、错误码；turn 执行与队列消费委托 `session.Manager`。

## 主要路由（摘要）

| 方法 | 路径 | 委托 |
|------|------|------|
| CRUD | `/v1/agents` | Agent 主契约（ensure / hydrate / context / cancel / skills / child-agents / media / policy） |
| POST | `/v1/messages` | 入队 user / resume；user message 必须携带 `client_request_id` 与 `conversation_epoch`，Node 持久回执支持安全重试 |
| GET | `/v1/agents/{agent_id}/inputs` | 按 request ID 查询已接受消息的当前回执 |
| GET | `/v1/agents/watermarks` | 批量读取 conversation revision 与 Agent SSE 目标序号；不读取运行态 JSON / transcript，也不启动 runtime |
| POST | `/v1/agents/{agent_id}/inputs/control` | 按 `queue_control_revision` CAS 暂停 / 恢复待处理输入队列 |
| GET | `/v1/agents/{agent_id}/conversations/{conversation_id}/rounds` | 按稳定轮次锚点分页读取已结束轮次 |
| GET | `/v1/agents/{agent_id}/conversations/{conversation_id}/rounds/{round_id}/revisions` | 只读列出轮次保留的终态修订版本 |
| GET | `/v1/agents/{agent_id}/conversations/{conversation_id}/rounds/{round_id}/items` | 读取指定轮次 / revision 的安全 transcript 项目页 |
| GET | `/v1/agents/{agent_id}/conversations/{conversation_id}/rounds/{round_id}/items/{item_id}/content` | 按需读取超长正文 / 推理内容 |
| GET | `/v1/agents/{agent_id}/conversations/{conversation_id}/rounds/{round_id}/tool-steps` | 按 assistant 记录及工具调用 ID 分页读取完整配对摘要 |
| GET | `/v1/agents/{agent_id}/conversations/{conversation_id}/rounds/{round_id}/tool-steps/{tool_call_id}` | 按需读取单个工具调用与结果详情 |
| GET | `/v1/agents/{agent_id}/conversations/{conversation_id}/legacy-items` | 读取尚无轮次归属的旧 transcript 项目页 |
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
