# repl — 行模式 REPL TUI

| 文件 | 用途 |
|------|------|
| [`app.go`](app.go) | `dagents>` 主循环、斜杠命令、turn 等待 |
| [`stream.go`](stream.go) | SSE 订阅与断线重连 |
| [`turn_gate.go`](turn_gate.go) | turn 期间阻塞主循环读 stdin |

无 alternate screen；HITL 走 stdin/stderr。用户发消息后主循环等待 `done` 再显示 `dagents>`，避免与审批/询问抢 stdin。适用于 RHEL 6 极老 SSH、`--plain`、`TERM=dumb`。
