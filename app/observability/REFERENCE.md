# `app/observability/` REFERENCE

## `metrics.py`

- **`LLM_PROMPT_TOKENS`**、**`LLM_COMPLETION_TOKENS`**：Prometheus `Counter`，label **`model`**
- **`sanitize_model_label`**：模型名 → 安全 label
- **`parse_usage_tokens`**：从 SDK `usage` 或 dict 解析 `(prompt_tokens, completion_tokens)`
- **`usage_fields_from_openai_usage`**：解析 **`total_tokens`** 并返回可序列化 dict（供 SSE **`usage`** 事件）
- **`record_llm_token_usage`**：累加一次调用的 token
- **`metrics_text`**：返回 `(bytes, content_type)` 供 **`/metrics`**
