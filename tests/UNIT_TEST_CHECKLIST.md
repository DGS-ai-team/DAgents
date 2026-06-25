# 单元测试清单

> **Python 约定**：`python -m unittest discover -s tests -p "test_*.py" -v`；**不**自动发现 `tests/integration/`（见该子目录 README）。  
> **Go 约定**：`go test ./shared/config/... ./node/... ./client/...`（各包内 `*_test.go`）。  
> **状态图例**：`✅` 已有对应用例且默认 discover / `go test` 会跑；`🟡` 已建文件但部分场景未覆盖或依赖完整 `requirements.txt` / 环境；`⬜` 尚未建建议文件；`—` Go 侧无对应 Python 模块或反之。

**进度快照（2025-05）**：Python **182** 例 OK；Go **~149** 个 `Test*`（2 SKIP），全绿。

---

## 进度快照 — Python（`tests/test_*.py`）

| 文件 | 覆盖章节 | 备注 |
|------|----------|------|
| `test_smoke.py` | §0 | 工作区可导入 |
| `test_support/stub_settings.py` | §0 | Settings `SimpleNamespace`；`FakeRuntime` 在 orchestrator 单测内联 |
| `test_config_settings.py` | §1 | `get_settings`、`load_env`、`resolve_runtime_root`、`metrics_enabled` |
| `test_context_models.py` | §2 | 往返与 `run_turn_phase` 规范化、`unpack` 过滤 |
| `test_context_clear.py` | §2 | `OpenAIConversationContext` reset / clear |
| `test_message_queue.py` | §3 | 优先级、FIFO、`stop`/`receive`、**P2 堆序与出队一致** |
| `test_agent_service.py` | §4 | 生命周期、`_map_event_envelope_to_stream`；无 `openai` 时 skip |
| `test_agent_service_sessions.py` | §4 | session 管理、pending tool 等 |
| `test_api_app.py` | §5 | FastAPI 路由、SSE 编码、registry 登记（Fake `AgentService`） |
| `test_raw_message_journal.py` | §6 | `record_*` / `append_*` / `insert_*` |
| `test_streaming_events.py` | §7 | `InMemoryEventBus` |
| `test_schema_approval.py` | §8 | approval resume 解析 |
| `test_schema_agent_peer.py` | §8 | `agent_peer` 信封 build/parse |
| `test_main_agent_orchestrator.py` | §9 | mock runtime：`human` / `tool_result` / `resume` / async 等 |
| `test_main_agent_prompt.py` | §9、§12（部分） | `get_system_prompt`、mock `HostSnapshot`、缓存 |
| `test_runtime_openai_thinking.py` | §9 | reasoning 持久化、`MessageRecord` |
| `test_fs_tools.py` | §10 | 文件系统工具 |
| `test_agent_peer_tools.py` | §10、§11（部分） | A2A discover/send/relay；含 metrics 文本断言 |
| `test_user_information_tool.py` | §10 | `ask_user_information` |
| `test_tool_schema_validation.py` | §10 | `bash_run` schema 校验 |
| `test_tool_result_policy.py` | §10 | 工具结果策略 |
| `test_async_tool_store.py` | §10 | 异步工具 store / routing |
| `test_memory_store.py` | §6（扩展） | SQLite 会话内容 |
| `test_triggers.py` | harness triggers | 条件校验、store、scheduler、工具 |
| `test_register_center_security.py` | §13 | 持久化、shared token、relay resume |
| `test_cli_daemon.py` | §16 | `dagents serve` 守护进程（**11 例**，CI discover） |
| `test_cli_session_controller.py` | §16 | Textual session 控制器渲染 |
| `test_cli_child_agent.py` | §16 | 子 Agent scope / tracker / SSE 过滤 |
| `test_cli_approval.py` | §16 | HITL 审批、SSE 解析 |
| `test_cli_tool_calls.py` | §16 | tool call 规范化 |
| `test_cli_user_information.py` | §16 | CLI 用户信息收集 |
| `integration/live_llm_smoke.py` | §14 | 显式模块运行，非 discover |

