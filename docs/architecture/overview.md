# 架构总览

> **已收敛至项目手册** → [../handbook/01-愿景与架构.md](../handbook/01-愿景与架构.md) · [handbook/README.md](../handbook/README.md)

DAgents 本地 **Agent 运行时** 为 **Go Agent Node**（`node/`）。Python 侧：**Textual TUI Client**（`app/cli/`）与 **Manage 控制面**（`manage/`）。

## 决策树

```text
需要本地单 Agent 助手（终端 / 老旧 OS）？
  └─ 是 → Go Agent Node + Client
         入口：go run ./node/cmd/dagents-node
               dagents chat / dagents-client tui

需要 Agent 登记 / 跨 Agent 协作？
  └─ python run_manage.py（默认 :8020）或 Docker Manage → manage/README.md
```

| 维度 | Go 本地助手栈 | Python 辅助组件 |
|------|---------------|-----------------|
| **Agent 运行时** | `node/`（Go） | — |
| **终端 Client** | Textual（`app/cli/`）+ Go TUI（`client/`） | — |
| **控制面** | — | `manage/`（Registry、A2A、Console） |
| **配置** | `packaging/agent-client/config.yaml` | Manage 环境变量 / Docker compose |
| **会话持久化** | Node SQLite（`.runtime/memory/sessions.db`） | — |

## 文档索引

| 主题 | 文档 |
|------|------|
| Node 内部结构 | [go-node-internals.md](./go-node-internals.md) |
| 本地助手联调 | [local-assistant.md](./local-assistant.md) |
| Agent Node HTTP/SSE | [agent-node-api.md](./agent-node-api.md) |
| 同包配置与安装 | [client-packaging.md](./client-packaging.md) |
| Manage | [../../manage/README.md](../../manage/README.md) |
| 三组件模型 | [../design/three-component-model.md](../design/three-component-model.md) |
