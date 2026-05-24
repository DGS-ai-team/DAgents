# app/cli — 终端客户端（Textual TUI）

| 文件/目录 | 用途 |
|---|---|
| [`main.py`](main.py) | `dagents` 命令入口：子命令解析、`chat` 启动 TUI |
| [`chat.py`](chat.py) | `run_chat`：构造 `SessionController` 并启动 Textual App |
| [`session_controller.py`](session_controller.py) | SSE pump、后台 render 循环、用户 turn 栅栏、trigger 绑定 |
| [`api_client.py`](api_client.py) | HTTP/SSE 客户端（session、message、stream、trigger CRUD） |
| [`approval.py`](approval.py) | 工具审批载荷解析与 resume 决策构造 |
| [`session_commands.py`](session_commands.py) | `dagents show session` / `dagents delete session` |
| [`tui/`](tui/) | Textual UI：主 App、审批 Modal |

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
| `/status` | 显示 api/session/client/sse 状态 |
| `/bind-triggers` | 将当前 session 的 trigger 绑定到本 client_id |
| `/exit` | 退出 |

## 架构要点

- **长连 SSE**：`_pump_stream` 后台入队，`_render_loop` 持续渲染到 RichLog（触发器/后台 turn 可实时可见）。
- **用户 turn 栅栏**：`submit_message` + `wait_user_turn`；在 submit 后见到内容事件之前的 `done` 被忽略（如在途 trigger 收尾）。
