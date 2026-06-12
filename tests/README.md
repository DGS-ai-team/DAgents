# `tests/`

Python 单元测试目录；Go 测试在 `node/`、`client/`、`shared/config/` 各包内（`*_test.go`）。

**当前规模**：Python **182** 例（`unittest discover`）；Go **~149** 个 `Test*`（`go test`，含 2 SKIP）。详细优先级与缺口见 **`UNIT_TEST_CHECKLIST.md`**。

---

## 目录与子目录

| 路径 | 说明 |
|------|------|
| **`UNIT_TEST_CHECKLIST.md`** | 单元测试清单：Python + Go 覆盖表、章节状态、CI 与实施顺序 |
| **`test_support/`** | 单测替身（如 `stub_settings.py`）；orchestrator 相关 `FakeRuntime` 在对应测试文件内联 |
| **`integration/`** | 可选联网集成测试（默认 skip）；见子目录 **`README.md`** |

---

## Python 用例文件（`test_*.py`）

### 横切 / 配置 / 上下文

| 文件 | 说明 |
|------|------|
| `test_smoke.py` | 工作区可导入（CI 非空 discovery） |
| `test_config_settings.py` | `get_settings` / `load_env` / `resolve_runtime_root` |
| `test_context_models.py` | `OpenAIConversationContext` 往返与规范化 |
| `test_context_clear.py` | context reset / clear |

### Harness（queue · service · api · history · streaming）

| 文件 | 说明 |
|------|------|
| `test_message_queue.py` | `MessageQueue` 优先级、FIFO、`stop`/`receive`、观测堆 |
| `test_agent_service.py` | `AgentService` 生命周期、stream 映射（**无 `requirements` 时 skip**） |
| `test_agent_service_sessions.py` | session 管理、pending tool |
| `test_api_app.py` | FastAPI 路由、SSE 编码、registry 登记 |
| `test_raw_message_journal.py` | 原始消息 JSONL：`record_*` / `append_*` / `insert_*` |
| `test_memory_store.py` | SQLite 会话内容 |
| `test_streaming_events.py` | `InMemoryEventBus` |
| `test_triggers.py` | harness triggers：条件、store、scheduler、工具 |

### Schema · 主 Agent · 工具

| 文件 | 说明 |
|------|------|
| `test_schema_approval.py` | 工具审批 resume 解析 |
| `test_schema_agent_peer.py` | `agent_peer` 信封 build/parse |
| `test_main_agent_orchestrator.py` | `MainAgentTurnOrchestrator` 各消息分支 |
| `test_main_agent_prompt.py` | `get_system_prompt`、mock `HostSnapshot` |
| `test_runtime_openai_thinking.py` | reasoning 持久化 |
| `test_fs_tools.py` | 文件系统工具 |
| `test_agent_peer_tools.py` | A2A discover/send/relay |
| `test_user_information_tool.py` | `ask_user_information` |
| `test_tool_schema_validation.py` | `bash_run` schema |
| `test_tool_result_policy.py` | 工具结果策略 |
| `test_async_tool_store.py` | 异步工具 store / routing |

### Register Center · Textual CLI（`dagents-cli`）

| 文件 | 说明 |
|------|------|
| `test_cli_daemon.py` | `dagents serve` 守护进程（11 例，CI discover） |
| `test_cli_session_controller.py` | session 控制器渲染 |
| `test_cli_child_agent.py` | 子 Agent scope / tracker / SSE 过滤 |
| `test_cli_approval.py` | HITL 审批、SSE 解析 |
| `test_cli_tool_calls.py` | tool call 规范化 |
| `test_cli_user_information.py` | CLI 用户信息收集 |

> `test_agent_service.py` / `test_main_agent_orchestrator.py` 在导入链未就绪（如未安装 `openai`）时 **skip**；CI 安装 **`requirements.txt`** 后应全部执行。

### 非 discover

| 文件 | 说明 |
|------|------|
| `integration/live_llm_smoke.py` | 真机 LLM 冒烟；显式模块名运行，见 `integration/README.md` |

---

## Go 用例（包内 `*_test.go`）

| 区域 | 包（节选） | 说明 |
|------|------------|------|
| 配置 | `shared/config` | 配置加载、路径解析 |
| Node 核心 | `node/internal/turn`、`session`、`queue`、`store`、`stream` | turn 编排、session、持久化、SSE hub |
| 子 Agent | `node/internal/childagent`、`node/internal/api`（child API） | 创建/wait、**delivered 终态**、HTTP |
| 工具 / 策略 | `node/internal/tools`、`policy`、`hitl`、`llm`、`skills` | 含 `bash_policy_test.go`、mock LLM |
| Triggers | `node/internal/triggers`、`node/internal/session` | trigger 与调度 |
| Client TUI | `client/internal/tui/full`、`repl`、`shared` | 全屏 TUI、**REPL 子 Agent SSE 过滤**、/children |
| Client 其他 | `client/internal/api`、`hitl`、`probe` | HTTP 客户端、审批、健康探测 |

**尚无单测**：`node/internal/hostsnapshot`、`node/cmd/*`、`client/cmd/*`、`client/internal/tui` 顶层 dispatch。完整包级列表见 **`UNIT_TEST_CHECKLIST.md`** § Go 进度表。

---

## 运行（仓库根目录）

```bash
pip install -r requirements.txt
python -m unittest discover -s tests -p "test_*.py" -v
go test ./shared/config/... ./node/... ./client/...
```

单文件示例：

```bash
python -m unittest tests.test_smoke -v
python -m unittest tests.test_cli_daemon -v
go test ./node/internal/childagent/... -v
# 可选联网 LLM 冒烟（模块名非 test_*，不在 discover 中）
python -m unittest tests.integration.live_llm_smoke -v
```

---

## CI

| Workflow | 说明 |
|----------|------|
| **`pr-tests.yml`** | Python `unittest discover -s tests -p "test_*.py"` |
| **`go-ac.yml`** | `go test ./shared/config/... ./node/... ./client/...` + 静态构建 smoke（`BUILD_CLIENT=1`） |
| **`build-and-release.yml`** | release 前 Python discover + `go test`（config、node） |

---

## 规划入口

补写用例前请先阅读 **`UNIT_TEST_CHECKLIST.md`**。下一批 Python 缺口：`test_bash_su_guard.py`、`test_metrics_tokens.py`、`test_host_snapshot.py` 等；Go 缺口：`node/internal/hostsnapshot`。
