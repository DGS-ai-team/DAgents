# 技术文档（`docs/`）

本目录存放 DAgents 技术说明。实现代码：

| 栈 | 路径 |
|----|------|
| **Go 本地助手（Agent 运行时）** | `node/`、`client/`、`shared/config/` |
| **Python 辅助** | `app/cli/`（Textual TUI）、`register_center/`（A2A 控制面） |

**文件命名**：Markdown 使用 **纯 ASCII 文件名**。

## 架构（先读）

| 文件 | 说明 |
|------|------|
| [architecture/overview.md](./architecture/overview.md) | **选型总览**：Go Node vs Register Center |
| [architecture/go-node-internals.md](./architecture/go-node-internals.md) | **Go Node 内部结构**：runtime、Orchestrator、MessageQueue 及协作关系 |
| [architecture/local-assistant.md](./architecture/local-assistant.md) | 本地助手：Go Node + Textual / Go REPL 联调 |
| [architecture/agent-node-api.md](./architecture/agent-node-api.md) | Agent Node HTTP/SSE API（`done`：`turn_complete` / `awaiting`） |
| [architecture/child-agent-tools.md](./architecture/child-agent-tools.md) | 临时子 Agent 工具 / HTTP / SSE 定稿 |
| [architecture/client-packaging.md](./architecture/client-packaging.md) | 同包 `config.yaml` 与安装布局 |
| [architecture/go-node-compatibility.md](./architecture/go-node-compatibility.md) | Go 静态构建 / glibc 矩阵（N7） |
| [architecture/rhel6-acceptance-checklist.md](./architecture/rhel6-acceptance-checklist.md) | RHEL 6.9 真机验收清单 |

## 设计与实施

| 文件 | 说明 |
|------|------|
| [design/background-and-motivation.md](./design/background-and-motivation.md) | 老旧 OS 动机与 Go 方案 |
| [design/three-component-model.md](./design/three-component-model.md) | Node + Client + Manage 三组件 ADR |
| [design/agent-client-refactor-plan.md](./design/agent-client-refactor-plan.md) | AC 分步计划与 N0–N7 状态 |
| [design/agent-directory-phase1.md](./design/agent-directory-phase1.md) | Phase 1 RC 企业化（过渡；见 Manage 方案） |
| [design/manage-architecture.md](./design/manage-architecture.md) | Manage 统一控制面架构方案 |
| [manage/README.md](../../manage/README.md) | **Manage 服务**（M0+M1 已落地） |

## 专题

| 文件 | 适用栈 | 说明 |
|------|--------|------|
| [context-compression-and-state.md](./context-compression-and-state.md) | Go Node | 压缩与 prompt 侧车 |
| [a2a-and-register-center.md](./a2a-and-register-center.md) | Register Center | RC HTTP；历史 **`agent_peer`** 见文档内「历史」节 |
| [built-in-tools.md](./built-in-tools.md) | Go Node + 归档 | §0 现行工具表；Python 见归档 |
| [triggers-design.md](./triggers-design.md) | Go Node | 长期设计；落地见 `node/internal/triggers/README.md` |
| [prometheus-metrics.md](./prometheus-metrics.md) | 历史 + RC | Python Agent `/metrics` 已移除；Register Center 指标仍适用 |
| [security-rollout.md](./security-rollout.md) | 通用 | 分阶段安全验收 |
| [os-compatibility.md](./os-compatibility.md) | 历史参考 | CPython 兼容（Python Agent 已移除） |
| [roadmap.md](./roadmap.md) | 通用 | 路线图 |

## 远期规划（`future/`）

Manage、A2A inbox、多租户等 **尚未实现** 的方案，见 [future/README.md](./future/README.md)。

## 归档（`archive/`）

| 目录 | 说明 |
|------|------|
| [archive/python-agent-runtime/](./archive/python-agent-runtime/) | **已移除的 Python FastAPI Agent API**（`api-reference`、turn loop 等） |
| [archive/README.md](./archive/README.md) | Proxy/Body 路由等更早方案 |

根目录 `api-reference.md`、`agent-input-output.md`、`agent-turn-loop.md`、`architecture-and-flows.md` 仅为**兼容跳转桩**，正文在 `archive/python-agent-runtime/`。

## 落地案例

[cases/README.md](./cases/README.md)

## 仓库内 README 索引

| 路径 | 说明 |
|------|------|
| [../README.md](../README.md) | 项目概览、快速开始 |
| [../node/README.md](../node/README.md) | Go Agent Node |
| [../client/README.md](../client/README.md) | Go REPL Client |
| [../app/README.md](../app/README.md) | Python 包（含 `cli/` TUI） |
| [../register_center/README.md](../register_center/README.md) | Register Center |

`node/`、`app/` 各子目录维护 **`README.md`** / **`REFERENCE.md`**。
