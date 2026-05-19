# `app/harness/api/` REFERENCE

## `app.py`

- **`create_app`**：创建 FastAPI 应用并挂载生命周期与路由；**`METRICS_ENABLED`** 为真时注册 **`GET /metrics`**（`app.observability.metrics.metrics_text`）
- **`CancelTurnResult`**：**`POST /v1/sessions/{session_id}/cancel`** 响应（**`cancelled`** 表示是否确有在途 **`_handle_message`** 被取消）
- **`MessageIn`**、**`SubmitResult`**：请求/响应模型（`MessageIn` 支持 `request_type=message|resume`；**`priority`** 缺省按请求类型填充 **`human`/`resume`**）
- **`_normalize_inbound_peer_message`**：识别入站 `AgentPeerEnvelope` 文本，提取 payload content 并将 source 标记为 `a2a:<caller_agent_id>`。
- **`_to_sse`**：将 `StreamEvent` 编码为 SSE 文本块

