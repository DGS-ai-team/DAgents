# 单元测试清单（重写规划）

> **约定**：默认 `python -m unittest discover -s tests -p "test_*.py" -v`；**不**自动发现 `tests/integration/`（见该子目录 README）。  
> **状态图例**：`✅` 已有对应用例且默认 discover 会跑；`🟡` 已建文件但部分场景未覆盖或依赖完整 `requirements.txt`；`⬜` 尚未建建议文件。

---

## 进度快照（与仓库 `tests/test_*.py` 同步）

| 文件 | 覆盖清单章节 | 备注 |
|------|----------------|------|
| `test_smoke.py` | §0 P0 | 工作区可导入 |
| `test_support/stub_settings.py` | §0 P1（部分） | Settings `SimpleNamespace`；`FakeRuntime` / sqlite 夹具仍缺 |
| `test_config_settings.py` | §1 | `get_settings`、`load_env`、`resolve_runtime_root` |
| `test_context_models.py` | §2 | 往返与 `run_turn_phase` 规范化、`unpack` 过滤 |
| `test_message_queue.py` | §3 | 含 P2「堆序与出队一致」断言（未拆 `test_message_queue_metrics.py`） |
| `test_agent_service.py` | §4 | 导入 `AgentService` 失败时**整类 skip**（如无 `openai`）；含 `_map_event_envelope_to_stream` |
| `test_raw_message_journal.py` | §6 | `record_*` / `append_*` / `insert_*` |
| `test_streaming_events.py` | §7 | `InMemoryEventBus` |
| `test_schema_approval.py` | §8（部分） | approval resume 解析；**未**含 `agent_peer` 信封 |
| `integration/live_llm_smoke.py` | §14 | 显式模块运行，非 discover |

**未建文件（仍按下方章节规划）**：`test_api_*.py`、`test_main_agent_orchestrator.py`、`test_prompt_runtime_env.py`、`test_bash_su_guard.py`、`test_tooling.py`、`test_metrics_tokens.py`、`test_host_snapshot.py` 等。

---

## 0. 横切与基础设施

| 优先级 | 模块 / 主题 | 建议用例文件 | 要点 | 状态 |
|--------|-------------|--------------|------|------|
| P0 | 工作区可导入 | `test_smoke.py` | 轻量 import，保证 CI 非空 discovery | ✅ |
| P1 | 测试夹具 / 替身 | `test_support/stub_settings.py`；可选 `fakes.py` | `FakeRuntime`、`MessageEnvelope` 工厂、临时目录 sqlite | 🟡 |

---

## 1. `app/config/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P1 | `get_settings` / `load_env` | `test_config_settings.py` | 关键字段默认值、环境变量覆盖（`patch.dict(os.environ)`） | ✅ |
| P2 | `resolve_runtime_root` / 路径拼接 | 同上 | 相对路径展开、边界空串、`sys.frozen` 分支 | ✅ |

---

## 2. `app/context/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P1 | `OpenAIConversationContext` / `PendingToolCall` | `test_context_models.py` | `from_conversation_context` / `to_conversation_context` 往返、`run_turn_phase` 规范化 | ✅ |
| P2 | `AgentSubmitRequest`（若在 interface） | 与 §8 合并或单独 | resume 默认 priority 等 | ⬜ |

---

## 3. `app/harness/queue/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P0 | `MessageQueue` 优先级 | `test_message_queue.py` | `tool_result` / `human` / `resume` / `other` 出队顺序 | ✅ |
| P1 | 同优先级 FIFO | 同上 | 同优先二级 `_seq` 稳定 | ✅ |
| P1 | `enqueue` 泛型 envelope | 同上 | 自定义类型 + `getattr` 日志不崩 | ✅ |
| P2 | `pending_metrics_rows` | 可拆 `test_message_queue_metrics.py`；当前合并在 `test_message_queue.py` | 堆顺序与出队一致 | ✅ |
| P1 | `stop` / `receive` 唤醒 | 同上 | 先 `pause` 再 `stop` 后 `receive` → `RuntimeError` | ✅ |

---

## 4. `app/harness/service/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P0 | `AgentService` 启动/停止 | `test_agent_service.py` | `start`/`stop`、`AsyncToolResultStore` 注册/注销 | 🟡 |
| P0 | 消费一条 `human` 消息 | 同上 | mock `MainAgentTurnOrchestrator.handle_message`，断言入队消费 | 🟡 |
| P1 | `cancel_current_turn` | 同上 | 在途子 task 取消 | 🟡 |
| P1 | `_map_event_envelope_to_stream` | 同上（未拆 `test_agent_service_stream_map.py`） | `assistant` / `tool_result` / `approval_required` / `done` | 🟡 |
| P1 | `handle_stream_event` 错误收口 | 同上 | 编排异常 → `error` + `done` 转发回调 | 🟡 |
| P2 | `release_session` / 队列上限淘汰 | `test_agent_service_capacity.py` | 闲置淘汰策略 | ⬜ |

> **说明**：§4 用例依赖 `AgentService` 完整导入链（含 `openai` 等）。未安装 `requirements.txt` 时 **skip**，CI 安装依赖后应 **全部执行**。

---

