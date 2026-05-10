# `tests/` REFERENCE

## `test_agent_service.py`

- **`FakeRuntime`** / **`HangRuntime`**：测试替身，模拟 `run_turn`；`HangRuntime` 用于取消路径
- **`AgentServiceTestCase`**：验证服务启动、消息消费、`cancel_current_turn`、事件映射

## `test_message_queue.py`

- **`MessageQueuePriorityTestCase`**：验证优先级顺序（`human` > `resume` > `other`）与自定义 envelope；上层自管 `receive` 循环
- **`MessageQueuePendingMetricsTestCase`**：**`pending_metrics_rows`** 堆快照与出队顺序一致

## `test_schema_approval.py`

- **`SchemaApprovalTestCase`**：验证 **`parse_resume_tool_decision`**、**`is_tool_execution_approved`**、**`ApprovalRequiredEnvelopePayload`**

## `test_metrics_tokens.py`

- **`MetricsTokensTestCase`**：验证 **`parse_usage_tokens`**、**`sanitize_model_label`**、**`record_llm_token_usage`**（Gauge **`set`**）与 `generate_latest` 文本

## `test_api_sse.py`

- **`_SseFakeRuntime`**：注入 `AgentService._runtime`，模拟 `assistant` + `done` 事件
- **`ApiSseTestCase`**：验证 `/v1/messages` 与 `GET /v1/streams?client_id=...` 的 SSE 回传链路

## `test_api_cancel_turn.py`

- **`ApiCancelTurnTestCase`**：验证 **`POST /v1/sessions/{session_id}/cancel`** 在无在途 turn 时的 JSON 形态

## `test_bash_su_guard.py`

- **`BashPrivilegeGuardTestCase`**：非 root + bash 拦截 **`su - … -c`**、无 **`-n`/`--non-interactive`** 的 **`sudo`/`sudoedit`**；**`sudo -n`** 与 root / powershell；通过 mock **`get_host_snapshot`** 注入 OS/euid

## `test_host_snapshot.py`

- **`HostSnapshotTestCase`**：**`capture`** 与 **`get`** 引用同一快照；惰性 **`get`** 单例

## `test_prompt_runtime_env.py`

- **`PromptRuntimeEnvTestCase`**：mock **`get_host_snapshot`**，断言 **`get_system_prompt`** 含 OS 类别、登录名、UID/GID 或非 POSIX 提示

## `test_session_context_metrics.py`

- **`SessionContextMetricsTestCase`**：验证 **`dagents_session_context_messages_count`** 与 session 移除后的 series 清理

## `call_agent_api.py`

- **`main`**：手动联调入口（提交消息并读取 SSE 直到 `done`）

## `call_runtime_request_model_stream.py`

- **`main`**：直接调用 `_request_model_stream`，打印 OpenAI 原始 chunk 与 runtime 事件
- **`_chunk_to_json`**：将 SDK chunk 转为单行 JSON 字符串，便于日志分析

## `integration/`

详见 `integration/REFERENCE.md`（`test_llm_live`：可选真实 LLM 冒烟）。

