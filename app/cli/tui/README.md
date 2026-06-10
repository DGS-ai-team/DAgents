# app/cli/tui — Textual 聊天界面

| 文件 | 用途 |
|---|---|
| [`app.py`](app.py) | 主 App：顶栏 SSE 状态、聊天 transcript（`TranscriptLog`）、`/context` / `/policy` 全屏视图、底栏 help 提示 |
| [`policy_view.py`](policy_view.py) | `/policy` 状态与渲染（过滤、Tab、三档决策） |
| [`transcript_log.py`](transcript_log.py) | `TranscriptLog`：监听 `scroll_y`，同步 `_transcript_follow_tail` / `auto_scroll` |
| [`prompt_text_area.py`](prompt_text_area.py) | 两行 `TextArea` 输入（Enter 发送，Shift+Enter 换行，Esc 取消当前 turn） |
| [`welcome_panel.py`](welcome_panel.py) | `build_welcome_panel()`：连接后写入 transcript 的欢迎 Panel |
| [`approval_screen.py`](approval_screen.py) | 旧工具审批 Modal（保留备用） |

## 展示约定

- **assistant usage**：完成态独占一行、`Align.right` 右对齐（避免空格 padding 被 Rich fold 拆开）；`USAGE` SSE 晚到时 retroactive 重写最近一条已完成 assistant 块（对齐 Go `ApplyRoundUsage`）。
- **滚动跟随**：用户上滚后流式输出不拽底；滚回底部或发消息恢复跟随。
- **trigger 审批**：`trigger_create` / `trigger_fire` 在 SSE `approval_mode=trigger_session` 时展示四选项（同会话 / 新会话 / 最新活跃 / 不同意），resume 写入 `trigger_session_targets`。
- **`/policy`**：全屏工具/shell 黑白名单；Esc 返回 · Tab 切页 · `1/2/3` 改档位 · Enter 应用 · `[`/`]` 切换 shell · `a` 显示全部 shell 项。
