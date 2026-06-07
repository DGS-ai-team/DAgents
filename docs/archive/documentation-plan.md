# Architecture v2 文档规划

**第一步实施**：[agent-client-refactor-plan.md](./agent-client-refactor-plan.md)（Agent Node + Client）。  
Manage / A2A 文档保留供第二步使用。

---

## 1. 文档状态

| 文档 | 状态 |
|------|------|
| [agent-client-refactor-plan.md](./agent-client-refactor-plan.md) | **当前主计划** |
| [three-component-model.md](./three-component-model.md) | 有效 |
| [background-and-motivation.md](./background-and-motivation.md) | 有效 |
| [agent-node-api-sketch.md](./agent-node-api-sketch.md) | 有效 |
| [client-packaging.md](./client-packaging.md) | 有效 |
| [manage-api-sketch.md](./manage-api-sketch.md) | 有效（第二步） |
| [a2a-via-manage.md](./a2a-via-manage.md) | 有效（第二步） |
| [client-events-and-hitl.md](./client-events-and-hitl.md) | **待修订**（AC N1/N4） |
| [session-persistence.md](./session-persistence.md) | **待修订**（AC N5） |
| [builtin-tools-routing.md](./builtin-tools-routing.md) | **待修订**（AC N3） |
| [temporary-child-agents.md](./temporary-child-agents.md) | **待修订**（AC 之后） |
| [security-and-policy.md](./security-and-policy.md) | **待修订**（Manage 阶段） |
| [ownership-and-tenancy.md](./ownership-and-tenancy.md) | **待修订**（低优先级） |
| — `go-node-compatibility.md` | **待建**（AC N7） |
| — `agent-node-internals.md` | **待建**（AC N2 起） |

### 已删除（2026-05 清理）

旧 Brain/Body、Python Backend、Proxy control channel、phase1/2 计划等 **14 篇** 已移除，包括：

`runtime-split`、`agent-dual-runtime`、`api-v2-sketch`、`control-channel-protocol`、  
`phase1-dev-plan`、`phase2-dev-plan`、`phase2-core-completion`、`brain-body-responsibilities`、  
`cross-backend-coordination`、`identity-and-session`、`message-queue-and-execution-control`、  
`agent-lifecycle-and-registration`、`deployment-and-ops`、`migration-from-v1`。

---

## 2. 阅读顺序（Agent + Client 阶段）

1. [background-and-motivation.md](./background-and-motivation.md)  
2. [three-component-model.md](./three-component-model.md)  
3. [agent-client-refactor-plan.md](./agent-client-refactor-plan.md)  
4. [agent-node-api-sketch.md](./agent-node-api-sketch.md)  
5. [client-packaging.md](./client-packaging.md)  
6. [client-events-and-hitl.md](./client-events-and-hitl.md)（修订后）

---

## 3. 实施阶段

| 阶段 | 目标 | 文档 |
|------|------|------|
| **AC** | Agent Node + Client 闭环 | agent-client-refactor-plan |
| **M1** | Manage MVP | manage-api-sketch |
| **M2** | A2A 经 Manage | a2a-via-manage |

---

## 4. 仓库目录（计划）

```text
node/       # Agent Node（Go）— AC 阶段创建
client/     # Client TUI（Go）
manage/     # Manage（Python）— M1 阶段从 app/ 拆出
```

---

## 5. 维护约定

- 架构变更 → 先改 [three-component-model.md](./three-component-model.md) ADR。  
- Node API 变更 → [agent-node-api-sketch.md](./agent-node-api-sketch.md)。  
- 实施勾选 → [agent-client-refactor-plan.md](./agent-client-refactor-plan.md) 各 N 阶段 checklist。