**仍缺 Python 文件（按下方章节）**：`test_bash_su_guard.py`（专用 su/sudo 守卫）、`test_metrics_tokens.py`、`test_host_snapshot.py`（`capture` 单例）、`test_agent_service_capacity.py`、`test_tooling.py`（`@tool` 装饰器专项，部分逻辑已在 `test_async_tool_store`）。

---

## 进度快照 — Go（`node/` · `client/` · `shared/config/`）

| 包 | 测试文件 | 要点 | 状态 |
|----|----------|------|------|
| `shared/config` | `config_test.go`, `resolve_test.go` | 配置加载、路径解析 | ✅ |
| `node/internal/api` | `server_test.go`, `triggers_api_test.go`, `child_agents_api_test.go` | HTTP 路由、triggers、子 Agent API | ✅ |
| `node/internal/childagent` | `manager_test.go`, `wait_delivered_test.go` | 创建/异步/wait；**终态 `delivered` 快照** | ✅ |
| `node/internal/compression` | `coordinator_test.go` | 摘要压缩协调 | ✅ |
| `node/internal/history` | `journal_test.go` | 历史 journal | ✅ |
| `node/internal/hitl` | `resume_test.go` | resume 解析 | ✅ |
| `node/internal/llm` | `mock_test.go` | mock LLM 客户端 | ✅ |
| `node/internal/logx` | `logx_test.go` | 日志 | ✅ |
| `node/internal/policy` | `engine_test.go` | 策略引擎 | ✅ |
| `node/internal/promptcontext` | `reader_test.go` | prompt 上下文读取 | ✅ |
| `node/internal/queue` | `queue_test.go` | 消息队列 | ✅ |
| `node/internal/session` | `manager_test.go`, `manager_child_test.go`, `triggers_test.go` | session、子 Agent 绑定、triggers | ✅ |
| `node/internal/skills` | `skills_test.go` | skills 加载 | ✅ |
| `node/internal/store` | `sqlite_test.go` | SQLite store | ✅ |
| `node/internal/stream` | `hub_test.go` | SSE hub | ✅ |
| `node/internal/tools` | `tools_test.go`, `bash_policy_test.go`, `background_jobs_test.go`, `fs_read_search_test.go` | 工具执行、**bash 策略**、后台任务、FS | ✅ |
| `node/internal/triggers` | `triggers_test.go`, `schedule_test.go` | trigger 与调度 | ✅ |
| `node/internal/turn` | `orchestrator_test.go`, `tool_result_messages_test.go`, `prompt_test.go` | turn 编排、tool 消息、prompt | ✅ |
| `client/internal/api` | `client_test.go` | Node HTTP 客户端 | ✅ |
| `client/internal/hitl` | `approval_test.go`, `compression_test.go`, `scope_test.go`, `user_information_test.go` | 审批、压缩、scope、用户信息 | ✅ |
| `client/internal/probe` | `probe_test.go` | 健康探测 | ✅ |
| `client/internal/tui/full` | `stream_events_test.go`, `child_agent_test.go` | 全屏 TUI SSE、子 Agent | ✅ |
| `client/internal/tui/repl` | `stream_test.go` | REPL **子 Agent SSE 过滤** | ✅ |
| `client/internal/tui/shared` | `transcript_test.go`, `child_agents_format_test.go` |  transcript、/children 格式化 | ✅ |
| `node/internal/hostsnapshot` | — | 宿主机快照 | ⬜ |
| `node/cmd/dagents-node` | — | main 入口 | ⬜ |
| `client/cmd/dagents-client` | — | main 入口 | ⬜ |
| `client/internal/tui`（顶层 dispatch） | — | full/repl 模式切换 | ⬜ |
| `node/internal/version` | — | 版本常量（全项目唯一） | ⬜ |

---

## 0. 横切与基础设施

| 优先级 | 模块 / 主题 | 建议用例文件 | 要点 | 状态 |
|--------|-------------|--------------|------|------|
| P0 | 工作区可导入 | `test_smoke.py` | 轻量 import，保证 CI 非空 discovery | ✅ |
| P1 | 测试夹具 / 替身 | `test_support/stub_settings.py`；orchestrator 内联 `FakeRuntime` | Settings、`MessageEnvelope`、临时 sqlite | 🟡 |

