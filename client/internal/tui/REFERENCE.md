# REFERENCE — `client/internal/tui`

## `dispatch.go`

| 符号 | 说明 |
|------|------|
| `Options` | `Plain` / `ForceFull` 模式开关 |
| `Run` | 分发至 `full.Run` 或 `repl.Run` |
| `preferPlain` | `DAGENTS_TUI`、`TERM`、isTTY 探测 |

## `full/`

| 符号 | 说明 |
|------|------|
| `Run` | 启动 bubbletea 全屏 TUI |
| `model` | viewport + textarea + SSE/HITL 状态 |
| `runSSELoop` | SSE 订阅与重连 |

## `repl/`

| 符号 | 说明 |
|------|------|
| `Run` | 启动行模式 REPL |
| `App` | REPL 会话与命令 |
| `streamRunner` | SSE 后台泵 |

## `shared/`

| 符号 | 说明 |
|------|------|
| `Transcript` | 输出行缓冲、流式 partial |
| `ToolFold` | tool 事件折叠/展开 |
