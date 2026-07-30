# 设计文档

> **项目手册** → [../handbook/README.md](../handbook/README.md)

## v0.8+ 架构（当前）

| 文件 | 说明 |
|------|------|
| **[agent-instance-model.md](./agent-instance-model.md)** | **主设计**：单 Node 多 Agent、模板、沙箱、`node_id`、Web UI-only、实施阶段 |
| **[workgroup-and-node-gateway.md](./workgroup-and-node-gateway.md)** | **现行产品规范**：工作组 + Timeline/RunHistory；成员资产绑 Manage；D0.9 完成 / D1 进行中 |
| **[workgroup-d05-contracts.md](./workgroup-d05-contracts.md)** | **D0.5 已冻结（§19 A）**：schema / 状态机 / fixtures；D0.9→D1 已开 |
| [remote-agent-placement.md](./remote-agent-placement.md) | **SUPERSEDED**：原 Placement/Edge，改走工作组 |
| [node-centric-architecture-cleanup.md](./node-centric-architecture-cleanup.md) | 过时逻辑清理（含拆 Placement/沙箱） |

内置 Agent 模板样例：`packaging/agent-templates/`。

## 专题（仍有效）

| 文件 | 说明 |
|------|------|
| `manage-architecture.md` | Manage 现状（**Phase 5 将随 A2A 重构**） |
| `manage-phase2-capabilities.md` | Manage Phase 2 规划 |
| `release-update-hub.md` | Release Hub |
| `agent-hooks.md` | Hook 扩展点 |
| `turn-side-effects-refactor.md` | 旁路事件 Produce/Apply |
| `context-compression-cache-analysis.md` | 压缩 × Prompt Cache |
| `tool-context-cost-analysis.md` | 工具链上下文成本 |
| `tool-before-hook-duplicate-approval.md` | 重复工具调用 Hook |
| `ux-agent-owned-file-approval.md` | Agent 文件信任审批 |
| `background-and-motivation.md` | 重构动机 |
| `major-changes.md` | 重大变更实录 |
| `browser-tools-and-demonstration.md` | Browser 工具 |
| `browser-remote-service-mode-a.md` | Browser 远程服务模式 A |
| `node-ui-media-display.md` | Web UI 媒体展示 |
| `windows-desktop-shell.md` | Windows Desktop Shell |

## 已移除（v0.8 架构替代）

以下文档已删除，内容被 `agent-instance-model.md` 取代：

- `three-component-model.md` — 三组件 + TUI Client
- `agent-client-refactor-plan.md` — 已完成归档
- `web-ui-redesign-v0.6.1.md` — v0.6 session 中心 UI
- `v0.6-v0.7-roadmap.md` — v0.6–v0.7 路线

新正文优先写入 **handbook** 与 **agent-instance-model.md**。
