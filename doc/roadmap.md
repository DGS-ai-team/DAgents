# Roadmap（路线图）

本文从 **产品/能力** 视角汇总 **DAgents 后端** 当前已落地能力与 **已知缺口或规划方向**，与版本细节 **[CHANGELOG.md](../CHANGELOG.md)**、契约类 **[doc/README.md](./README.md)** 互补。**0.x** 阶段仍以代码与变更记录为准；本文件会随里程碑调整。

---

## 1. 整体阶段

| 阶段 | 状态 | 说明 |
|------|------|------|
| **0.1.0** | **已发布** | 首个对外标记版本：核心 API、可选 SQLite、SSE、指标、Register Center（内存）、文档与单测基线（见 **CHANGELOG**）。 |
| **0.x** | **进行中** | 在 **1.0** 前允许不兼容的 HTTP/OpenAPI/配置调整；重要变更写入 **CHANGELOG**。 |
| **1.0** | **目标** | API 与配置形态相对稳定、破坏性变更受控；具体门槛待社区与维护者共同收敛。 |

---

## 2. 已实现能力（概要）

以下按子域归纳，**不**替代各 **`doc/*.md`** 与 **`app/**/README.md`** 的字段级说明。

### 2.1 Agent 服务与 HTTP

- **FastAPI**：会话创建/释放、消息提交（含 **`resume`**）、**SSE** 流、取消当前 turn、健康检查等（**`app/harness/api/`**）。
- **进程内编排**：按 **`session_id`** 的 **`MessageQueue`** 串行消费、**`AgentService`** 与 **`MainAgentTurnOrchestrator`**（见 [agent-turn-loop.md](./agent-turn-loop.md)、[agent-input-output.md](./agent-input-output.md)）。
- **可选自登记**：启动/关闭生命周期内向 **Register Center** 登记或注销（配置齐全时，**`app/harness/api/app.py`**）。

### 2.2 模型运行时与工具

- **OpenAI 兼容 Chat Completions**：流式、`tools`、**`AsyncOpenAI`**（**`app/core/main_agent/model.py`**）；支持 **`LLM_API_BASE`** 指向兼容网关。
- **隐式 ReAct 单轮运行时**：**`OpenAIImplicitReActRuntime.run_turn`**，工具执行与审批在编排层（见 [agent-turn-loop.md](./agent-turn-loop.md)）。
- **工具集**：**`bash_run`**、受限 **FS** 四件套、**`load_skills`**、**`agent_peer`**、异步工具托管等（**`app/harness/tools/`**）；**当前模型可见工具名与顺序**见 [built-in-tools.md](./built-in-tools.md)。
- **审批**：工具级策略 + **`resume_value`** 与 **`approval_required`** SSE。
- **异步工具**：后台协程 + 结果回灌 **`async_tool_result`**；**`client_id`** 与 **`sse_client_id`** 对齐（见 **CHANGELOG [Unreleased]**）。

### 2.3 上下文、压缩与提示词

- **双层上下文模型**与 **summary 压缩**（静默 / 阻塞阈值、**`ctx.messages`** 区间替换）（[context-compression-and-state.md](./context-compression-and-state.md)）。
- **可选 SQLite 会话持久化**（**`AGENT_SESSION_STORE_ENABLED`**；路径固定 **`.runtime/memory/session.sqlite3`**）。
- **系统提示词**：静态段 + **`.runtime/prompt_context/`** 侧车（**`soul.md` / `user.md` / `custom.md`**）+ skills + 主机快照 + **JSONL 历史说明**等（**CHANGELOG [Unreleased]**、**`prompt.py`**）。

### 2.4 可观测与分发侧车

- **Prometheus**：可选 **`/metrics`**（**`app/observability/`**）。
- **Register Center（MVP）**：内存登记表、按 **`discovery_group`** 查询、**`/v1/broadcast`**、**`/v1/relay`**（**`register_center/`**）。
- **A2A**：**`agent_discover` / `agent_send_message` / `agent_broadcast` / `agent_peer_approve_tools`**，**`direct` / `relay`** 投递模式（[a2a-and-register-center.md](./a2a-and-register-center.md)）。

### 2.5 工程与文档

- **CI**：单测、PyInstaller 相关 workflow（见 **`.github/workflows/`**）。
- **技术文档**：架构、入出站、上下文、编排循环、A2A、API、指标等（**`doc/`**）。
- **落地案例目录**：**`doc/cases/`**（索引见 **`doc/cases/README.md`**）。

---

## 3. 待实现 / 演进中 / 已知限制

### 3.1 代码中已标 TODO 或占位

| 项 | 说明 | 参考 |
|----|------|------|
| **阻塞压缩失败的用户可见反馈** | 编排层在阻塞压缩失败时，**TODO**：返回更明确的错误语义（当前策略以代码为准）。 | **`app/core/main_agent/agent.py`** |
| **外部记忆文件接入** | **`read_memory_file_cached`** 现为占位实现（恒返回空串），尚未拼入 **`get_system_prompt`**；与下文 **「Agent 内置记忆书」** 可合并演进。 | **`app/core/main_agent/prompt.py`** |

