# 单元测试清单

> **Python 约定**：`python -m unittest discover -s tests -p "test_*.py" -v`；**不**自动发现 `tests/integration/`（见该子目录 README）。  
> **Go 约定**：`go test ./shared/config/... ./node/... ./client/...`（各包内 `*_test.go`）。  
> **状态图例**：`✅` 已有对应用例且默认 discover / `go test` 会跑；`🟡` 已建文件但部分场景未覆盖或依赖完整 `requirements.txt` / 环境；`⬜` 尚未建建议文件；`—` Go 侧无对应 Python 模块或反之。

**进度快照**：测试数量以当前代码和 CI 实际运行结果为准。

> **已移除**：原 Python Agent API（`app/harness/`、`app/core/` 等）及对应单测；见下方归档说明。行为覆盖见 **Go Node** 各包 `*_test.go` 与 **§13、§16**。

---

## 进度快照 — Python（`tests/test_*.py`）

| 文件 | 覆盖 | 备注 |
|------|------|------|
| `test_smoke.py` | 横切 | 工作区可导入 |
| `test_manage_*.py` | §13 Manage | Registry、Skills、LLM、Releases、Cases、Admin、Workgroup |
| `test_workgroup_*.py` | Workgroup | D05 索引与 store golden 用例 |
| `integration/live_llm_smoke.py` | §14 | 显式模块运行，非 discover |

**下一批 Python 缺口**：Manage Workflow 落地后补 `test_manage_workflows.py`。

**已归档（文件已删）**：`test_agent_service.py`、`test_main_agent_orchestrator.py`、`test_message_queue.py` 等 — 见 §2–§12 表；等价逻辑在 Go `node/internal/*_test.go`。

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
| `client/internal/probe` | `probe_test.go` | 健康探测 | ✅ |
| `client/internal/desktop` | `update_test.go` | 桌面更新辅助 | ✅ |
| `client/internal/update` | `update_test.go` | Release Hub 更新 | ✅ |
| `node/internal/hostsnapshot` | — | 宿主机快照 | ⬜ |
| `node/cmd/dagents-node` | — | main 入口 | ⬜ |
| `client/cmd/dagents-client` | — | main 入口 | ⬜ |
| `node/internal/version` | — | 版本常量（全项目唯一） | ⬜ |

---

## 已归档（Python Agent API）

原 `app/harness/`、`app/core/`、`app/context/`、`app/schemas/`、`app/observability/`、`app/harness/triggers/` 及对应 `test_*.py` 已随运行时移除。下列能力由 **Go Node** 覆盖：

| 原 Python 域 | Go 包 / 测试 |
|-------------|-------------|
| queue / session / turn | `node/internal/queue`、`session`、`turn` |
| API / SSE | `node/internal/api`、`stream` |
| tools / policy | `node/internal/tools`、`policy` |
| triggers | `node/internal/triggers` |
| store / history | `node/internal/store`、`history` |

勿再补 Python harness 用例。

---

## 13. `manage/`

| 优先级 | 主题 | 建议用例 | 断言方向 | 状态 |
|--------|------|----------|----------|------|
| P1 | Registry | `test_manage_m0_m1.py` | 注册、心跳、discover | ✅ |
| P2 | Skills / LLM / Releases | `test_manage_skills.py`、`test_manage_llm.py`、`test_manage_releases.py` | 上传、发布、check | ✅ |
| P2 | Cases / ExternalTools | `test_manage_cases.py`、`test_manage_externaltools.py` | CRUD、JSONL | ✅ |
| P2 | Admin / Schema | `test_manage_admin.py`、`test_manage_storage_schema.py` | 只读列表、DB schema | ✅ |

---

## 14. `tests/integration/`（非默认 discovery）

| 优先级 | 主题 | 现有/建议 | 说明 | 状态 |
|--------|------|-----------|------|------|
| Opt | 真机 LLM 冒烟 | `integration/live_llm_smoke.py` | `RUN_LIVE_LLM_TESTS=1` + `LLM_API_KEY`；显式模块名运行 | ✅ |

---

## 16. Go Node / Client（本地 Assistant 运行时）

| 优先级 | 主题 | 包 / 测试 | 断言方向 | 状态 |
|--------|------|-----------|----------|------|
| P0 | turn 编排 | `node/internal/turn` | human/tool/resume 闭环 | ✅ |
| P0 | 子 Agent | `node/internal/childagent` | async create/wait、**delivered 终态** | ✅ |
| P1 | session + 子 Agent API | `node/internal/session`、`api` | 绑定、HTTP | ✅ |
| P1 | bash 策略 | `node/internal/tools/bash_policy_test.go` | su/sudo 守卫 | ✅ |
| P2 | LLM mock | `node/internal/llm/mock_test.go` | 测试替身行为 | ✅ |
| P2 | skills | `node/internal/skills/skills_test.go` | 加载与列表 | ✅ |
| P2 | hostsnapshot | `node/internal/hostsnapshot` | capture 缓存 | ⬜ |
| P3 | cmd main | `node/cmd`、`client/cmd` | 仅 smoke 或 e2e | ⬜ |

---

## 17. 手动脚本（可选，不纳入 discovery）

| 文件 | 用途 | 状态 |
|------|------|------|
| `scripts/` 或 `tests/manual/` | API / runtime 联调脚本 | ⬜ |

---

## 建议实施顺序（迭代）

1. **已完成主干**：Manage Registry、Go Node/Client 核心包与 Workgroup 协作。
2. **Go 缺口**：`node/internal/hostsnapshot`、可选 cmd smoke。  
3. **Python 下一批**：Manage Workflow 落地后补 `test_manage_workflows.py`；integration 慢测按需。  
4. **P3**：manual 脚本归档、Node Prometheus 指标单测（落地后）。

---

## 与 CI 对齐

| Workflow | 命令 |
|----------|------|
| **`pr-tests.yml`** | `python -m unittest discover -s tests -p "test_*.py" -v` |
| **`go-ac.yml`** | `go test ./shared/config/... ./node/... ./client/...` + 静态构建 smoke（`BUILD_CLIENT=1`） |
| **`build-and-release.yml`** | 同上 Python discover；release 前 `go test`（config + node；client 随 go-ac） |

**新增约定**：

- Python 文件：`test_<领域>.py`，类继承 `unittest.TestCase` 或 `IsolatedAsyncioTestCase`。
- Go 文件：与同包源码并列的 `<name>_test.go`；新行为优先补在对应 `internal/` 包内。

**本地一键**：

```bash
pip install -r requirements.lock -r requirements-dev.txt
python -m unittest discover -s tests -p "test_*.py" -v
go test ./shared/config/... ./node/... ./client/...
```
