# `app/context/` REFERENCE

## `models.py`

- 枚举：
  - `RunTurnPhase`（`idle` / `branch_resolving` / `model_streaming` / `awaiting_tool_execution`）
  - `SummaryCompressionPhase`（供 summary runtime 内部使用）
- 模型：
  - `MessageRecord`：单条消息记录（`role/content/meta`）
  - `ConversationContext`：可持久化会话上下文（`history/openai_messages/pending_tool_calls/run_turn_phase/messages_total_tokens/tool_loop_count/loaded_skills`）
  - `PendingToolCall`：待执行/待审批工具规格
  - `OpenAIConversationContext`：runtime 可变推理上下文（`messages/pending_tool_calls/run_turn_phase/messages_total_tokens/tool_loop_count/loaded_skills`）
- 辅助函数：
  - `_json_safe_deep()`：JSON 安全深拷贝
  - `_openai_messages_to_message_records()`：OpenAI 消息派生 `MessageRecord` 列表
- 关键方法（类方法/实例方法）：
  - `ConversationContext.add_turn()`
  - `ConversationContext.unpack_for_openai_runtime()`
  - `ConversationContext.from_openai_runtime()`
  - `OpenAIConversationContext.from_conversation_context()`
  - `OpenAIConversationContext.to_conversation_context()`
  - `OpenAIConversationContext.normalized_run_turn_phase_for_persist()`
