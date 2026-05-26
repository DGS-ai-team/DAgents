# `tests/` REFERENCE

> 历史逐文件用例索引已移除；请以 **`UNIT_TEST_CHECKLIST.md`** 为权威规划。补回用例后在此按 **`test_<领域>.py`** 分节维护。

## `test_smoke.py`

- **`WorkspaceImportSmokeTest`**：轻量 `import app.harness.queue.message_queue`，验证工作区可解析。

## `test_config_settings.py`

- **`ResolveRuntimeRootTests`**：`resolve_runtime_root` 源码根与 `sys.frozen` 分支。
- **`LoadEnvTests`**：`load_env` 在有/无 `.env` 时对 `dotenv.load_dotenv` 的调用约定（`override=False`）。
- **`SettingsLoadTests`**：`Settings.load` / `get_settings` 默认值、环境覆盖与 `reload=True`。

## `test_message_queue.py`

- **`MessageQueueAsyncTests`**：四级优先级、同优先 FIFO、`pause`+`stop` 与 `receive` 的 `RuntimeError`、自定义 envelope、`pending_metrics_rows`。

## `test_async_tool_store.py`

- **`AsyncToolSubmitClientIdTests`**：**`AsyncToolResultStore.submit_coroutine`** 对 **`client_id`** 非空约束及任务跑通终态。

## `test_api_app.py`

- **`FastApiRouteTests`**：使用 `AgentService` 替身验证 `/v1/sessions`、`/v1/messages`、`resume`、`cancel`、`release`、session context/skills 路由接线与默认 priority。
- **`SseEncodingTests`**：验证 `_to_sse` 输出 `event:` / `data:` 与空行结尾格式。

## `test_agent_service.py`

- **`AgentServiceStreamMapTests`**：`_map_event_envelope_to_stream` 扁平字段（需完整依赖，否则 skip）。
- **`AgentServiceLifecycleTests`**：`start`/`stop`、`submit_message`→`handle_message`、`cancel_current_turn`、`handle_stream_event` 错误路径；**`_make_service`** 内 patch **`runtime_openai.get_openai_client`**，避免懒加载 **runtime** 时无 **`LLM_API_KEY`** 触发 **OpenAI** 构造异常（需完整依赖，否则 skip）。

## `test_main_agent_orchestrator.py`

- **`MainAgentTurnOrchestratorTests`**：使用脚本化 runtime 替身覆盖 human 无工具收口、自动工具执行、审批挂起、pending 被新 human 打断、resume approve 工具闭环。

## `test_context_models.py`

- **`OpenAIConversationContextRoundTripTests`**：`to_conversation_context` / `from_conversation_context` 与 `normalized_run_turn_phase_for_persist`。
- **`ConversationContextUnpackTests`**：`unpack_for_openai_runtime` 过滤无 `call_id` 的 pending。

## `test_raw_message_journal.py`

- **`RecordRawOpenaiMessageAppendTests`**：开关关闭与空 `session_id` 不写盘。
- **`AppendOpenaiMessageWithJournalTests`** / **`InsertOpenaiMessageWithJournalTests`**：列表变更、JSONL 行结构，以及 `assistant + tool_calls` 写入时统一补齐/继承 `reasoning_content`（`get_settings` / `resolve_runtime_root` patch）。

## `test_schema_approval.py`

- **`ParseResumeToolDecisionTests`** / **`IsToolExecutionApprovedTests`**：`parse_resume_tool_decision`、`is_tool_execution_approved`。

## `test_streaming_events.py`

- **`InMemoryEventBusTests`**：`publish` 与 `subscribe_all` 投递、`seq` 递增。

## `test_support/`

- 详见 **`test_support/REFERENCE.md`**（Settings 替身等）。

## `UNIT_TEST_CHECKLIST.md`

- 全仓库单测路线图；**§5 API** 等待在「完整 `requirements` + 预 patch 编排层」下补 `TestClient` 用例。

## `test_cli_approval.py`

- **`CliApprovalTests`** / **`CliSseParserTests`**：审批载荷解析、resume 决策、SSE block 解析。

## `test_cli_session_controller.py`

- **`SessionControllerRenderTests`**：后台 render、turn 栅栏、approval skip done。
- **`SessionControllerBindTriggersTests`**：`bind_triggers_to_client` PATCH 逻辑。

## `test_context_clear.py`

- **`OpenAIConversationContextResetTests`**：`reset_conversation_state` 保留/清空字段。

## `test_agent_service_sessions.py`

- **`AgentServiceSessionAdminTests`**：`list_sessions`、`delete_persisted_session`、`get_session_context_summary`、`clear_session_context`、session skill 加载/卸载行为。

## `integration/`

- 详见 **`integration/REFERENCE.md`**（`live_llm_smoke`：可选真实 LLM 冒烟）。
