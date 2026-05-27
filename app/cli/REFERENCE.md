# app/cli — 符号索引

## `main.py`

- **`main`** / **`build_parser`**：CLI 子命令入口
- **`_default_api_base`**：从 `.env` 读取 API 地址
- **`_normalize_serve_extra`**：剥离 `serve` 的 `REMAINDER` 中 `--` 前缀

## `daemon.py`

- **`add_serve_arguments`**：`serve` / `api` 的 `--foreground` / `--stop` / `--status` / `--no-hooks` / `--no-wait`
- **`run_serve_command`**：后台启动、停止、状态、前台运行；默认写 `dagents-api.pid` 与 `logs/dagents-api.log`
- **`_run_hook_dir`** / **`_run_hook_script`**：`.runtime/scripts/serve/startup.d` 与 `shutdown.d` 钩子
- **`_start_serve_daemon`** / **`_run_api_foreground`**：后台 `Popen` 或前台 `subprocess.call`

## `chat.py`

- **`run_chat`**：启动 Textual TUI（`asyncio.run` + `DAgentsTuiApp.run_async`）

## `session_controller.py`

- **`SessionController`**：会话生命周期、SSE pump/render、turn 栅栏、trigger 绑定
  - **`start` / `stop`**：连接后端、启停后台任务
  - **`submit_message` / `wait_user_turn` / `cancel_current_turn`**：用户消息、轮次等待与在途 turn 取消
  - **`list_sessions`**：调用 `GET /v1/sessions`
  - **`get_context`**：调用 `GET /v1/sessions/{session_id}/context`
  - **`list_skills` / `load_skill` / `unload_skill`**：调用 session skills API
  - **`clear_context`**：调用 `POST .../clear-context`
  - **`bind_triggers_to_client`**：PATCH 同 session trigger 的 `client_id`
  - **`on_transcript` / `on_approval` / `on_status`**：UI 回调注册

## `api_client.py`

- **`StreamEvent`**：SSE 解析结果（`event_type`、`session_id`、`data`）
- **`DAgentsApiClient`**：health、session、message、resume、cancel、stream、session list/delete/clear-context/context、session skills、trigger list/patch
- **`_parse_sse_block`**：SSE block → `StreamEvent`

## `approval.py`

- **`ToolApprovalRequest`** / **`ApprovalDecision`** / **`ApprovalCancelled`**：审批模型与用户取消信号
- **`extract_tool_approval_requests`**：从 SSE `approval_required.data` 提取工具列表
- **`build_*_decision`** / **`parse_selection_tokens`**：resume 决策构造

## `session_commands.py`

- **`run_show_session`** / **`run_delete_session`**：`dagents show session` 与 `dagents delete session`
- **`_render_session_list`**：终端表格输出

## `render.py`

- **`TranscriptKind`** / **`TranscriptUpdate`**：transcript 更新类型
- **`format_*`**：各 SSE 事件 → 格式化文本

## `version_info.py`

- **`CLI_VERSION`**：CLI 展示版本（与 API `0.1.0` 对齐）
- **`get_cli_username`** / **`get_cli_version`**：欢迎区用户名与版本

## `tui/app.py`

- **`DAgentsTuiApp`**：Textual 主界面（`theme=textual-light` + 顶栏 SSE/session + RichLog/context 视图 + 底栏 help 提示）
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
