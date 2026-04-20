# `app/observability/`

Prometheus 指标与相关辅助（由 FastAPI **`GET /metrics`** 暴露）。

| 文件 | 说明 |
|------|------|
| **`metrics.py`** | LLM token Counter、`metrics_text()` 供路由返回 |
| **`__init__.py`** | 对外 re-export |
