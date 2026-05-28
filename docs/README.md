# 技术文档（`docs/`）

本目录存放 **DAgents 后端** 对外发布的技术说明。实现代码位于仓库根目录的 **`app/`** 与 **`register_center/`**。

**文件命名**：此处 Markdown 文件使用 **纯 ASCII 文件名**（例如 `api-reference.md`），便于跨平台与自动化工具引用。

## 索引

| 文件 | 说明 |
|------|------|
| [os-compatibility.md](./os-compatibility.md) | **操作系统 / glibc 兼容清单**：CPython **3.11** 官方对 Windows 的下界、Linux 为何不写「最低 RHEL」、PyInstaller 与 glibc 的关系；**不必固定 3.13** 时的落地建议。 |
| [architecture-v2/](./architecture-v2/) | **双运行时架构重构设计**：Agent = 大脑(Python) + 身体(Go Proxy)；终端/非终端 Agent 分类、Python-Go 功能划分、身份与会话模型、部署运维指南。 |
| [architecture-and-flows.md](./architecture-and-flows.md) | 架构分层、`session` / 队列 / 运行时概念，主流程与分支（工具、审批、异步工具、SSE、Register Center）。 |
| [agent-input-output.md](./agent-input-output.md) | **输入/输出专题**：HTTP 入队与进程内 **`MessageQueue`**（优先级、消费循环、背压）+ **SSE 出站**（总线、`client_id`、事件映射与联调要点）。 |
| [context-compression-and-state.md](./context-compression-and-state.md) | **`ConversationContext` / `OpenAIConversationContext`**、SQLite **会话记忆**、持久化边界、**summary 压缩**（静默/阻塞、`ctx.messages` 替换）、**`RunTurnPhase`**、**`.runtime/prompt_context` 侧车**（**`soul.md` / `user.md` / `custom.md`**）与 **`get_system_prompt`** 拼接顺序。 |
| [agent-turn-loop.md](./agent-turn-loop.md) | **Agent 循环流程**：队列外层与 **`run_turn`** 单轮内层、**`_run_turn_and_maybe_execute_tools`**、**`tool_result` 入队闭环**、审批与 **`async_tool_result`** 分支要点。 |
| [a2a-and-register-center.md](./a2a-and-register-center.md) | **A2A（`agent_peer` 工具）**与 **Register Center**：自登记、**`direct`/`relay`**、**`/v1/broadcast`** / **`/v1/relay`**、信封与审批 **`resume`** 约束。 |
| [built-in-tools.md](./built-in-tools.md) | **内置工具**：**`get_tools()`** 列表、**`@tool` docstring → LLM `description`**、**Schema/参数管道**、异步与 **`FS_ROOT`** / A2A 依赖、未注册的 **`host_platform`**。 |
| [api-reference.md](./api-reference.md) | HTTP / SSE：路径、请求与响应体、`client_id`、错误约定；与 `app/harness/api/app.py` 对齐维护。 |
| [prometheus-metrics.md](./prometheus-metrics.md) | Prometheus **`/metrics`** 行为、内置指标与安全扩展方式。 |
| [security-rollout.md](./security-rollout.md) | 安全治理、审计指标、长任务/A2A/tools/prompt 变更的分阶段验收与回滚方式。 |
| [roadmap.md](./roadmap.md) | **路线图**：已实现能力汇总、待办与已知限制（与 **CHANGELOG** 互补）。 |
| [triggers-design.md](./triggers-design.md) | **触发器设计**：**调度器轮询**、`Trigger` **基类协议**、**注册表**、**投递层**；**动态注册**（HTTP / `@tool`）；**`session_id` / `client_id` / SSE 回流`** 约定与分阶段（Webhook / A2A）。 |

## 落地案例目录

场景化实践、效果与踩坑记录见 [cases/README.md](./cases/README.md)（单篇案例 Markdown 放在 **`cases/`** 下，命名约定见该说明）。

## 仓库内其它说明

| 位置 | 说明 |
|------|------|
| [../README.md](../README.md) | 项目概览、安装运行、安全与版本 |
| [../CHANGELOG.md](../CHANGELOG.md) | 版本变更记录 |
| [../SECURITY.md](../SECURITY.md) | 漏洞报告方式 |
| [../register_center/README.md](../register_center/README.md) | Register Center 的 HTTP API 与用法 |

**`app/`** 下各子包另维护 **`README.md`** / **`REFERENCE.md`**（含目录索引与符号说明；增删源码时同步更新）。
