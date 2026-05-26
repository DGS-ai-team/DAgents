# `app/context/` REFERENCE

## `models.py`

- 枚举：
  - `RunTurnPhase`（`idle` / `branch_resolving` / `model_streaming` / `awaiting_tool_execution`）
  - `SummaryCompressionPhase`（供 summary runtime 内部使用）
- 模型：
  - `MessageRecord`：单条消息记录（`role/content/meta`）
  - `ConversationContext`：可持久化会话上下文（`history/openai_messages/pending_tool_calls/run_turn_phase/messages_total_tokens/tool_loop_count/loaded_skills`；每项为 `skill_name/description`）
  - `PendingToolCall`：待执行/待审批工具规格
  - `OpenAIConversationContext`：runtime 可变推理上下文（`messages/pending_tool_calls/run_turn_phase/messages_total_tokens/tool_loop_count/loaded_skills/sse_client_id`，每项为 `skill_name/description`）；**`sse_client_id`** 为进程内最近入站 **`client_id`**，供异步工具提交与 SSE 路由；**`messages`** 条数由 **`metrics.refresh_session_context_metrics`** 暴露到 Prometheus（**`dagents_session_context_messages_count`**）
- 辅助函数：
  - `_json_safe_deep()`：JSON 安全深拷贝
  - `_openai_messages_to_message_records()`：OpenAI 消息派生 `MessageRecord` 列表
- 关键方法（类方法/实例方法）：
  - `ConversationContext.add_turn()`
  - `ConversationContext.unpack_for_openai_runtime()`
  - `ConversationContext.from_openai_runtime()`
  - `OpenAIConversationContext.from_conversation_context()`
  - `OpenAIConversationContext.to_conversation_context()`
  - `OpenAIConversationContext.reset_conversation_state()`：清空对话态，保留 `session_id` / `sse_client_id` / `loaded_skills`
  - `OpenAIConversationContext.normalized_run_turn_phase_for_persist()`

## `openai_messages.py`

- **`normalize_reasoning_content()`**：将模型/历史中的 `reasoning_content` 规范为字符串，`None` 转为空串。
- **`latest_assistant_reasoning_content()`**：从 OpenAI messages 尾部读取最近 assistant 的 `reasoning_content`。
- **`normalize_openai_message_for_context()`**：`ctx.messages` 写入前的统一规范化入口；对 `assistant + tool_calls` 保证 `reasoning_content` 字段存在，异步 `tool_callback` 可继承最近 assistant reasoning。
