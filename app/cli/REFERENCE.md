# app/cli — 符号索引

## `main.py`

- **`main`** / **`build_parser`**：CLI 子命令入口
- **`_default_api_base`**：无 YAML 时从 env/.env 解析 API 地址
- **`_add_client_config_arguments`** / **`apply_client_settings`**：`--config` / `--api` 合并
- **`_normalize_serve_extra`**：剥离 `serve` 的 `REMAINDER` 中 `--` 前缀

## `config_file.py`

- **`AgentClientConfig`**：Python 侧共用配置子集（`api_base`）
- **`resolve_config_path`** / **`resolve_agent_id`** / **`load_agent_client_config`** / **`resolve_client_settings`**：与 Go `ResolveConfigPath` / `ResolveAgentID` 对齐

## `daemon.py`

- **`add_serve_arguments`**：`serve` / `api` 的 `--foreground` / `--stop` / `--status` / `--no-hooks` / `--no-wait`
- **`run_serve_command`**：后台启动、停止、状态、前台运行；默认写 `dagents-api.pid` 与 `logs/dagents-api.log`
- **`_run_hook_dir`** / **`_run_hook_script`**：`.runtime/scripts/serve/startup.d` 与 `shutdown.d` 钩子
- **`_start_serve_daemon`** / **`_run_api_foreground`**：后台 `Popen` 或前台 `subprocess.call`

## `chat.py`

- **`run_chat`**：启动 Textual TUI（`asyncio.run` + `DAgentsTuiApp.run_async`）

## `session_controller.py`

- **`SessionController`**：会话生命周期、SSE pump/render、turn 栅栏
  - **`start` / `stop`**：连接 Node、启停后台任务
  - **`submit_message` / `wait_user_turn` / `cancel_current_turn`**：用户消息、轮次等待与在途 turn 取消
  - **`list_sessions`**：调用 `GET /v1/sessions`
  - **`get_context`**：调用 `GET /v1/sessions/{session_id}/context`
  - **`list_skills` / `load_skill` / `unload_skill`**：调用 session skills API
  - **`clear_context`**：调用 `POST .../clear-context`
  - **`on_transcript` / `on_approval` / `on_user_information` / `on_status`**：UI 回调注册

## `user_information.py`

- **`UserInformationRequest`** / **`UserInformationAnswer`**：SSE 询问与用户回答模型
- **`extract_user_information_request`**：解析 `user_information_required` 载荷
- **`build_answer_from_text`** / **`build_answer_from_options`**：构造 resume 回答
- **`UserInformationCancelled`**：用户 Esc 取消信号

## `api_client.py`

- **`StreamEvent`**：SSE 解析结果（`event_type`、`session_id`、`data`）
- **`DAgentsApiClient`**：Agent Node HTTP/SSE（health、session、message、resume、cancel、stream、skills、context）
- **`_parse_sse_block`**：SSE block → `StreamEvent`

## `approval.py`

- **`ToolApprovalRequest`** / **`ApprovalDecision`** / **`ApprovalCancelled`**：审批模型与用户取消信号
- **`extract_tool_approval_requests`**：从 SSE `approval_required.data` 提取工具列表
- **`build_*_decision`** / **`parse_selection_tokens`**：resume 决策构造

## `session_commands.py`

- **`run_show_session`** / **`run_delete_session`**：`dagents show session` 与 `dagents delete session`
- **`_render_session_list`**：终端表格输出

## `tool_calls.py`

- **`parse_tool_arguments`**：将 JSON 字符串或 dict 解析为 tool arguments dict
- **`normalize_tool_call_item`**：OpenAI `function.name` / 扁平 Node 格式统一为 `{id, name, arguments}`

## `render.py`

- **`TranscriptKind`** / **`TranscriptUpdate`**：transcript 更新类型
- **`format_*`**：各 SSE 事件 → 格式化文本

## `version_info.py`

- **`CLI_VERSION`**：CLI 展示版本（与仓库标记版本 `0.2.0` 对齐）
- **`get_cli_username`** / **`get_cli_version`**：欢迎区用户名与版本

## `tui/app.py`

- **`DAgentsTuiApp`**：Textual 主界面（`theme=textual-dark` + 顶栏 SSE 状态 + RichLog/context 视图 + 底栏 help 提示）
- **`_write_welcome_panel`** / **`_transcript_base_lines`**：连接后 Rich Panel 欢迎区与流式回退边界
- **`_message_block`** / **`_assistant_block`** / **`_event_block`**：统一消息圆点与正文对齐；assistant 完成态用 Markdown 渲染
- **`_start_status_line`** / **`_finish_status_line`** / **`_animate_status_line`**：`prefilling/thinking` 等待状态行
- **`_write_tool_call`** / **`_write_tool_result`** / **`action_toggle_tool_result`**：多工具逐条黄点占位、结果绿点重写、点击展开/收起
- **`submit_prompt`**：`PromptTextArea` Enter 提交入口
- **`_show_context_view` / `_enter_context_view` / `_exit_context_view` / `_format_context_state`**：`/context` 只读摘要视图，隐藏聊天 RichLog，Esc 返回
- **`_write_approval_block`** / **`confirm_approval_choice`**：RichLog 内逐工具审批选项展示与确认
- **`_apply_top_status`**：顶栏右侧 SSE 文案 + ● + session_id
- **`_finish_assistant_stream`** / **`_rewind_assistant_stream_lines`**：assistant 流式输出缓冲与 RichLog 行回退重写

## `tui/welcome_panel.py`

- **`build_welcome_panel`**：生成进入时 Rich `Panel`（版本、欢迎语、用户名、backend、session、风险提示）

## `tui/approval_screen.py`

- **`ApprovalScreen`**：工具审批 Modal（全批准/全拒绝/选择）
