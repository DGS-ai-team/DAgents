# `app/` 模块索引

## `deprecated_backend.py`

- **`DEPRECATED_BACKEND`** / **`DEPRECATED_BACKEND_REPLACEMENT`**：弃用标记与替代栈名
- **`DEPRECATED_BACKEND_NOTICE_ZH`** / **`_EN`**：中英文说明
- **`log_deprecation_warning(logger)`**：启动时 WARNING
- **`health_deprecation_fields()`**：`/health` JSON 扩展字段

## `config/settings.py`

- **`Settings`**、**`get_settings`**
- 可观测性：**`metrics_enabled`**（**`METRICS_ENABLED`**）、LLM 流式 usage：**`llm_stream_include_usage`**（**`LLM_STREAM_INCLUDE_USAGE`**）
- 队列相关字段：**`max_queue_size`**
- API 监听：**`api_host`** / **`api_port`**（**`API_HOST`** / **`API_PORT`**）；CLI 客户端 URL 为推导字段 **`agent_api_base`**
- 会话 sqlite：路径固定为 **`<运行根>/.runtime/memory/session.sqlite3`**（**`runtime_layout.session_sqlite_path()`**）；是否使用 **`SqliteMessageStore`** 由 **`agent_session_store_enabled`**（**`AGENT_SESSION_STORE_ENABLED`**，默认开启）控制

## `config/env.py`

- **`load_env`**

## `context/models.py`

- **`MessageRecord`**、**`ConversationContext`**、**`PendingToolCall`**、**`RunTurnPhase`**、**`SummaryCompressionPhase`**、**`OpenAIConversationContext`**（均为 **Pydantic**）

## `observability/`

- 详见 **`app/observability/README.md`**；**`metrics.py`**：LLM token Counter、**`/metrics`** 文本生成

## `schemas/`（审批与 resume 契约）

- 详见 **`app/schemas/README.md`**；**`approval.py`**：`ApprovalRequiredEnvelopePayload`、`ResumeToolApprove`/`Reject`、`parse_resume_tool_decision` 等

## `core/main_agent/model.py`

- **`get_openai_client`**、**`get_model_config`**

## `core/main_agent/prompt.py`

- **`get_static_system_prompt`**、**`get_system_prompt`**、**`read_memory_file_cached`**

## `core/main_agent/runtime_openai.py`

- **`OpenAIImplicitReActRuntime`**：**`run_turn(ctx: OpenAIConversationContext, ...)`**、**`ctx.run_turn_phase`**（无 session/存储）

## `core/main_agent/agent.py`

- **`init_agent`**

## `harness/tools/tool.py`

- **`get_tools`**

## `harness/tools/bash.py`

- **`bash_run`**

## `harness/tools/fs.py`

- **`read_file`**（流式 **`line_offset`/`line_limit`**，**`next_line_offset`**）、**`write_file`**、**`search_replace`**、**`search_file`**（**`next_index_offset`**，建议 **`read_file`**）

## `harness/queue/message_queue.py`

- **`MessageEnvelope`**（**Pydantic frozen**）、**`MessageQueue[EnvelopeT]`**（`enqueue` / `await receive` / `pause` / `resume` / `stop`；消费者由上层实现）

## `harness/service/agent_service.py`

- **`AgentService`**：每 session **`MessageQueue` + `_session_consume_loop`**、**`submit_message`（仅入队）**、**`cancel_current_turn`（客户端显式打断）**、**`OpenAIConversationContext`** 缓存、**`SqliteMessageStore`**、**`run_turn`** 与 **`flush_cancelled_turn`（取消时）**

## `harness/service/interface.py`

- **`AgentSessionCreateResult`**、**`AgentSubmitRequest`**、**`AgentSubmitResult`**、**`AgentEventEnvelope`**、**`AgentStreamEventData`**

## `harness/api/app.py`

- **`create_app`**、**`SessionCreateIn`**、**`SessionCreateResult`**、**`MessageIn`**、**`SubmitResult`**

## `harness/streaming/events.py`

- **`StreamEvent`**、**`EventBus`**、**`InMemoryEventBus`**

## `harness/memory/store.py`

- **`SqliteMessageStore`**（每会话 `content` BLOB：`history` + runtime/summary 常驻字段；**`save_conversation_content`**、**`load_conversation_content`**；单条消息见 **`content/models.py`**）

## `harness/cli/main.py`

- **`main`**、**`_run_http_cli`**、**`_submit_and_stream`**
