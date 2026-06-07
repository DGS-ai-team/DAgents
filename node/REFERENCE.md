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
| `EnvOpenAIClient` | OpenAI 兼容流式 HTTP |

## `internal/turn/`

| 符号 | 说明 |
|------|------|
| `State` | `idle` / `model_streaming` / `awaiting_tool` |
| `BuildSystemPrompt` | 最小 system prompt（静态规则 + agent/FS_ROOT） |
| `RunTurnPhase` | Node 状态 → Python 兼容 `run_turn_phase` |
| `DefaultMaxToolLoops` | 工具循环默认上限（16） |
| `RunMessageTurn` | LLM + 工具循环；SSE assistant/tool_call/tool_result/done |
| `publishTurnIdleDone` | 语义 B 的 `done`：`finish_reason` + `turn_complete` + `awaiting`；文档见 `docs/architecture/agent-node-api.md` §2.4.1 |

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
| `handleDeleteSession` | `DELETE /v1/sessions/{id}` |
| `handleClearContext` | `POST /v1/sessions/{id}/clear-context` |
| `handleSessionContext` | `GET /v1/sessions/{id}/context`（含 queue_pending） |
| `handleCancelSession` | `POST /v1/sessions/{id}/cancel` |