### 3.2 架构级缺口（非小修）

| 项 | 说明 |
|----|------|
| **Register Center 持久化与高可用** | 当前为 **单进程内存表**；重启丢失；多副本无共享状态。演进方向可包括持久存储、鉴权、健康剔除等（需单独设计）。 |
| **HTTP API 单测覆盖** | **0.1.0 已知限制**：未在仓库内对全部路由做自动化覆盖；CI 以默认 **`test_*.py`** 为主。 |
| **多运行时抽象** | 主路径深度绑定 **OpenAI SDK 形态**；若需统一抽象多厂商协议，需在 **`model` / `runtime`** 层演进。 |

### 3.3 文档与生态

| 项 | 说明 |
|----|------|
| **`doc/cases/` 案例正文** | 目录与约定已就绪；**具体场景案例**依赖贡献者后续补充。 |
| **1.0 稳定性门槛** | 兼容性承诺、废弃策略、发布节奏待与 **CHANGELOG / Releases** 对齐后写清。 |

### 3.4 规划中的能力（路线图优先方向）

以下条目为 **尚未在代码中完整交付** 的产品方向，实施顺序与拆分里程碑需在 **Issues / 设计文档** 中另行约定；与 §3.1～§3.3 可能重叠处会合并收敛。

| 方向 | 预期价值与边界（概要） |
|------|------------------------|
| **CLI** | 在 **HTTP API** 之外提供终端侧会话、调试与自动化入口（与配置项 **`AGENT_CLI_MODE`** 等长期对齐）；降低本地联调与脚本化运维成本。 |
| **触发器（条件唤起）** | 在「显式 **`POST` 消息 / SSE 拉流**」之外，提供 **可声明的触发面**：按 **时间或日历**、**Webhook / 队列消息到达**、**文件或配置变更**、**Prometheus 指标阈值**、**Register Center 广播或 relay 的过滤匹配** 等条件 **唤起** Agent 执行（可映射为向既有 **`session_id`** 投递系统/用户消息，或 **模板化创建会话 + 首轮任务**）。需单独设计：**条件 DSL 或插件边界**、**幂等与去抖**、**并发与 `MessageQueue` 串行语义**、**鉴权/租户隔离/审计**、失败 **重试与死信**、与 **审批流** 的交互，避免与现有 **A2A** 语义重复或竞态。 |
| **子 Agent 创建** | 由主 Agent 或编排层 **动态创建/挂载** 子会话或子运行时实例（与当前「单服务多 `session`」模型如何衔接需单独设计）；可能涉及资源配额、生命周期与观测。 |
| **本地与远端 A2A 优化** | 在现有 **`agent_peer` + `direct`/`relay`**（见 [a2a-and-register-center.md](./a2a-and-register-center.md)）之上：超时与重试策略、连接池、大 payload、**`resume` 经 relay 的可行性**、广播扇出与 SSE 聚合性能、跨 NAT 拓扑下的可达性检测等。 |
| **Register Center 全局总览** | 在 **不按 `discovery_group` 过滤** 的前提下，为 **可信管理员或控制面** 提供全局视图（当前 API **有意** 不提供全量列举以防未授权探测）；需配套 **鉴权、审计、分页与多租户** 设计。 |
| **Context 压缩优化** | 在现有 **静默 / 阻塞 + summary 替换**（见 [context-compression-and-state.md](./context-compression-and-state.md)）之上：压缩质量（保留工具边界/结构化信息）、触发策略（多信号而不仅是 token 计数）、失败降级与 **§3.1** 用户可见错误等。 |
| **Agent 内置记忆书** | 结构化、可检索的 **长期会话外记忆**（「记忆书」）：与 **SQLite 会话态**、**JSONL 原始消息**、**`custom.md` 侧车** 分工明确；可能包含章节、标签、显式写入 API 及与 **`get_system_prompt`** / 工具联动的读取策略。 |

---

## 4. 如何参与路线图讨论

- **缺陷与功能请求**：通过本仓库 **Issues**（建议注明版本与环境）。  
- **安全**：见根目录 **[SECURITY.md](../SECURITY.md)**。  
- **已交付事实**：以 **Git / 代码 / CHANGELOG** 为准；本文件若与实现冲突，**以实现为准** 并欢迎 PR 修正本文。

---

**最后更新**：已含 **§3.4 规划能力**（CLI、**触发器（条件唤起）**、子 Agent、A2A 与 Register Center 增强、压缩与记忆书等方向）；与 **0.1.0 + [Unreleased]** 已交付事实并列维护，若与代码冲突以 **Git / CHANGELOG** 为准。
