# DAgents 文档索引

本目录存放项目规划与子系统设计说明；**与实现代码分离**（代码在仓库根 **`app/`**）。

## 当前代码布局（与实现对齐）

| 路径 | 说明 |
|------|------|
| **`app/config/`** | **`Settings`** / **`get_settings`**；**`load_env`**（`.env`） |
| **`app/core/main_agent/`** | 主 Agent：`init_agent()`、`model`、`prompt` 等 |
| **`app/harness/`** | 工具、CLI、memory、服务与 API 等能力（无独立 `graph.py` 时组装在 **`main_agent/agent.py`**） |
| **`app/harness/tools/`** | `get_tools()` |
| **`app/harness/memory/`** | SQLite 记忆存储（`SqliteMessageStore`） |
| **`app/harness/cli/`** | 命令行入口 `main()` |

启动脚本：

- **`run_agent.py`**（CLI）
- **`run_agent_api.py`**（FastAPI）
- **`run_register_center.py`**（Register Center）
- **`run_dev_stack.py`**（本地联调：统一拉起 API + Register Center + Frontend）
- **`export_openapi_schema.py`**（导出 OpenAPI 到 `frontend/openapi.json`）

---

## 文档列表

| 文档 | 说明 |
|------|------|
| [项目实现总览.md](./项目实现总览.md) | 当前实现总览（架构图、模块职责、模块内逻辑、实现状态清单） |
| [项目规划.md](./项目规划.md) | 总目标、功能清单、里程碑（**管理端暂缓**） |
| [异步工具执行-设计.md](./异步工具执行-设计.md) | **异步工具执行方案**：sync/async 工具分流、后台执行器、结果回灌（运行中与自动唤醒两种场景） |

代码目录维护 **`README.md`** / **`REFERENCE.md`** 约定见 **`.cursor/rules/folder-readme.mdc`**。