---

## 1. `app/config/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P1 | `get_settings` / `load_env` | `test_config_settings.py` | 关键字段默认值、环境变量覆盖 | ✅ |
| P2 | `resolve_runtime_root` / 路径拼接 | 同上 | 相对路径展开、边界空串、`sys.frozen` 分支 | ✅ |

---

## 2. `app/context/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P1 | `OpenAIConversationContext` / `PendingToolCall` | `test_context_models.py` | 往返、`run_turn_phase` 规范化 | ✅ |
| P2 | context reset / clear | `test_context_clear.py` | reset 后 messages / pending 清空 | ✅ |
| P2 | `AgentSubmitRequest`（interface） | 与 §8 合并或单独 | resume 默认 priority 等 | 🟡 |

---

## 3. `app/harness/queue/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P0 | `MessageQueue` 优先级 | `test_message_queue.py` | `tool_result` / `human` / `resume` / `other` 出队顺序 | ✅ |
| P1 | 同优先级 FIFO | 同上 | 同优先二级 `_seq` 稳定 | ✅ |
| P1 | `enqueue` 泛型 envelope | 同上 | 自定义类型 + `getattr` 日志不崩 | ✅ |
| P2 | `pending_metrics_rows` | 合并在 `test_message_queue.py` | 堆顺序与出队一致 | ✅ |
| P1 | `stop` / `receive` 唤醒 | 同上 | 先 `pause` 再 `stop` 后 `receive` → `RuntimeError` | ✅ |

---

## 4. `app/harness/service/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P0 | `AgentService` 启动/停止 | `test_agent_service.py` | `start`/`stop`、`AsyncToolResultStore` 注册/注销 | ✅ |
| P0 | 消费一条 `human` 消息 | 同上 | mock orchestrator，断言入队消费 | 🟡 |
| P1 | `cancel_current_turn` | 同上 | 在途子 task 取消 | 🟡 |
| P1 | `_map_event_envelope_to_stream` | `test_agent_service.py` | `assistant` / `tool_result` / `approval_required` / `done` | ✅ |
| P1 | `handle_stream_event` 错误收口 | 同上 | 编排异常 → `error` + `done` | 🟡 |
| P1 | session 管理 | `test_agent_service_sessions.py` | 多 session、admin 路径 | ✅ |
| P2 | `release_session` / 队列上限淘汰 | `test_agent_service_capacity.py` | 闲置淘汰策略 | ⬜ |

> **说明**：§4 部分用例依赖 `AgentService` 完整导入链。未安装 `requirements.txt` 时相关类 **skip**；CI 安装依赖后应全部执行。

---

## 5. `app/harness/api/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P1 | `POST /v1/messages` 等路由 | `test_api_app.py` | 202/422、与 `AgentService` 接线 | ✅ |
| P1 | `GET /v1/streams` SSE | 同上 | `StreamingResponse`、`SseEncodingTests` | ✅ |
| P2 | `POST .../cancel` | 可合并 `test_api_app.py` | `cancelled` 布尔、幂等 | 🟡 |
| P2 | `POST /v1/sessions` | 同上 | session_id 生成/传入 | 🟡 |
| P2 | registry 自登记 | `RegistryRegistrationTests` | httpx mock 下游 | ✅ |

---

## 6. `app/harness/history/` 与 store

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P1 | `record_raw_openai_message_append` | `test_raw_message_journal.py` | 开关关闭不写、空 session 跳过 | ✅ |
| P1 | `append_openai_message_with_journal` | 同上 | messages 追加 + JSONL | ✅ |
| P2 | `insert_openai_message_with_journal` | 同上 | insert 顺序与 JSONL | ✅ |
| P2 | SQLite message store | `test_memory_store.py` | 会话内容读写 | ✅ |

---

## 7. `app/harness/streaming/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P2 | `InMemoryEventBus` | `test_streaming_events.py` | `publish` / `subscribe_all`、`seq` | ✅ |

