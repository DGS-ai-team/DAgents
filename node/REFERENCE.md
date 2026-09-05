# REFERENCE — `node`

## `cmd/dagents-node/main.go`

| 符号 | 说明 |
|------|------|
| `main` | 解析 `-config`，加载配置并启动 HTTP 服务 |

## `internal/store/sqlite.go`

| 符号 | 说明 |
|------|------|
| `SQLiteStore` | session 消息 SQLite 持久化 |
| `Open` / `Save` / `Load` / `List` / `Delete` / `ClearMessages` | CRUD 与摘要列表 |

## `internal/tools/`

| 符号 | 说明 |
|------|------|
| `Registry` | 工具注册表与 FS_ROOT 沙箱执行 |
| `read_file` / `write_file` / `bash_run` | N3 内置工具 |

## `internal/llm/`

| 符号 | 说明 |
|------|------|
| `Client` | LLM 接口（支持 tools） |
| `ChatRequest` / `ChatResult` | 含 messages 与 tool_calls |
| `MockClient` | mock；`EnableTools` 驱动工具环测 |
| `envAdapterClient` | 环境变量 API Key + MessageAdapter 出站 |

## `internal/turn/`

| 符号 | 说明 |
|------|------|
| `State` | `idle` / `model_streaming` / `awaiting_tool` |
| `BuildSystemPrompt` | 稳定 system prompt（行为规则、工作区与可用能力目录；运行时身份通过 ContextInjection 注入） |
| `DefaultMaxSteps` | Agent 每个 Turn 的工具步数默认上限（32） |
| `RunHumanMessageTurn` / `RunToolMessageTurn` | 单步 LLM + 工具；生产由 session runtime 在同一 Turn 链内 inline 续跑 |
| `publishTurnFinished` | `turn_finished` 终态事件；实现见 `node/internal/turn/sse_publish.go` |

## `internal/queue/queue.go`

| 符号 | 说明 |
|------|------|
| `MessageQueue` | per-session 优先级队列 |

## `internal/stream/hub.go`

| 符号 | 说明 |
|------|------|
| `Hub` | SSE 事件总线 |

## `internal/session/`

| 符号 | 说明 |
|------|------|
| `Manager` | session 表、入队、cancel、持久化恢复 |
| `runtime` | consumer + 对话历史 + turn 后落盘 |

## `internal/api/server.go`

| 符号 | 说明 |
|------|------|
| `NewServer` | 注册路由；`WithLLM` / `WithTools` / `WithPolicy` / `WithStore` |
| `handleClearContext` | `POST /v1/agents/{id}/clear-context`（经 withAgentAsSession） |
| `handleSessionContext` | `GET /v1/agents/{id}/context`（含 queue_pending） |
| `handleCancelSession` | `POST /v1/agents/{id}/cancel` |
