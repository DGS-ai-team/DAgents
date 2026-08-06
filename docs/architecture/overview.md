# 架构总览

> **项目手册** → [../handbook/01-愿景与架构.md](../handbook/01-愿景与架构.md)  
> **Agent 实例模型** → [../design/agent-instance-model.md](../design/agent-instance-model.md)（文首含 v0.9.1 勘误）  
> **工作组** → [../handbook/07-Workgroup协作.md](../handbook/07-Workgroup协作.md)

DAgents 本地运行时为 **Go Agent Node**（`node/`）+ 内嵌 **Web UI**（`/ui/`）。可选 **Manage**（Registry + **Workgroup** + Console）。

## 目标架构（v0.9 预览）

```text
单 Node 进程（node_id）→ 多 Agent 实例（agent_id）
  每 Agent：1 主对话；工具边界 = 工具组 + policy + fs_root
  人机：Web UI /ui/
  跨机：Manage Workgroup（Supervisor + Member Home Node）
```

## 决策树

```text
需要本地多 Agent 助手？
  └─ 启动 dagents-node → 浏览器打开 /ui/ → 按模板新建 Agent

需要跨服务器成员协作？
  └─ 启动 Manage → 启用 manage.workgroup → 建组 / 加成员
     → handbook/07-Workgroup协作.md
```

| 维度 | 现行 |
|------|------|
| **运行时** | `node/`（Go） |
| **人机交互** | Web UI（`node/webui/`） |
| **控制面** | `manage/`（Registry · Workgroup · Console） |
| **配置** | `config.yaml`（引导）+ SQLite + Agent 模板 |
| **持久化** | `agents.db` + `.runtime/` |
| **沙箱** | **已移除** |
| **Placement** | **已拆除** |

## 文档索引

| 主题 | 文档 |
|------|------|
| Agent 实例模型 | [agent-instance-model.md](../design/agent-instance-model.md) |
| Workgroup 规范 | [workgroup-and-node-gateway.md](../design/workgroup-and-node-gateway.md) |
| Node 内部结构 | [go-node-internals.md](./go-node-internals.md) |
| HTTP/SSE | [agent-node-api.md](./agent-node-api.md) |
| 子 Agent | [child-agent-tools.md](./child-agent-tools.md) |
| Manage | [../../manage/README.md](../../manage/README.md) |
| v0.9.1 验收 | [../design/v0.9.1-smoke-checklist.md](../design/v0.9.1-smoke-checklist.md) |
