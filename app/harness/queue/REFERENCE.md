# `app/harness/queue/` REFERENCE

## `message_queue.py`

- **`MessageEnvelope`**：**Pydantic `BaseModel`（frozen）**；`request_type=message|resume|async_tool_result|tool_result`，含 `content`/`resume_value`/`async_tool_result`/`tool_result`（工具审批见 **`app.schemas.approval`**）
- **`MessageQueue[EnvelopeT]`**：MVP 优先级消息队列；`enqueue(envelope=..., priority=tool_result|human|resume|other)`；`await receive()` 阻塞出队；`pause_consuming` / `resume_consuming` / `stop`；优先级：`tool_result=-1`、`human=0`、`resume=1`、`other=10`；**`pending_metrics_rows`**：观测用读取堆快照（CPython **`PriorityQueue._queue`**），按出队顺序列出尚未 **`receive`** 的条目（Prometheus 会话上下文指标见 **`metrics.refresh_session_context_metrics`**）

