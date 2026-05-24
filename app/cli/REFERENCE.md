# app/cli — 符号索引

## `main.py`

- **`main`** / **`build_parser`**：CLI 子命令入口
- **`_default_api_base`**：从 `.env` 读取 API 地址

## `chat.py`

- **`run_chat`**：启动 Textual TUI（`asyncio.run` + `DAgentsTuiApp.run_async`）

## `session_controller.py`

- **`SessionController`**：会话生命周期、SSE pump/render、turn 栅栏、trigger 绑定
  - **`start` / `stop`**：连接后端、启停后台任务
  - **`submit_message` / `wait_user_turn`**：用户消息与轮次等待
  - **`bind_triggers_to_client`**：PATCH 同 session trigger 的 `client_id`
  - **`on_transcript` / `on_approval` / `on_status`**：UI 回调注册

## `api_client.py`

- **`StreamEvent`**：SSE 解析结果（`event_type`、`session_id`、`data`）
- **`DAgentsApiClient`**：health、session、message、resume、stream、session list/delete、trigger list/patch
- **`_parse_sse_block`**：SSE block → `StreamEvent`

## `approval.py`

- **`ToolApprovalRequest`** / **`ApprovalDecision`**：审批模型
- **`extract_tool_approval_requests`**：从 SSE `approval_required.data` 提取工具列表
- **`build_*_decision`** / **`parse_selection_tokens`**：resume 决策构造

## `session_commands.py`

- **`run_show_session`** / **`run_delete_session`**：`dagents show session` 与 `dagents delete session`
- **`_render_session_list`**：终端表格输出

## `render.py`

- **`TranscriptKind`** / **`TranscriptUpdate`**：transcript 更新类型
- **`format_*`**：各 SSE 事件 → 格式化文本

## `tui/app.py`

- **`DAgentsTuiApp`**：Textual 主界面（RichLog + Input + Status + Footer）

## `tui/approval_screen.py`

- **`ApprovalScreen`**：工具审批 Modal（全批准/全拒绝/选择）
