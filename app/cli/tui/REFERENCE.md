# app/cli/tui — 符号索引

## `app.py`

- **`DAgentsTuiApp`**：Textual 聊天主应用
- **`_write_welcome_panel`**：连接成功后向 RichLog 写入欢迎 Panel
- **`_assistant_block`** / **`_write_assistant_block`**：assistant 流式/完成态渲染；完成态 Table 正文列 `overflow=fold` 避免 ellipsis 截断
- **`_transcript_base_lines`**：欢迎 Panel 占用的行数下限（流式回退）
- **`_exit_with_resume_hint`**：`/exit` 退出后打印 `dagents chat --session ...` 会话恢复命令
- **`_rich_code_box`** / **`_tool_dot_block`**：Rich Panel + Syntax 代码框与圆点对齐布局
- **`_tool_display_name`** / **`_tool_call_parts_from_call`**：工具调用短标题与可选代码框正文；`write_file` / 过长 `bash_run` 用代码框展示全文
- **`_write_tool_call`** / **`_write_tool_result`**：多工具逐条占位与结果重写；执行中黄点行动态显示耗时，完成后绿点标题展示总耗时
- **`_format_tool_elapsed`** / **`_animate_tool_pending`** / **`_cancel_tool_pending_tasks`**：工具执行耗时格式化与占位行动画
- **`_start_status_line`** / **`_finish_status_line`**：`prefilling/thinking` 等待状态行
- **`_cancel_current_turn`** / **`_cancel_current_turn_request`**：Esc 触发当前 turn 取消；审批中会中断 approval future，避免继续 submit resume
- **`_show_context_view` / `_enter_context_view` / `_exit_context_view` / `_format_context_state`**：`/context` 命令展示当前 context 摘要；进入时隐藏聊天 RichLog，Esc 返回
- **`action_toggle_tool_result`**：点击工具结果摘要展开/收起
- **`_process_hitl_queue`** / **`_run_approval_hitl`** / **`_run_user_info_hitl`**：非阻塞 HITL 队列处理
- **`_refresh_input_strip`** / **`_show_children`**：子 Agent 状态条与 `/children` 命令
- **`_begin_approval_ui`** / **`_end_approval_ui`**：通过 Textual UI 队列初始化/清理 RichLog 内审批交互；子任务审批显示青色标题
- **`_write_approval_block`** / **`_render_approval_block`** / **`_delete_approval_block`**：RichLog 内逐工具审批选项块
- **`_refresh_approval_layout`**：审批块增删后刷新布局并滚动到底部
- **`confirm_approval_choice`**：确认 RichLog 内审批选项当前选择
- **`_show_sessions`**：`/session` 命令查询并展示当前队列中的 active sessions
- **`_handle_skill_command`** / **`_format_skill_state`** / **`_skill_state_block`**：`/skill` 命令；列表按 transcript 宽度折行（`expand=True` + `overflow=fold`）
- **`_transcript_content_width`**：RichLog 可用列宽（欢迎 Panel、skill 块等共用）

## `welcome_panel.py`

- **`build_welcome_panel`**：构造 Rich `Panel` 欢迎内容

## `prompt_text_area.py`

- **`PromptTextArea`**：聊天输入 TextArea（Enter → `submit_prompt`；Esc → App 取消当前 turn）

## `approval_screen.py`

- **`ApprovalScreen`**：旧工具审批 Modal
