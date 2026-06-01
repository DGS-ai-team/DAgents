# 架构总览

DAgents 仓库内存在 **两条可并行使用的运行时栈**，按场景选择，勿混用 endpoint。

## 决策树

```text
需要本地单 Agent 助手（终端 / 老旧 OS）？
  └─ 是 → Go Agent Node + Client（见 local-assistant.md）
         入口：go run ./node/cmd/dagents-node
               dagents chat / dagents-client tui

需要 Web UI / OpenAPI / A2A / Register Center？
  └─ 是 → Python FastAPI Agent API（**Agent 运行时已弃用**，仍维护联调）
         入口：python run_agent_api.py（默认 :8000）
               python run_dev_stack.py（API + Register Center）
         替代（本地助手）：Go Agent Node
```

| 维度 | Go 本地助手栈 | Python API 栈 |
|------|---------------|---------------|
| **状态** | **当前主线** | **Agent 运行时已弃用**（UI/A2A 仍维护） |
| **主代码** | `node/`、`client/`、`shared/config/` | `app/`、`register_center/` |
| **终端 Client** | Textual TUI（`app/cli/`）+ Go REPL（`client/`） | 浏览器 [DAgentsUI](https://github.com/DGS-ai-team/DAgentsUI) |
| **配置** | `packaging/agent-client/config.yaml` | `.env` + `app/config/settings.py` |
| **会话持久化** | Node SQLite（`data_dir/sessions.db`） | 可选 SQLite（`app/harness/memory/`） |
| **A2A / 多 Agent** | 未实现 | `agent_peer` + Register Center |
| **触发器** | 完整：`interval` / `fire_at` / 日历 `schedule` + `cmd` 门控 | MVP：`interval` / `fire_at` |
| **发布** | 源码 `go run`（N7 二进制发布待完成） | PyInstaller `dagents-backend-*` |

## 文档索引

| 主题 | 文档 |
|------|------|
| 本地助手联调 | [local-assistant.md](./local-assistant.md) |
| Agent Node HTTP/SSE | [agent-node-api.md](./agent-node-api.md) |
| 同包配置与安装 | [client-packaging.md](./client-packaging.md) |
| Python 分层与流程 | [python-runtime.md](./python-runtime.md) |
| Python HTTP 契约 | [../api-reference.md](../api-reference.md) |
| 三组件远期模型 | [../design/three-component-model.md](../design/three-component-model.md) |
| AC 实施状态 | [../design/agent-client-refactor-plan.md](../design/agent-client-refactor-plan.md) |

## 已移除方案

Brain/Body + Proxy、`app/**/v2/` execution 子目录 **不在本仓库**；相关路由文档已归档至 [../archive/builtin-tools-routing.md](../archive/builtin-tools-routing.md)。
