# 架构总览

DAgents 本地 **Agent 运行时** 为 **Go Agent Node**（`node/`）。Python 侧保留 **Textual TUI Client**（`app/cli/`）与 **Register Center**（`register_center/`）。

## 决策树

```text
需要本地单 Agent 助手（终端 / 老旧 OS）？
  └─ 是 → Go Agent Node + Client
         入口：go run ./node/cmd/dagents-node
               dagents chat / dagents-client tui

需要 Agent 登记 / A2A 中继（Register Center）？
  └─ 是 → python run_register_center.py（默认 :8010）
         文档：register_center/README.md
```

| 维度 | Go 本地助手栈 | Python 辅助组件 |
|------|---------------|-----------------|
| **Agent 运行时** | **`node/`（Go）** | 无（已移除 Python FastAPI Agent API） |
| **终端 Client** | Textual（`app/cli/`）+ Go TUI（`client/`） | — |
| **配置** | `packaging/agent-client/config.yaml` | Register Center 环境变量 |
| **会话持久化** | Node SQLite（`data_dir/sessions.db`） | — |
| **A2A 控制面** | — | Register Center relay/broadcast |

## 文档索引

| 主题 | 文档 |
|------|------|
| 本地助手联调 | [local-assistant.md](./local-assistant.md) |
| Agent Node HTTP/SSE | [agent-node-api.md](./agent-node-api.md) |
| 同包配置与安装 | [client-packaging.md](./client-packaging.md) |
| Register Center | [../../register_center/README.md](../../register_center/README.md) |
| 三组件远期模型 | [../design/three-component-model.md](../design/three-component-model.md) |
| AC 实施状态 | [../design/agent-client-refactor-plan.md](../design/agent-client-refactor-plan.md) |

## 历史说明

原 **Python FastAPI Agent API**（`run_agent_api.py`、`app/harness/`）已从仓库移除；HTTP/SSE 契约以 Go Node 为准（[agent-node-api.md](./agent-node-api.md)）。  
原 Python API 文档 [python-runtime.md](./python-runtime.md)、[api-reference.md](../api-reference.md) 仅作历史参考。

## 已移除方案

Brain/Body + Proxy、`app/**/v2/` execution 子目录 **不在本仓库**；Python Agent 运行时 **已移除**。
