# 技术文档（`docs/`）

DAgents 文档分四层：**架构（现网）** → **设计（决策与优化）** → **专题（参考）** → **归档 / 远期**。实现以 **Go Agent Node**（`node/`）为准；Python 仅保留 TUI（`app/cli/`）与 **Manage**（`manage/`）。

**命名**：Markdown 文件名使用 **纯 ASCII**。

---

## 先读什么

| 你是谁 | 建议路径 |
|--------|----------|
| 新同学 / 联调 | [architecture/local-assistant.md](./architecture/local-assistant.md) → [architecture/agent-node-api.md](./architecture/agent-node-api.md) |
| 改 Node 内部 | [architecture/go-node-internals.md](./architecture/go-node-internals.md) + 各包 `node/internal/*/README.md` |
| 回顾重大优化 | [design/major-changes.md](./design/major-changes.md) |
| 查 Manage 通信 | [manage-communication.md](./manage-communication.md)（全量端点与流向） |
| 查工具列表 | [built-in-tools-reference.md](./built-in-tools-reference.md)（全量）· [built-in-tools.md](./built-in-tools.md) §0（索引） |

---

## 1. 架构（`architecture/`）

已落地的运行时与 API 契约。

| 文件 | 说明 |
|------|------|
| [overview.md](./architecture/overview.md) | 选型总览（Go Node + Manage） |
| [go-node-internals.md](./architecture/go-node-internals.md) | runtime、queue、Orchestrator、compression |
| [local-assistant.md](./architecture/local-assistant.md) | 本地助手联调（双 Client） |
| [agent-node-api.md](./architecture/agent-node-api.md) | HTTP/SSE API（`done` / HITL / usage） |
| [child-agent-tools.md](./architecture/child-agent-tools.md) | 临时子 Agent |
| [client-packaging.md](./architecture/client-packaging.md) | 同包 `config.yaml` 与安装布局 |
| [go-node-compatibility.md](./architecture/go-node-compatibility.md) | 静态构建 / glibc 矩阵 |
| [rhel6-acceptance-checklist.md](./architecture/rhel6-acceptance-checklist.md) | RHEL 6.9 验收 |

索引：[architecture/README.md](./architecture/README.md)

---

## 2. 设计（`design/`）

架构决策、实施计划、已落地优化的可读摘要与深度分析。

| 类别 | 入口 |
|------|------|
| **优化实录（首选）** | [major-changes.md](./design/major-changes.md) — 背景 / 思路 / 落地 |
| **精简专题** | [tool-context-cost-analysis.md](./design/tool-context-cost-analysis.md)（四段结构范本） |
| **深度分析** | [context-compression-cache-analysis.md](./design/context-compression-cache-analysis.md)、[skills-context-cost-analysis.md](./design/skills-context-cost-analysis.md)（搁置存档） |
| **ADR / 路线** | [three-component-model.md](./design/three-component-model.md)、[agent-client-refactor-plan.md](./design/agent-client-refactor-plan.md)、[manage-architecture.md](./design/manage-architecture.md) |
| **设计稿（未完全落地）** | [agent-hooks.md](./design/agent-hooks.md)、[ux-agent-owned-file-approval.md](./design/ux-agent-owned-file-approval.md) |

索引：[design/README.md](./design/README.md)

### 文档写法约定

| 类型 | 结构 | 存放 |
|------|------|------|
| **重大优化** | 背景与痛点 → 优化思路 → 落地方案 → 效果与局限 | `major-changes.md` 条目 + 可选精简专题 |
| **大型专题分析** | 背景与痛点 → 分析 → 优化思路 → 落地方案 | `design/*-analysis.md`（保持可扫读，细节不进实录重复堆叠） |
| **未落地方案** | 同上或完整设计稿 | `design/*.md`，文首标明 **设计稿 / 部分落地** |
| **模块 API** | `README.md` + `REFERENCE.md` | 与代码同目录 |

---

## 3. 专题参考（`docs/` 根）

| 文件 | 说明 |
|------|------|
| [manage-communication.md](./manage-communication.md) | Manage 与 Node/Client/Console 全量通信逻辑 |
| [built-in-tools-reference.md](./built-in-tools-reference.md) | Go 内置工具全量参考（description、参数、审批） |
| [built-in-tools.md](./built-in-tools.md) | §0 Go 工具索引；§1+ Python 归档对照 |
| [triggers-design.md](./triggers-design.md) | 触发器历史设计；**现网**见 `node/internal/triggers/` |
| [a2a-and-register-center.md](./a2a-and-register-center.md) | RC / `agent_peer` 历史；**现网 A2A** 见 Manage + `agent_invoke` |
| [security-rollout.md](./security-rollout.md) | 分阶段安全验收 |
| [roadmap.md](./roadmap.md) | 路线图 |
| [prometheus-metrics.md](./prometheus-metrics.md) | 指标（Python Agent 部分已移除） |
| [os-compatibility.md](./os-compatibility.md) | CPython 兼容（历史） |

---

## 4. 远期（`future/`）

尚未实现或需大幅修订的方案 → [future/README.md](./future/README.md)

---

## 5. 归档（`archive/`）

已移除的 Python Agent API、旧路由方案等 → [archive/README.md](./archive/README.md)

根目录 **`api-reference.md`**、**`agent-turn-loop.md`** 等为 **跳转桩**；正文在 `archive/python-agent-runtime/`。

---

## 6. 案例（`cases/`）

集成与验收案例 → [cases/README.md](./cases/README.md)

---

## 7. 仓库 README 索引

| 路径 | 说明 |
|------|------|
| [../README.md](../README.md) | 项目概览、快速开始 |
| [../node/README.md](../node/README.md) | Go Agent Node |
| [../client/README.md](../client/README.md) | Go Client |
| [../manage/README.md](../manage/README.md) | Manage 控制面 |
| [../app/cli/README.md](../app/cli/README.md) | Python Textual TUI |

`node/`、`client/`、`shared/config/` 等子目录维护 **`README.md`** / **`REFERENCE.md`**（与代码同步）。
