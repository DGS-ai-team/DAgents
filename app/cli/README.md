# app/cli — 终端客户端（Textual TUI）

| 文件/目录 | 用途 |
|---|---|
| [`main.py`](main.py) | `dagents` 命令入口：子命令解析、`chat` 启动 TUI |
| [`config_file.py`](config_file.py) | 与 Go 共用的 YAML 配置加载 |
| [`chat.py`](chat.py) | `run_chat`：构造 `SessionController` 并启动 Textual App |
| [`session_controller.py`](session_controller.py) | SSE pump、后台 render 循环、用户 turn 栅栏 |
| [`api_client.py`](api_client.py) | Agent Node HTTP/SSE 客户端 |
| [`approval.py`](approval.py) | 工具审批载荷解析与 resume 决策构造 |
| [`child_agent.py`](child_agent.py) | 子 Agent SSE 过滤、生命周期文案、`ChildAgentTracker` |
| [`user_information.py`](user_information.py) | `ask_user_information` SSE 解析与用户回答 resume 构造 |
| [`session_commands.py`](session_commands.py) | `dagents show session` / `dagents delete session` |
| [`tui/`](tui/) | Textual UI：主 App、进入欢迎区、审批 Modal |
| [`version_info.py`](version_info.py) | CLI 版本号与用户名解析 |

## 使用

**本地助手（连 Go Node）：**

```bash
cp packaging/agent-client/config.example.yaml packaging/agent-client/config.yaml
dagents chat
```

```bash
dagents chat [--config PATH] [--api URL] [--session ID] [--client-id ID] [--show-reasoning]
dagents show session [--config PATH] [--api URL]
dagents delete session SESSION_ID [--config PATH] [--api URL]
```

配置查找顺序：`--config` → `DAGENTS_CONFIG` → `packaging/agent-client/config.yaml`。  
无 YAML 时 `--api` 回退 `DAGENTS_NODE_ENDPOINT`（默认 `http://127.0.0.1:18765`）。

恢复会话：在 `show session` 输出中找到目标 `session_id`，然后 `dagents chat --session SESSION_ID`。

双 Client 选型见 [local-assistant.md](../../docs/architecture/local-assistant.md)。

## 命令（TUI 输入框内）

| 命令 | 说明 |
|---|---|
| `/help` | 帮助 |
| `/status` | 在聊天记录中输出 api/session/client/sse |
| `/context` | 打开当前 session 的 context 摘要视图；按 `Esc` 返回聊天记录 |
| `/session` | 查询当前队列中的 active session |
| `/skill` | 展示 loaded/available skills |
| `/skill load NAME` | 加载 skill |
| `/skill unload NAME` | 卸载 skill |
| `/children` | 列出当前 session 下活跃子 Agent |
| `/clear` | 清空服务端 context 并清 transcript |
| `/exit` | 退出，并在终端打印恢复当前会话的 `dagents chat --session ...` 命令 |

`dagents serve`（非 TUI 内命令，在 shell 中执行）：

| 命令 | 说明 |
|------|------|
| `dagents serve` | 后台启动 Agent API（默认），日志写入 `logs/dagents-api.log`，PID 写入 `dagents-api.pid` |
| `dagents serve --stop` | 停止后台 API，并执行 `.runtime/scripts/serve/shutdown.d/` 钩子 |
| `dagents serve --status` | 查看后台 API 是否在运行 |
| `dagents serve --foreground` | 前台运行（调试，不写 PID） |
| `dagents serve --no-hooks` | 跳过 startup/shutdown 钩子 |

钩子目录见 `packaging/runtime/scripts/serve/`（安装后位于 `.runtime/scripts/serve/`）。

快捷键：context 视图中按 `Esc` 返回聊天记录；输出中、工具审批或 Agent 询问中按 `Esc` 可调用 cancel 中断当前 turn。

**Agent 询问（`ask_user_information`）**：Agent 调用该工具时，TUI 会展示问题；无选项时在底部输入框输入后 Enter 提交；有选项时用 ↑/↓ 选择、Space 多选切换、Enter 确认。

## 架构要点

- **唯一运行时**：TUI 只连 Go Agent Node；SSE 按 `session_id` 订阅。
- **TUI 主题**：固定 `textual-dark` 暗色（`DAgentsTuiApp.theme`），不跟随终端配色。
- **进入欢迎区**：连接成功后 `build_welcome_panel()` 以 Rich `Panel` 写入 RichLog，随消息一起滚动；`/clear` 清屏时一并清除。
- **长连 SSE**：`_pump_stream` 后台入队，`_render_loop` 持续渲染到 RichLog；子 Agent turn 的 assistant/tool 等事件被过滤，仅展示审批与生命周期系统行。
- **HITL 非阻塞**：`approval_required` / `user_information_required` 入队后由 TUI 异步处理，避免阻塞 SSE 消费。
- **子 Agent 状态条**：输入框上方 `#input-strip` 展示活跃子 Agent 数与待审批数。
- **用户 turn 栅栏**：`submit_message` + `wait_user_turn`；在 submit 后见到内容事件之前的 `done` 被忽略。
