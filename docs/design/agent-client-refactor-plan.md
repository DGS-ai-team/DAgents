# Agent Node + Client 重构计划（归档）

> **状态：已完成**（AC N0–N7，2026-06）。**现网正文**以 [handbook/01-愿景与架构.md](../handbook/01-愿景与架构.md)、[handbook/02-Agent-Node-核心.md](../handbook/02-Agent-Node-核心.md) 与 [CHANGELOG.md](../../CHANGELOG.md) 为准。

本文件曾为 Go Node + Client 闭环的实施清单（N0 骨架 → N7 交叉编译/RHEL6）。Python Agent API（`app/harness/`）已移除；本地运行时仅为 **Go Node**。

| 主题 | 文档 |
|------|------|
| 三组件边界 | [three-component-model.md](./three-component-model.md) → handbook §2 |
| HTTP/SSE 契约 | [agent-node-api.md](../architecture/agent-node-api.md) |
| 打包与同包发布 | [client-packaging.md](../architecture/client-packaging.md) |
| Manage / A2A | [manage-architecture.md](./manage-architecture.md) |
| RHEL6 验收 | [rhel6-acceptance-checklist.md](../architecture/rhel6-acceptance-checklist.md) |

完整 N0–N7 勾选记录见 Git 历史（本文件 2026-07 收敛为跳转桩）。
