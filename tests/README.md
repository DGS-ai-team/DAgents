# `tests/`

Python 单元测试目录；Go 测试在 `node/`、`client/`、`shared/config/` 各包内（`*_test.go`）。

**当前规模**：Python **157** 例（`unittest discover`）；Go **~149** 个 `Test*`（`go test`，含 2 SKIP）。详细优先级与缺口见 **`UNIT_TEST_CHECKLIST.md`**。

---

## 目录与子目录

| 路径 | 说明 |
|------|------|
| **`UNIT_TEST_CHECKLIST.md`** | 单元测试清单：Manage + CLI + Go 覆盖表、CI 与实施顺序 |
| **`test_support/`** | 单测替身（如 `stub_settings.py`） |
| **`integration/`** | 可选联网集成测试（默认 skip）；见子目录 **`README.md`** |

---

## Python 用例文件（`test_*.py`）

### Manage（`manage/`）

| 文件 | 说明 |
|------|------|
| `test_manage_m0_m1.py` | Registry：注册、心跳、discover |
| `test_manage_m2_a2a.py`、`test_manage_a2a_store.py` | A2A Task、inbox、HITL 中继 |
| `test_manage_skills.py`、`test_manage_llm.py` | Skills / LLM 目录 API |
| `test_manage_releases.py` | Release Hub 上传与发布 |
| `test_manage_cases.py`、`test_manage_case_tool_resolve.py` | Cases CRUD |
| `test_manage_externaltools.py` | ExternalTools |
| `test_manage_admin.py`、`test_manage_storage_schema.py` | Admin 只读、DB schema |
| `test_manage_text_sanitize.py` | 文本消毒 |

### Textual CLI（`app/cli/` · `dagents chat`）

| 文件 | 说明 |
|------|------|
| `test_cli_session_controller.py` | session 控制器渲染 |
| `test_cli_child_agent.py` | 子 Agent scope / tracker / SSE 过滤 |
| `test_cli_approval.py`、`test_cli_hitl_batch.py`、`test_cli_hitl_large_args.py` | HITL 审批、SSE 解析 |
| `test_cli_tool_calls.py`、`test_cli_render_tokens.py` | tool call 规范化与渲染 |
| `test_cli_user_information.py` | CLI 用户信息收集 |
| `test_cli_a2a_relay.py` | A2A 中继展示 |
| `test_cli_policy.py`、`test_cli_triggers.py` | policy / triggers |
| `test_cli_last_session.py`、`test_cli_welcome_panel.py` | 会话恢复与欢迎面板 |
| `test_tui_usage_layout.py`、`test_transcript_log.py` | TUI 布局与 transcript |

### 横切 / 渲染

| 文件 | 说明 |
|------|------|
| `test_smoke.py` | 工作区可导入（CI 非空 discovery） |
| `test_render_sanitize.py`、`test_tool_call_streaming.py` | 渲染与流式 tool call |

### 非 discover

| 文件 | 说明 |
|------|------|
| `integration/live_llm_smoke.py` | 真机 LLM 冒烟；显式模块名运行，见 `integration/README.md` |

> 原 Python **Agent API**（`app/harness/`）单测已随运行时移除；对等行为由 **Go Node** 包内 `*_test.go` 覆盖。

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
python -m unittest tests.test_manage_m0_m1 -v
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

补写用例前请先阅读 **`UNIT_TEST_CHECKLIST.md`**。下一批缺口：`node/internal/hostsnapshot` 单测；Manage Workflow 落地后补 `manage/workflows/` 用例。
