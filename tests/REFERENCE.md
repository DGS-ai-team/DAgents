# `tests/` REFERENCE

## `test_agent_service.py`

- **`FakeRuntime`** / **`HangRuntime`**：测试替身，模拟 `run_turn`；`HangRuntime` 用于取消路径
- **`AgentServiceTestCase`**：验证服务启动、消息消费、`cancel_current_turn`、事件映射

## `test_message_queue.py`

- **`MessageQueuePriorityTestCase`**：验证优先级顺序（`human` > `resume` > `other`）与自定义 envelope；上层自管 `receive` 循环

## `test_schema_approval.py`

- **`SchemaApprovalTestCase`**：验证 **`parse_resume_tool_decision`**、**`is_tool_execution_approved`**、**`ApprovalRequiredEnvelopePayload`**

## `test_metrics_tokens.py`

- **`MetricsTokensTestCase`**：验证 **`parse_usage_tokens`**、**`sanitize_model_label`**、**`record_llm_token_usage`** 与 `generate_latest` 文本

## `test_api_sse.py`

- **`ApiSseTestCase`**：验证 `/v1/messages` 与 `/v1/streams/{request_id}` 的 SSE 回传链路

## `test_api_cancel_turn.py`

- **`ApiCancelTurnTestCase`**：验证 **`POST /v1/sessions/{session_id}/cancel`** 在无在途 turn 时的 JSON 形态

## `call_agent_api.py`

- **`main`**：手动联调入口（提交消息并读取 SSE 直到 `done`）

## `call_runtime_request_model_stream.py`

- **`main`**：直接调用 `_request_model_stream`，打印 OpenAI 原始 chunk 与 runtime 事件
- **`_chunk_to_json`**：将 SDK chunk 转为单行 JSON 字符串，便于日志分析