---

## 8. `app/schemas/` 与 `interface.py`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P1 | `approval` 解析 | `test_schema_approval.py` | `parse_resume_tool_decision`、`is_tool_execution_approved` | ✅ |
| P2 | `agent_peer` 信封 | `test_schema_agent_peer.py` | `build_agent_peer_envelope` / `parse_*` | ✅ |

---

## 9. `app/core/main_agent/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P1 | `get_system_prompt` | `test_main_agent_prompt.py` | mock `HostSnapshot`、skills 段、缓存 | ✅ |
| P0 | `MainAgentTurnOrchestrator` 分支 | `test_main_agent_orchestrator.py` | mock runtime 各消息类型 | ✅ |
| P1 | `_handle_human_message` + pending 打断 | 同上 | `pending_tool_calls`、`run_turn_phase` | 🟡 |
| P2 | `_invoke_tool` + SSE | 同上 | dict/list/str、错误路径 | 🟡 |
| P2 | reasoning 持久化 | `test_runtime_openai_thinking.py` | thinking / message record | ✅ |

---

## 10. `app/harness/tools/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P1 | `bash_run` schema | `test_tool_schema_validation.py` | 必填 command、拒绝未知参数 | ✅ |
| P1 | `bash_run` su/sudo 守卫 | `test_bash_su_guard.py`（Go：`bash_policy_test.go` ✅） | 非 root 拦截 / `sudo -n` 放行 | 🟡 |
| P2 | `@tool` 装饰器 | `test_tooling.py`；部分 `test_async_tool_store.py` | async_store 通知 | 🟡 |
| P2 | `agent_peer` HTTP | `test_agent_peer_tools.py` | discover、send、审批、relay | ✅ |
| P2 | FS / 用户信息 / 结果策略 | `test_fs_tools.py` 等 | 各工具边界 | ✅ |

---

## 11. `app/observability/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P2 | `parse_usage_tokens` / `sanitize_model_label` | `test_metrics_tokens.py` | 边界、负值钳制 | ⬜ |
| P2 | `record_llm_token_usage` | 同上 | Counter `inc`、独立 registry | ⬜ |
| P2 | `refresh_session_context_metrics` | `test_session_context_metrics.py` | session 移除后 series 清理 | ⬜ |
| P3 | metrics 文本冒烟 | `test_agent_peer_tools.py`（部分） | `metrics_text()` 含预期 label | 🟡 |

---

## 12. `app/config/host_snapshot.py`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P2 | `capture` / `get` 单例 | `test_host_snapshot.py` | 引用相等、惰性构建 | ⬜ |
| P3 | prompt 内嵌 mock | `test_main_agent_prompt.py`（部分） | 仅验证 prompt 拼接，非 capture 本身 | 🟡 |

---

## 13. `register_center/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P2 | 安全与 relay | `test_register_center_security.py` | token、持久化、resume relay | ✅ |
| P3 | 模型校验 | `test_register_center_models.py` | Pydantic 字段、normalize | ⬜ |

---

## 14. `tests/integration/`（非默认 discovery）

| 优先级 | 主题 | 现有/建议 | 说明 | 状态 |
|--------|------|-----------|------|------|
| Opt | 真机 LLM 冒烟 | `integration/live_llm_smoke.py` | `RUN_LIVE_LLM_TESTS=1` + `LLM_API_KEY`；显式模块名运行 | ✅ |

---

## 15. Python harness triggers（`app/harness/triggers/`）

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P1 | 条件 / store / scheduler | `test_triggers.py` | 校验、CRUD、调度 tick | ✅ |
| P2 | trigger 工具 | 同上 | agent 侧 trigger 工具调用 | ✅ |

> Go 侧 mirror：`node/internal/triggers`、`node/internal/api/triggers_api_test.go`、`node/internal/session/triggers_test.go`。

---

