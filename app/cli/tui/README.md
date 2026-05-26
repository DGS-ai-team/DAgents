# app/cli/tui — Textual 聊天界面

| 文件 | 用途 |
|---|---|
| [`app.py`](app.py) | 主 App：顶栏 SSE/session、聊天 RichLog、`/context` 摘要视图、底栏 help 提示 |
| [`prompt_text_area.py`](prompt_text_area.py) | 两行 `TextArea` 输入（Enter 发送，Shift+Enter 换行，Esc 取消当前 turn） |
| [`welcome_panel.py`](welcome_panel.py) | `build_welcome_panel()`：连接后写入 RichLog 的欢迎 Panel |
| [`approval_screen.py`](approval_screen.py) | 旧工具审批 Modal（保留备用） |
