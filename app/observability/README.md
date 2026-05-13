# `app/observability/`

Prometheus 指标与相关辅助（由 FastAPI **`GET /metrics`** 暴露）。机制与新增指标步骤见 **`../../doc/prometheus-metrics.md`**。

| 文件 | 说明 |
|------|------|
| **`metrics.py`** | LLM token Counter、**`dagents_session_context_messages_count`**（**`OpenAIConversationContext.messages`** 条数，由 **`AgentService`** 在上下文变更时刷新）、**`metrics_text()`** 供路由返回 |
| **`__init__.py`** | 对外 re-export |
