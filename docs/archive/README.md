# 文档归档

本目录存放**不再作为现行产品说明**的材料，避免新人把过期叙事当现状。

## 原则

| 类别 | 处理 |
|------|------|
| **现行** | `docs/handbook/`、现行 `docs/design/`、`docs/architecture/` |
| **冻结契约** | `docs/design/workgroup-d05-contracts.md` + fixtures |
| **归档** | 本目录；**不**链到根 README / handbook 主路径 |
| **远期未落地** | `docs/future/`（仅索引；具体稿可迁入本目录） |

## 根目录迁入

| 文档 | 说明 |
|------|------|
| [manage-communication.md](./manage-communication.md) | Manage 通信长文（含已拆除 A2A）；现行见 handbook/05 |
| [security-rollout.md](./security-rollout.md) | 早期安全分阶段验收；要点已并入 handbook/06 |
| [os-compatibility.md](./os-compatibility.md) | CPython/PyInstaller 兼容矩阵（Python Agent 时代） |
| [a2a-via-manage.md](./a2a-via-manage.md) | A2A Task 模型（已退役） |

## `design/` 归档

| 文档 | 说明 |
|------|------|
| [design/remote-agent-placement.md](./design/remote-agent-placement.md) | SUPERSEDED → Workgroup |
| [design/node-centric-architecture-cleanup.md](./design/node-centric-architecture-cleanup.md) | 一次性清理清单 |
| [design/v0.6.0-smoke-checklist.md](./design/v0.6.0-smoke-checklist.md) | 旧发版 smoke |
| [design/v0.6.1-smoke-checklist.md](./design/v0.6.1-smoke-checklist.md) | 旧发版 smoke |
| [design/v0.6.2-smoke-checklist.md](./design/v0.6.2-smoke-checklist.md) | 旧发版 smoke |
| [design/v0.7.0-smoke-checklist.md](./design/v0.7.0-smoke-checklist.md) | 旧发版 smoke |

已删除 stub：`design/major-changes.md`、`design/background-and-motivation.md`、`docs/triggers-design.md`（→ `node/internal/triggers/README.md`）。

## 其它

| 入口 | 说明 |
|------|------|
| [`../design/workgroup-and-node-gateway.md`](../design/workgroup-and-node-gateway.md) §15–§16 | 历史审核纪要 |
| [`../future/`](../future/) | 远期索引（空壳时可删） |

现行验收：[v0.9.1-smoke-checklist.md](../design/v0.9.1-smoke-checklist.md)。
