# `app/harness/api/`

FastAPI 接入层：统一对外入口（UI/CLI 等客户端通过 HTTP 调用）。

| 文件 | 说明 |
|------|------|
| **`app.py`** | FastAPI 应用与路由：健康检查、**`GET /metrics`**、**`POST /v1/sessions/{session_id}/cancel`**（取消当前 turn）、提交消息、SSE；**`MessageIn.priority`** 缺省 **`message`→`human`**、**`resume`→`resume`**（**`human` 仅队列优先级，不自动 cancel**） |

