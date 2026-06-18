# shared — TUI 共用组件

| 文件 | 用途 |
|------|------|
| [`transcript.go`](transcript.go) | `Transcript` 行缓冲、`SnapshotLinesForDisplay`（tool 折叠过滤） |
| [`transcript_display.go`](transcript_display.go) | 彩色圆点、usage 右对齐、system panel ANSI |
| [`theme.go`](theme.go) | 终端 ANSI 主题色常量 |
| [`welcome_panel.go`](welcome_panel.go) | 启动欢迎面板正文 |
| [`tool_block.go`](tool_block.go) | 单条 tool 块展开/收起 registry |
| [`command_panel.go`](command_panel.go) | `/status` / `/sessions` 等面板正文 |
| [`context_format.go`](context_format.go) | `/context` 面板与纯文本格式 |
| [`tokens_format.go`](tokens_format.go) | input strip token / usage |
| [`tool_format.go`](tool_format.go) | tool_call/tool_result 格式化（含 preview/detail 行） |
| [`turn_gate.go`](turn_gate.go) | 用户 turn 栅栏 |
