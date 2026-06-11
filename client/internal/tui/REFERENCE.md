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
| `model` | viewport + textarea + SSE/HITL + tool 展开 + 等待态 |
| `statusLineManager` | prefilling/thinking/compression 展示层行 |
| `refreshContextViewportContent` | `/context` viewport 刷新 |
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
| `Transcript` | 输出行缓冲、流式 partial、`RemoveToolPendingLines` |
| `ToolFold` | 全局 verbose 开关 |
| `ToolBlockRegistry` | 单条 tool 展开/收起状态 |
| `ToolDisplayOptions` | viewport 渲染时过滤 tool 详情行 |
| `FormatWelcomePanelBody` | 启动欢迎面板正文 |
| `FormatSessionContextPanel` | `/context` ANSI 面板文本 |
| `theme.go` | ANSI 主题色常量 |
| `TurnGate` | 用户 turn 栅栏 |
