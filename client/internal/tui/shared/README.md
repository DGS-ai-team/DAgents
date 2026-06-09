# shared — TUI 共用组件

| 文件 | 用途 |
|------|------|
| [`transcript.go`](transcript.go) | `Transcript` 行缓冲、`AddSystemPanel`、`SnapshotLinesForDisplay`（含流式 partial） |
| [`transcript_display.go`](transcript_display.go) | 彩色圆点、usage 右对齐、system panel ANSI 样式 |
| [`command_panel.go`](command_panel.go) | `/status` / `/sessions` / `/skill` / `/help` / `/children` 面板正文 |
| [`context_format.go`](context_format.go) | `/context` 只读视图文本 |
| [`tokens_format.go`](tokens_format.go) | input strip token / usage / cache hit 短文案 |
| [`tool_format.go`](tool_format.go) | tool_call/tool_result 用户可读格式化 |
| [`turn_gate.go`](turn_gate.go) | 用户 turn 栅栏（`seqFence`、语义 B `done`；对齐 Python `SessionController`） |