## 16. `app/cli/`（Textual TUI · `dagents-cli`）

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P1 | `dagents serve` 守护进程 | `test_cli_daemon.py` | pid、hook、stop/status、health | ✅ |
| P1 | session 控制器 | `test_cli_session_controller.py` | 渲染、非阻塞 HITL | ✅ |
| P1 | 子 Agent | `test_cli_child_agent.py` | scope、tracker、SSE 过滤 | ✅ |
| P1 | HITL 审批 | `test_cli_approval.py` | 审批 UI、SSE 解析 | ✅ |
| P2 | tool call 规范化 | `test_cli_tool_calls.py` | OpenAI tool_calls 结构 | ✅ |
| P2 | 用户信息 | `test_cli_user_information.py` | ask user information 流 | ✅ |

> Go Client mirror：`client/internal/tui/full`、`repl`、`shared`（见 Go 进度表）。

---

## 17. Go Node / Client（本地 Assistant 运行时）

| 优先级 | 主题 | 包 / 测试 | 断言方向 | 状态 |
|--------|------|-----------|----------|------|
| P0 | turn 编排 | `node/internal/turn` | human/tool/resume 闭环 | ✅ |
| P0 | 子 Agent | `node/internal/childagent` | async create/wait、**delivered 终态** | ✅ |
| P1 | session + 子 Agent API | `node/internal/session`、`api` | 绑定、HTTP | ✅ |
| P1 | bash 策略 | `node/internal/tools/bash_policy_test.go` | su/sudo 守卫 | ✅ |
| P1 | REPL 子 Agent SSE | `client/internal/tui/repl/stream_test.go` | 父会话不泄漏子事件 | ✅ |
| P1 | 全屏 TUI 子 Agent | `client/internal/tui/full/child_agent_test.go` | /children、事件 | ✅ |
| P2 | LLM mock | `node/internal/llm/mock_test.go` | 测试替身行为 | ✅ |
| P2 | skills | `node/internal/skills/skills_test.go` | 加载与列表 | ✅ |
| P2 | hostsnapshot | `node/internal/hostsnapshot` | capture 缓存 | ⬜ |
| P3 | cmd main | `node/cmd`、`client/cmd` | 仅 smoke 或 e2e | ⬜ |

---

## 18. 手动脚本（可选，不纳入 discovery）

| 文件 | 用途 | 状态 |
|------|------|------|
| `scripts/` 或 `tests/manual/` | API / runtime 联调脚本 | ⬜ |

---

## 建议实施顺序（迭代）

1. **已完成主干**：Python P0–P1（queue、service、api、orchestrator、CLI 子 Agent）；Go node/client 核心包与子 Agent / REPL 过滤。  
2. **下一批 Python**：`test_bash_su_guard.py`（补 Python 侧 su 守卫，与 Go `bash_policy` 对齐）、`test_metrics_tokens.py`、`test_host_snapshot.py`。  
3. **P2 收尾**：`test_agent_service_capacity.py`、`test_tooling.py`、observability session metrics。  
4. **Go 缺口**：`node/internal/hostsnapshot`、可选 cmd smoke。  
5. **P3**：`test_register_center_models.py`、integration 慢测、manual 脚本归档。

---

## 与 CI 对齐

| Workflow | 命令 |
|----------|------|
| **`pr-tests.yml`** | `python -m unittest discover -s tests -p "test_*.py" -v` |
| **`go-ac.yml`** | `go test ./shared/config/... ./node/... ./client/...` + 静态构建 smoke（`BUILD_CLIENT=1`） |
| **`build-and-release.yml`** | 同上 Python discover；release 前 `go test`（config + node；client 随 go-ac） |

**新增约定**：

- Python 文件：`test_<领域>.py`，类继承 `unittest.TestCase` 或 `IsolatedAsyncioTestCase`（`test_cli_daemon.py` 已迁入 discover，勿再用独立 pytest 入口）。  
- Go 文件：与同包源码并列的 `<name>_test.go`；子 Agent / REPL 等新行为优先补在对应 `internal/` 包内。

**本地一键**：

```bash
pip install -r requirements.txt
python -m unittest discover -s tests -p "test_*.py" -v
go test ./shared/config/... ./node/... ./client/...
```
