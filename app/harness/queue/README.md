# `app/harness/queue/`

进程内优先级队列（MVP）：**仅入队 / 阻塞出队**（`enqueue` / `await receive`）及 pause/resume/stop；**消费者由上层**（如 `AgentService`）自行循环 `receive` 并处理。

| 文件 | 说明 |
|------|------|
| **`message_queue.py`** | **`MessageEnvelope`**（Pydantic，支持 `async_tool_result` 与 `tool_result` 事件）、**`MessageQueue`**：`enqueue`、`receive`、`pause` / `resume`、`stop` |

