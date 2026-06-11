# 架构总览

DAgents 本地 **Agent 运行时** 为 **Go Agent Node**（`node/`）。Python 侧保留 **Textual TUI Client**（`app/cli/`）与 **Register Center**（`register_center/`）。

## 决策树

```text
需要本地单 Agent 助手（终端 / 老旧 OS）？
  └─ 是 → Go Agent Node + Client
         入口：go run ./node/cmd/dagents-node
               dagents chat / dagents-client tui

需要 Agent 登记 / 跨 Agent 协作？
  └─ 新：**python run_manage.py**（默认 :8020）→ `manage/README.md`
     过渡：python run_register_center.py（:8010，M5 删除）
```

| 维度 | Go 本地助手栈 | Python 辅助组件 |
|------|---------------|-----------------|
| **Agent 运行时** | **`node/`（Go）** | 无（已移除 Python FastAPI Agent API） |
| **终端 Client** | Textual（`app/cli/`）+ Go TUI（`client/`） | — |
| **配置** | `packaging/agent-client/config.yaml` | Register Center 环境变量 |
| **会话持久化** | Node SQLite（`.runtime/memory/sessions.db`） | — |
| **A2A 控制面** | Manage（M2 inbox）/ 过渡 Register Center relay | — |

## 文档索引

| 主题 | 文档 |
|------|------|
| Node 内部结构 | [go-node-internals.md](./go-node-internals.md) |
| 本地助手联调 | [local-assistant.md](./local-assistant.md) |
| Agent Node HTTP/SSE | [agent-node-api.md](./agent-node-api.md) |
| 同包配置与安装 | [client-packaging.md](./client-packaging.md) |
| Register Center | [../../register_center/README.md](../../register_center/README.md)（**过渡，M5 移除**） |
| Manage | [../../manage/README.md](../../manage/README.md) |
| 三组件远期模型 | [../design/three-component-model.md](../design/three-component-model.md) |
| AC 实施状态 | [../design/agent-client-refactor-plan.md](../design/agent-client-refactor-plan.md) |

## 历史说明

原 **Python FastAPI Agent API**（`run_agent_api.py`、`app/harness/`）已从仓库移除；HTTP/SSE 契约以 Go Node 为准（[agent-node-api.md](./agent-node-api.md)）。  
相关文档已归档至 [../archive/python-agent-runtime/](../archive/python-agent-runtime/)；根目录旧链名为跳转桩。

## 已移除方案

Brain/Body + Proxy、`app/**/v2/` execution 子目录 **不在本仓库**；Python Agent 运行时 **已移除**。
