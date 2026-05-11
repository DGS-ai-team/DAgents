# `app/harness/service/` REFERENCE

## `agent_service.py`

- **`AgentService`**：独立 Agent 服务（`start` / `stop` / `run_forever`、`create_session`、`submit_message` / `submit_resume`、`cancel_current_turn`）；**`submit_message`** 仅 **`enqueue`**（**`human`** 只影响 **`MessageQueue`** 优先级，不自动 **`cancel_current_turn`**）；按 session 维护 **`MessageQueue`** + **`_session_consume_loop`**（`receive` → **`_handle_message`**）、**`_session_active_handles`**；活跃 session 队列上限来自 **`Settings.agent_max_active_session_queues`**（环境变量 **`AGENT_MAX_ACTIVE_SESSION_QUEUES`**）；启动时向 `AsyncToolResultStore` 注册 message_queue sender，把任务终态以 `request_type="async_tool_result"` 入队；`_handle_message` 在业务分支前会调用 **`MainAgentTurnOrchestrator.maybe_handle_summary_compression`** 处理 summary 压缩入口流程；具体业务分支与工具审批/执行同样委托 **`app.core.main_agent.agent.MainAgentTurnOrchestrator`**；runtime 仅承担 `human_message/tool_message` 单轮推理；**`CancelledError`** 时调用 runtime **`flush_cancelled_turn`**；**`_session_contexts`**、可选 **`SqliteMessageStore`**、`_resolve_context` / `_persist_context`；**`_stream_base_meta`**（`session_id` / `model`）；**`_map_event_envelope_to_stream`** 合并 **`base_meta`** 到每条 SSE **`data.meta`**，并映射 **`usage`**（`prompt_tokens` / `completion_tokens` / `total_tokens`）；**`assistant` / `reasoning` / `tool_call` / `tool_result`** 的扁平 **`data`** 含 **`display_type`**（缺省分别为 **`normal_text` / `reasoning` / `normal_text` / `normal_text`**）；在 **`_handle_message` finally**、**`create_session`**、**`_resolve_context` 首次装入**、**`release_session`** / **`_evict_session_for_capacity`**、**`stop`** 时调用 **`refresh_session_context_metrics`**，将 **`OpenAIConversationContext.messages`** 条数暴露到 Prometheus（**`dagents_session_context_messages_count`**）
- **`_queue_maxsize`**：将配置的队列上限规范化为 `MessageQueue` 参数

## `interface.py`

- **`AgentSubmitRequest`** 等：**Pydantic `BaseModel`（frozen）**；`message` 类型时校验 **`content`** 非空；**`resume`** 且 **`priority` 仍为默认 `other`** 时自动改为 **`resume`**
- **`AgentCancelTurnResult`**：**`cancel_current_turn`** 的返回
- **`AgentSessionCreateResult`**、**`AgentSubmitResult`**、**`AgentStreamEventData`**、**`AgentEventEnvelope`**：同上
- **`AgentServiceClient`**：客户端协议（`submit`、`cancel_current_turn`、`stream`）

## `http_client.py`

- **`HttpAgentServiceClient`**：HTTP 客户端实现，提交消息、**`cancel_current_turn`**、订阅 SSE 事件流。

