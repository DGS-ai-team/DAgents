# 技术文档（`docs/`）

本目录存放 DAgents **双运行时栈**的技术说明。实现代码：

| 栈 | 路径 |
|----|------|
| **Go 本地助手** | `node/`、`client/`、`shared/config/` |
| **Python API / A2A** | `app/`、`register_center/` |

**文件命名**：Markdown 使用 **纯 ASCII 文件名**。

## 架构（先读）

| 文件 | 说明 |
|------|------|
| [architecture/overview.md](./architecture/overview.md) | **双栈选型**：何时用 Go Node vs Python API |
| [architecture/local-assistant.md](./architecture/local-assistant.md) | 本地助手：Go Node + Textual / Go REPL 联调 |
| [architecture/agent-node-api.md](./architecture/agent-node-api.md) | Agent Node HTTP/SSE API 草图 |
| [architecture/child-agent-tools.md](./architecture/child-agent-tools.md) | 临时子 Agent 工具 / HTTP / SSE 定稿（Go Node） |
| [architecture/client-packaging.md](./architecture/client-packaging.md) | 同包 `config.yaml` 与安装布局 |
| [architecture/go-node-compatibility.md](./architecture/go-node-compatibility.md) | Go 静态构建 / glibc 矩阵（N7） |
| [architecture/rhel6-acceptance-checklist.md](./architecture/rhel6-acceptance-checklist.md) | RHEL 6.9 真机验收清单 |
| [architecture/python-runtime.md](./architecture/python-runtime.md) | Python FastAPI 分层与主流程 |

## 设计与实施

| 文件 | 说明 |
|------|------|
| [design/background-and-motivation.md](./design/background-and-motivation.md) | 老旧 OS 动机与 Go 方案 |
| [design/three-component-model.md](./design/three-component-model.md) | Node + Client + Manage 三组件 ADR |
| [design/agent-client-refactor-plan.md](./design/agent-client-refactor-plan.md) | AC 分步计划与 N0–N7 状态 |

## 专题（按栈标注）

| 文件 | 适用栈 | 说明 |
|------|--------|------|
| [api-reference.md](./api-reference.md) | Python **（已弃用 Agent 运行时）** | HTTP / SSE 契约（`app/harness/api`） |
| [agent-input-output.md](./agent-input-output.md) | Python **（已弃用）** | 入队、`MessageQueue`、`connection_id` SSE |
| [agent-turn-loop.md](./agent-turn-loop.md) | Python **（已弃用）** | `run_turn`、工具闭环 |
| [context-compression-and-state.md](./context-compression-and-state.md) | 双栈 | 压缩与 prompt 侧车（Go/Python 均有实现） |
| [a2a-and-register-center.md](./a2a-and-register-center.md) | Python | `agent_peer`、Register Center |
| [built-in-tools.md](./built-in-tools.md) | 以 Python 为主 | 工具清单；Go 差异见各 `node/internal/tools/README.md` |
| [triggers-design.md](./triggers-design.md) | 长期设计 | Go 落地见 [`node/internal/triggers/README.md`](../node/internal/triggers/README.md) |
| [prometheus-metrics.md](./prometheus-metrics.md) | Python | `/metrics` |
| [security-rollout.md](./security-rollout.md) | 通用 | 分阶段安全验收 |
| [os-compatibility.md](./os-compatibility.md) | Python 发布 | glibc / Windows 兼容 |
| [roadmap.md](./roadmap.md) | 通用 | 路线图 |

## 远期规划（`future/`）

Manage、A2A inbox、多租户等 **尚未实现** 的方案，见 [future/README.md](./future/README.md)。

## 归档（`archive/`）

已废弃的 Proxy/Body 路由等，见 [archive/README.md](./archive/README.md)。

## 落地案例

[cases/README.md](./cases/README.md)

## 仓库内 README 索引

| 路径 | 说明 |
|------|------|
| [../README.md](../README.md) | 项目概览、双栈快速开始 |
| [../node/README.md](../node/README.md) | Go Agent Node |
| [../client/README.md](../client/README.md) | Go REPL Client |
| [../app/README.md](../app/README.md) | Python 包（含 `cli/` TUI） |
| [../register_center/README.md](../register_center/README.md) | Register Center |

`app/`、`node/` 各子目录维护 **`README.md`** / **`REFERENCE.md`**。
