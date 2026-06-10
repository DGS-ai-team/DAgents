# app/cli/tui — Textual 聊天界面

| 文件 | 用途 |
|---|---|
| [`app.py`](app.py) | 主 App：顶栏 SSE 状态、聊天 transcript（`TranscriptLog`）、`/context` 摘要视图、底栏 help 提示 |
| [`transcript_log.py`](transcript_log.py) | `TranscriptLog`：监听 `scroll_y`，同步 `_transcript_follow_tail` / `auto_scroll` |
| [`prompt_text_area.py`](prompt_text_area.py) | 两行 `TextArea` 输入（Enter 发送，Shift+Enter 换行，Esc 取消当前 turn） |
| [`welcome_panel.py`](welcome_panel.py) | `build_welcome_panel()`：连接后写入 transcript 的欢迎 Panel |
| [`approval_screen.py`](approval_screen.py) | 旧工具审批 Modal（保留备用） |

## 展示约定

- **assistant usage**：完成态独占一行、`Align.right` 右对齐（避免空格 padding 被 Rich fold 拆开）；`USAGE` SSE 晚到时 retroactive 重写最近一条已完成 assistant 块（对齐 Go `ApplyRoundUsage`）。
- **滚动跟随**：用户上滚后流式输出不拽底；滚回底部或发消息恢复跟随。
