# `app/harness/cli/` REFERENCE

## `main.py`

- **`main`**：CLI 主入口（加载环境并进入 HTTP 交互模式）。
- **`_parse_cli_line`**：将一行解析为用户消息或斜杠指令（**`/yes`** / **`/no`** / **`/cancel`** / **`/cancel …`**）；未知 **`/`** 前缀抛 **`ValueError`**。
- **`_use_prompt_toolkit_layout`**：是否 stdin/stdout 均为 TTY。
- **`_stdin_pump`**、**`_wait_line_or_stream_end`**：非 TTY 时队列读行；TTY 时 **`prompt_async`** 与 SSE **`asyncio.wait` 并发**；**`patch_stdout`** 下输出在输入行之上。
- **`_consume_stream_print`**：仅消费 SSE 并写 stdout（供 **`asyncio.create_task`**）。
- **`_format_event`**：将统一流事件格式化为终端输出（前缀 `[事件类型]`）；**`usage`** 在 **`_consume_stream_print`** 中忽略不打印。
- **`_ensure_stream_category_prefix`**、**`_end_stream_if_any`**：`assistant` / `reasoning` 流式块首段打标签、同类型分片拼接、切换类别或结构化事件前换行。
- **`_extract_approval_payload`**：从流事件中提取 `approval_required` 审批数据。
- **`_async_resume_decision`**：审批阶段读 **`> `**；**`/yes`** / **`/no`** 返回 **`resume`** 字典，**普通用户消息**返回 **`str`** 由外层 **`submit_user_text`** 提交并清空待批列表（**`_wait_line_or_stream_end(None, …)`**）。
- **`CLI_CMD_YES`**、**`CLI_CMD_NO`**、**`CLI_CMD_CANCEL`**：斜杠指令字面量。
- **`_run_http_cli`**：HTTP CLI 主循环；**`pending_stream_rids`** 保存「已 submit、尚未订阅 SSE」的 **`request_id`**（FIFO）；**`start_next_stream_from_pending`** 在当前无在跑消费 task 时 **`popleft`** 并 **`_consume_stream_print`**；**`submit_user_text(..., interrupt=False)`** 在有在途 SSE 时只入队不打断；**`interrupt=True`**（**`/cancel …`**）先入队再 **`cancel_current_turn`** 并接续 pending；**`handle_cancel`**（**`/cancel`**）仅 cancel 后同样 **`start_next_stream_from_pending`**。
