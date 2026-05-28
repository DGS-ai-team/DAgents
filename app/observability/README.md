# `app/observability/`

Prometheus 指标与相关辅助（由 Agent API 与 Register Center 的 FastAPI **`GET /metrics`** 暴露）。机制与新增指标步骤见 **`../../docs/prometheus-metrics.md`**。

| 文件 | 说明 |
|------|------|
| **`metrics.py`** | LLM token Counter、session/queue/tool/summary/A2A 指标、**`metrics_text()`** 供路由返回 |
| **`__init__.py`** | 对外 re-export |
