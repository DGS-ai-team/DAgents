# `app/observability/` REFERENCE

## `metrics.py`

- **`LLM_PROMPT_TOKENS`**、**`LLM_COMPLETION_TOKENS`**：Prometheus **`Counter`**（指标名 **`dagents_llm_*_total`**），label **`model`**（**`record_llm_token_usage`** 对每次 **`usage` 分片 `inc`**，进程内累计）
- **`LLM_PROMPT_AUDIO_TOKENS`**、**`LLM_PROMPT_CACHED_TOKENS`**、**`LLM_PROMPT_CACHE_HIT_TOKENS`**、**`LLM_PROMPT_CACHE_MISS_TOKENS`**：同上，**`usage`** 中 prompt 明细/cache 按次 **`inc`**
- **`record_llm_token_usage`**：将一次 **`usage`** 的各字段以增量写入 Counter（**`inc`**）；依赖上游为「按请求」口径（见 **`metrics.py`** 模块说明）
- **`sanitize_model_label`**：模型名 → 安全 label
- **`SESSION_CONTEXT_MESSAGES_COUNT`**：各 session 的 **`OpenAIConversationContext.messages`** 条数（label **`session_id`**）
- **`sanitize_prometheus_label_value`**：通用 label 净化（**`session_id`** 等）
- **`refresh_session_context_metrics`**：由 **`AgentService`** 在 **`_handle_message` finally**、**`create_session`**、**`_resolve_context` 首次装入缓存**、**`release_session`**、**`_evict_session_for_capacity`**、**`stop`** 后调用
- **`parse_usage_tokens`**：从 SDK `usage` 或 dict 解析 `(prompt_tokens, completion_tokens)`
- **`parse_usage_prompt_cache_details`**：从 **`usage`** 解析 **`prompt_tokens_details`** 与 **`prompt_cache_*`**
- **`usage_fields_from_openai_usage`**：解析 **`total_tokens`** 及上述明细，返回可序列化 dict（供 SSE **`usage`** 事件）
- **`metrics_text`**：返回 `(bytes, content_type)` 供 **`/metrics`**
