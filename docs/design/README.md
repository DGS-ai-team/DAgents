# 设计文档

> **项目手册** → [../handbook/README.md](../handbook/README.md)  
> **归档** → [../archive/README.md](../archive/README.md)

## 现行（优先阅读）

| 文件 | 说明 |
|------|------|
| **[workgroup-and-node-gateway.md](./workgroup-and-node-gateway.md)** | 工作组产品规范（正文 §0–§13） |
| **[workgroup-d05-contracts.md](./workgroup-d05-contracts.md)** | D0.5 冻结契约 + fixtures |
| **[v0.9.1-smoke-checklist.md](./v0.9.1-smoke-checklist.md)** | v0.9.1 预览验收清单 |
| **[agent-instance-model.md](./agent-instance-model.md)** | 单 Node 多 Agent、模板、Web UI |

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
| `tool-before-hook-duplicate-approval.md` | 重复工具调用审批 |
| `ux-agent-owned-file-approval.md` | 写盘信任链 |
| `skills-context-cost-analysis.md` | Skills 成本（搁置存档） |
| `browser-tools-and-demonstration.md` | Browser 工具 |
| `browser-remote-service-mode-a.md` | Browser 薄服务 |
| `node-ui-media-display.md` | Web UI 媒体 |
| `windows-desktop-shell.md` | Desktop Shell |

## 已迁出本目录

过期设计、superseded 稿与旧版 smoke 见 [../archive/design/](../archive/design/)。

新正文优先写入 **handbook**；工作组契约变更写入 **workgroup-d05** / CHANGELOG。
# Linux channel design

See [linux-channel-plan.md](./linux-channel-plan.md) for the Node-side Linux SSH channel design.
# Shell execution design

See [shell-execution-plan.md](./shell-execution-plan.md) for the local/remote Bash, Process, PTY, Sandbox and Exec Server design.
