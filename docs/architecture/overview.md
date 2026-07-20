# 架构总览

> **项目手册** → [../handbook/01-愿景与架构.md](../handbook/01-愿景与架构.md)  
> **v0.8 重构（进行中）** → [../design/agent-instance-model.md](../design/agent-instance-model.md)

DAgents 本地运行时为 **Go Agent Node**（`node/`）+ 内嵌 **Web UI**（`/ui/`）。Manage 控制面（`manage/`）与 A2A 将在 v0.8 后续 Phase 重构。

## 目标架构（v0.8+）

```text
单 Node 进程（node_id）→ 多 Agent 实例（agent_id）
  每 Agent：1 主对话 + 可选沙箱工作区
  唯一交互：Web UI
```

## 决策树

```text
需要本地多 Agent 助手？
  └─ 启动 dagents-node → 浏览器打开 /ui/ → 按模板新建 Agent

需要跨服务器 Agent 协作？
  └─ Manage + A2A（Phase 5 重构后）→ manage/README.md
```

| 维度 | Go Agent Node | 其他 |
|------|---------------|------|
| **运行时** | `node/`（Go） | — |
| **人机交互** | Web UI（`node/webui/`） | TUI/CLI 将移除 |
| **控制面** | — | `manage/`（待重构） |
| **配置** | `config.yaml`（Node 全局）+ Agent 模板 + 实例快照 | |
| **持久化** | `agents.db` + `agents/<agent_id>/`（替代 sessions） | |

## 文档索引

| 主题 | 文档 |
|------|------|
| **v0.8 Agent 实例模型** | [agent-instance-model.md](../design/agent-instance-model.md) |
| Node 内部结构 | [go-node-internals.md](./go-node-internals.md) |
| HTTP/SSE（待更新） | [agent-node-api.md](./agent-node-api.md) |
| 子 Agent | [child-agent-tools.md](./child-agent-tools.md) |
| Manage | [../../manage/README.md](../../manage/README.md) |
