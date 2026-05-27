# app/cli — 终端客户端（Textual TUI）

| 文件/目录 | 用途 |
|---|---|
| [`main.py`](main.py) | `dagents` 命令入口：子命令解析、`chat` 启动 TUI |
| [`chat.py`](chat.py) | `run_chat`：构造 `SessionController` 并启动 Textual App |
| [`session_controller.py`](session_controller.py) | SSE pump、后台 render 循环、用户 turn 栅栏、trigger 绑定 |
| [`api_client.py`](api_client.py) | HTTP/SSE 客户端（session、message、stream、trigger CRUD） |
| [`approval.py`](approval.py) | 工具审批载荷解析与 resume 决策构造 |
| [`session_commands.py`](session_commands.py) | `dagents show session` / `dagents delete session` |
| [`tui/`](tui/) | Textual UI：主 App、进入欢迎区、审批 Modal |
| [`version_info.py`](version_info.py) | CLI 版本号与用户名解析 |

## 使用

```bash
dagents chat [--api URL] [--session ID] [--client-id ID] [--show-reasoning]
dagents show session [--api URL]
dagents delete session SESSION_ID [--api URL]
```

恢复会话：在 `show session` 输出中找到目标 `session_id`，然后 `dagents chat --session SESSION_ID`。

## 命令（TUI 输入框内）

| 命令 | 说明 |
|---|---|
| `/help` | 帮助 |
| `/status` | 在聊天记录中输出 api/session/client/sse（顶栏同步刷新 SSE/session） |
| `/context` | 打开当前 session 的 context 摘要视图；按 `Esc` 返回聊天记录 |
| `/session` | 查询当前队列中的 active session |
| `/skill` | 展示当前 loaded skills 与 available skills |
| `/skill load NAME` | 向当前会话加载一个 skill |
| `/skill unload NAME` | 从当前会话卸载一个 skill |
| `/bind-triggers` | 将当前 session 的 trigger 绑定到本 client_id |
| `/clear` | 清空服务端 context 并清 transcript |
| `/exit` | 退出，并在终端打印恢复当前会话的 `dagents chat --session ...` 命令 |

快捷键：context 视图中按 `Esc` 返回聊天记录；输出中或工具审批中按 `Esc` 可调用取消接口中断当前 turn。

## 架构要点

- **TUI 主题**：固定 `textual-light` 亮色（`DAgentsTuiApp.theme`），不跟随终端配色。
- **进入欢迎区**：连接成功后 `build_welcome_panel()` 以 Rich `Panel` 写入 RichLog，随消息一起滚动；`/clear` 清屏时一并清除；不再重复连接/ session 行。
- **消息样式**：用户、assistant、tool 消息使用固定圆点列；流式/执行中为黄点，完成为绿点；`/context` 会临时隐藏聊天 RichLog 并展示只读 context 摘要。
- **等待状态**：用户消息提交成功后显示 `prefilling... Ns`，首条内容到达后冻结为 `done`；reasoning 到达时显示 `thinking... Ns`。
- **工具审批**：不弹窗，隐藏输入框并在 RichLog 对应工具下方逐条展示审批选项；每个工具单独批准/拒绝，上下键选择、Enter 确认，`Esc` 取消当前 turn 后恢复输入框。
- **工具耗时**：工具执行中黄点占位行显示动态耗时（如 `bash(...)... 1.2s`），完成后绿点结果标题展示总耗时（含审批等待时间）。
- **长连 SSE**：`_pump_stream` 后台入队，`_render_loop` 持续渲染到 RichLog（触发器/后台 turn 可实时可见）。
- **用户 turn 栅栏**：`submit_message` + `wait_user_turn`；在 submit 后见到内容事件之前的 `done` 被忽略（如在途 trigger 收尾）。