## 5. `app/harness/api/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P1 | `POST /v1/messages` | `test_api_messages.py` | 202/422、与 `AgentService.submit_message` 接线 | ⬜ |
| P1 | `GET /v1/streams` SSE | `test_api_sse.py` | `StreamingResponse` 片段、`handle_stream_event` → bus | ⬜ |
| P2 | `POST .../cancel` | `test_api_cancel_turn.py` | `cancelled` 布尔、无在途时幂等语义 | ⬜ |
| P2 | `POST /v1/sessions` | 可合并 | session_id 生成/传入 | ⬜ |

> **说明**：API 单测建议在**预 patch 编排层 / Settings** 后再 `import` 应用模块，或提供 `create_test_app` 类工厂，避免 `create_app()` 拉起真实 LLM。

---

## 6. `app/harness/history/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P1 | `record_raw_openai_message_append` | `test_raw_message_journal.py` | 开关关闭不写、`session_id` 空跳过 | ✅ |
| P1 | `append_openai_message_with_journal` | 同上 | messages 追加 + 文件行 JSON 字段 | ✅ |
| P2 | `insert_openai_message_with_journal` | 同上 | insert 顺序与 JSONL 追加一行 | ✅ |

---

## 7. `app/harness/streaming/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P2 | `InMemoryEventBus` | `test_streaming_events.py` | `publish` / `subscribe_all` 异步迭代、`seq` | ✅ |

---

## 8. `app/schemas/` 与 `app/harness/service/interface.py`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P1 | `approval` 解析 | `test_schema_approval.py` | `parse_resume_tool_decision`、`is_tool_execution_approved` | ✅ |
| P2 | `agent_peer` 信封 | `test_schema_agent_peer.py` | `build_agent_peer_envelope` / `parse_*` | ⬜ |

---

## 9. `app/core/main_agent/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P1 | `get_system_prompt` | `test_prompt_runtime_env.py` | mock `HostSnapshot`、运行环境段、JSONL 记录段开关 | ⬜ |
| P0 | `MainAgentTurnOrchestrator` 分支 | `test_main_agent_orchestrator.py` | mock runtime：`human_message` / `tool_result` / `resume` / `async_tool_result` | ⬜ |
| P1 | `_handle_human_message` + pending 打断 | 同上 | `pending_tool_calls` 清空、`run_turn_phase` | ⬜ |
| P2 | `_invoke_tool` + SSE | 同上 | 工具返回 dict/list/str、错误路径 | ⬜ |

---

## 10. `app/harness/tools/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P1 | `bash_run` su/sudo 守卫 | `test_bash_su_guard.py` | mock `get_host_snapshot`：非 root 拦截 / `sudo -n` 放行 | ⬜ |
| P2 | `@tool` 装饰器 | `test_tooling.py` | async_store 通知路径（可 mock store） | ⬜ |
| P2 | `agent_peer` HTTP 客户端 | `test_agent_peer_tools.py` | `httpx` mock：discover、send、审批 | ⬜ |

---

## 11. `app/observability/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P2 | `parse_usage_tokens` / `sanitize_model_label` | `test_metrics_tokens.py` | 边界、负值钳制 | ⬜ |
| P2 | `record_llm_token_usage` | 同上 | Counter `inc`、独立 registry 防串测 | ⬜ |
| P2 | `refresh_session_context_metrics` | `test_session_context_metrics.py` | session 移除后 series 清理 | ⬜ |

---

## 12. `app/config/host_snapshot.py`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P2 | `capture` / `get` 单例 | `test_host_snapshot.py` | 引用相等、惰性构建 | ⬜ |

---

## 13. `register_center/`（若与主包同仓联测）

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P3 | 模型校验 | `test_register_center_models.py` | Pydantic 字段、normalize | ⬜ |

---

## 14. `tests/integration/`（非默认 discovery）

| 优先级 | 主题 | 现有/建议 | 说明 | 状态 |
|--------|------|-----------|------|------|
| Opt | 真机 LLM 冒烟 | `integration/live_llm_smoke.py` | `RUN_LIVE_LLM_TESTS=1` + `LLM_API_KEY`；显式 `unittest` 模块名运行 | ✅ |

---

## 15. 手动脚本（可选，不纳入 `test_*.py` discovery）

| 文件 | 用途 | 状态 |
|------|------|------|
| `scripts/` 或 `tests/manual/` 下移 | `call_agent_api.py`、`call_runtime_request_model_stream.py` 类联调（若仍需要） | ⬜ |

---

## 建议实施顺序（迭代）

1. **P0（当前）**：`test_smoke` ✅ → `test_message_queue` ✅ → `test_agent_service` 🟡（CI 全依赖下跑满）。  
2. **P1（下一批）**：§5 API（`TestClient` + 编排 mock）、`test_main_agent_orchestrator`、`test_prompt_runtime_env`、`test_bash_su_guard`。  
3. **P2**：metrics、host_snapshot、§8 `agent_peer` 信封、`test_tooling` / `test_agent_peer_tools`。  
4. **P3**：register_center 模型、sqlite 大集成（可拆慢测 job）。

---

## 与 CI 对齐

`.github/workflows/pr-tests.yml` 使用：

```bash
python -m unittest discover -s tests -p "test_*.py" -v
```

新增用例文件请命名为 `test_<领域>.py`，类继承 `unittest.TestCase` 或 `IsolatedAsyncioTestCase`。
