# `app/observability/` REFERENCE

## `metrics.py`

- **`LLM_PROMPT_TOKENS`**、**`LLM_COMPLETION_TOKENS`**：Prometheus **`Gauge`**，label **`model`**（**`record_llm_token_usage`** 使用 **`set`**）
- **`sanitize_model_label`**：模型名 → 安全 label
- **`SESSION_CONTEXT_MESSAGES_COUNT`**：各 session 的 **`OpenAIConversationContext.messages`** 条数（label **`session_id`**）
- **`sanitize_prometheus_label_value`**：通用 label 净化（**`session_id`** 等）
- **`refresh_session_context_metrics`**：由 **`AgentService`** 在 **`_handle_message` finally**、**`create_session`**、**`_resolve_context` 首次装入缓存**、**`release_session`**、**`_evict_session_for_capacity`**、**`stop`** 后调用
- **`parse_usage_tokens`**：从 SDK `usage` 或 dict 解析 `(prompt_tokens, completion_tokens)`
- **`usage_fields_from_openai_usage`**：解析 **`total_tokens`** 并返回可序列化 dict（供 SSE **`usage`** 事件）
- **`record_llm_token_usage`**：将一次 **`usage`** 的 prompt/completion 写入 Gauge（**`set`**）
- **`metrics_text`**：返回 `(bytes, content_type)` 供 **`/metrics`**
