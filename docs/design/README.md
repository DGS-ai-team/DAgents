# 设计文档

> **项目手册** → [../handbook/README.md](../handbook/README.md)  
> **归档策略** → [../archive/README.md](../archive/README.md)

## 现行（优先阅读）

| 文件 | 说明 |
|------|------|
| **[workgroup-and-node-gateway.md](./workgroup-and-node-gateway.md)** | 工作组产品规范；D0.5–D5 分期（正文 §0–§13） |
| **[workgroup-d05-contracts.md](./workgroup-d05-contracts.md)** | D0.5 冻结契约 + fixtures |
| **[v0.9.1-smoke-checklist.md](./v0.9.1-smoke-checklist.md)** | v0.9.1 预览验收与文档收口清单 |
| **[agent-instance-model.md](./agent-instance-model.md)** | 单 Node 多 Agent、模板、Web UI；**沙箱章节已过时**（运行时已移除，以 CHANGELOG 为准） |

内置 Agent 模板：`packaging/agent-templates/`。

## 专题（仍有效）

| 文件 | 说明 |
|------|------|
| `manage-architecture.md` | Manage 架构 |
| `manage-phase2-capabilities.md` | Manage Phase 2 规划 |
| `release-update-hub.md` | Release Hub |
| `agent-hooks.md` | Hook 扩展点 |
| `turn-side-effects-refactor.md` | 旁路事件 Produce/Apply |
| `context-compression-cache-analysis.md` | 压缩 × Prompt Cache |
| `tool-context-cost-analysis.md` | 工具链上下文成本 |
| `browser-tools-and-demonstration.md` | Browser 工具 |
| `node-ui-media-display.md` | Web UI 媒体 |
| `windows-desktop-shell.md` | Desktop Shell（部分清单项可能滞后于实现） |

## SUPERSEDED / 历史

| 文件 | 状态 |
|------|------|
| [remote-agent-placement.md](./remote-agent-placement.md) | **SUPERSEDED** → Workgroup |
| [node-centric-architecture-cleanup.md](./node-centric-architecture-cleanup.md) | 清理清单（含已完成的沙箱/Placement 拆除） |
| workgroup 文 §15–§16 | 历史审核纪要 |

## 已移除（勿再引用）

以下曾存在、已由现行架构替代：`three-component-model.md`、`agent-client-refactor-plan.md`、`web-ui-redesign-v0.6.1.md`、`v0.6-v0.7-roadmap.md`。

新正文优先写入 **handbook**；契约变更写入 **workgroup-d05** / CHANGELOG。
