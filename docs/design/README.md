# 当前设计与契约

设计文档只记录“为什么这样实现、边界是什么、如何验证”。已经落地的设计必须以当前代码和测试复核；未落地内容必须明确标为规划，不能被用户指南引用为现有能力。

## 冻结/现行契约

| 文档 | 负责的问题 |
|---|---|
| [workgroup-d05-contracts.md](workgroup-d05-contracts.md) | Workgroup 的消息、WS、ACL、HITL、恢复和 JSON fixtures |
| [workgroup-and-node-gateway.md](workgroup-and-node-gateway.md) | Workgroup 产品边界和 Node↔Manage 连接模型 |
| [agent-instance-model.md](agent-instance-model.md) | Node 多 Agent、AgentRef、模板和实例边界 |
| [agent-hooks.md](agent-hooks.md) | Node Hook 扩展点和执行阶段 |
| [terminal-websocket.md](terminal-websocket.md) | Terminal WebSocket 输入、输出和生命周期 |
| [browser-remote-service-mode-a.md](browser-remote-service-mode-a.md) | Browser sidecar 与 Node 任务工具边界 |

## 当前专题

| 文档 | 状态 |
|---|---|
| [manage-architecture.md](manage-architecture.md) | Manage/Registry/Workgroup 现网架构，历史 A2A 仅作背景 |
| [release-update-hub.md](release-update-hub.md) | Release Hub 与客户端更新链路 |
| [shell-execution-plan.md](shell-execution-plan.md) | Shell、本地/SSH PTY 与 Terminal 执行边界 |
| [node-mcp-client-implementation.md](node-mcp-client-implementation.md) | Node MCP 客户端现行实现 |
| [turn-side-effects-refactor.md](turn-side-effects-refactor.md) | Produce / Apply / Continue 旁路副作用模型 |
| [context-compression-cache-analysis.md](context-compression-cache-analysis.md) | 压缩与 Prompt Cache 的实现约束 |
| [agent-quality.md](agent-quality.md) | Agent 效果目标、上下文边界、工具质量和验收矩阵 |
| [tool-before-hook-duplicate-approval.md](tool-before-hook-duplicate-approval.md) | 重复工具调用的审批策略 |
| [ux-agent-owned-file-approval.md](ux-agent-owned-file-approval.md) | Agent 自有文件写入的审批信任链 |
| [ui-e2e-regression-checklist.md](ui-e2e-regression-checklist.md) | 当前 Node Web UI 回归清单 |
| [ui-product-grade-redesign.md](ui-product-grade-redesign.md) | Node Web UI 产品级评审、已落地的核心改造与持续精修基线 |

## 规划中

| 文档 | 说明 |
|---|---|
| [linux-channel-plan.md](linux-channel-plan.md) | Linux/SSH 通道产品方案；实现状态以代码为准 |
| [workgroup-agent-membership-ui.md](workgroup-agent-membership-ui.md) | Workgroup 成员 UI 的后续增强 |

后续能力的旧规划见 [`archive/reports/manage-phase2-capabilities.md`](../archive/reports/manage-phase2-capabilities.md)，不构成当前 API 承诺。旧版本验收入口 [`v0.9.1-smoke-checklist.md`](v0.9.1-smoke-checklist.md) 仅是兼容指针，正文位于 `docs/archive/releases/`。媒体展示、桌面 Shell 和旧 OS 矩阵等历史方案位于 `docs/archive/design/` 或 `docs/archive/`。

## 已归档的材料

带日期的实施计划、验证报告、一次性审计、旧版本 smoke 和已停止实验不再放在当前设计入口，统一见 [archive/README.md](../archive/README.md)。对标研究仍在 [comparative-analysis/](../comparative-analysis/)，因为它们需要按日期持续追加且不定义 DAgents 契约。

新增设计前先确认是否能更新现有文档；同一主题不得同时维护“方案稿 + 实现状态稿 + 验证报告”三份当前正文。实现完成后，将结论合并到架构/参考文档，再把过程稿归档。
