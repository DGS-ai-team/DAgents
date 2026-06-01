# repl — 行模式 REPL TUI

| 文件 | 用途 |
|------|------|
| [`app.go`](app.go) | `dagents>` 主循环、斜杠命令 |
| [`stream.go`](stream.go) | SSE 订阅与断线重连 |

无 alternate screen；HITL 走 stdin/stderr。适用于 RHEL 6 极老 SSH、`--plain`、`TERM=dumb`。
