# app/cli — 终端客户端（Textual TUI）

| 文件/目录 | 用途 |
|---|---|
| [`main.py`](main.py) | `dagents` 命令入口：子命令解析、`chat` 启动 TUI |
| [`config_file.py`](config_file.py) | 与 Go 共用的 YAML 配置加载 |
| [`chat.py`](chat.py) | `run_chat`：构造 `SessionController` 并启动 Textual App |
| [`log.py`](log.py) | CLI 落盘日志（`logs/session_controller.log`） |
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
| `/help` | 帮助（Panel，中文） |
| `/status` | agent、session、队列与 turn 状态 |
| `/context` | 只读 context 视图；`Esc` 返回 |
| `/policy` | 工具/shell 策略管理；`Esc` 返回 |
| `/triggers` | 查看已配置触发器 |
| `/compress` | 手动触发阻塞压缩 |
| `/session` | 列出 session（亦可用 `/sessions`） |
| `/switch <id>` | 切换 session（重连 SSE） |
| `/new` | 新建 session |
| `/skill` | skills 列表；`/skill load\|unload NAME` |
| `/children` | 子 Agent 列表 |
| `/reasoning on\|off` | 推理流显示（亦可用 `--show-reasoning` 启动） |
| `/clear` | 清空服务端 context 与 transcript |
| `/exit` | 退出，并打印 `dagents chat --session ...` 恢复命令 |

快捷键：context / policy 视图中 `Esc` 返回；**流式输出、审批或 Agent 询问中 `Esc` 取消在途 turn**（无 `/cancel` 斜杠命令）。

**滚动**：transcript 默认贴底跟随；**滚轮上滚** 后流式输出、审批等待、**点击展开工具详情** 不会强制跳底；滚回底部或发送消息后恢复。Go Client 另支持 **PgUp/PgDn**。

**Agent 询问（`ask_user_information`）**：TUI 将 `tool_call` 与 `user_information_required` 合并为一条「Agent 询问」块（问题 + 选项）。无选项时在底部输入框输入后 Enter；有选项时 ↑/↓、Space 多选、Enter 确认。`done` 表示轮到用户（见 [agent-node-api.md §2.4.1](../../docs/architecture/agent-node-api.md)）。

## 架构要点

- **唯一运行时**：TUI 只连 Go Agent Node；SSE 按 `session_id` 订阅。
- **TUI 主题**：固定 `textual-dark` 暗色（`DAgentsTuiApp.theme`），不跟随终端配色。
- **进入欢迎区**：连接成功后 `build_welcome_panel()` 以 Rich `Panel` 写入 RichLog，随消息一起滚动；`/clear` 清屏时一并清除。
- **长连 SSE**：`_pump_stream` 后台入队，`_render_loop` 持续渲染到 RichLog；子 Agent turn 的 assistant/tool 等事件被过滤，仅展示审批与生命周期系统行。
- **HITL 非阻塞**：`approval_required` / `user_information_required` 入队后由 TUI 异步处理，避免阻塞 SSE 消费。
- **子 Agent 状态条**：输入框上方 `#input-strip` 展示活跃子 Agent 数与待审批数。
- **用户 turn 栅栏**：`submit_message` + `wait_user_turn`；`done` 仅语义 B（编排暂停/链结束），含 `turn_complete` 与 `awaiting`；HITL 暂停的 `done` 正常唤醒；submit 前在途 turn 的陈旧 `done`（seq ≤ fence）被忽略。
