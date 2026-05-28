# Roadmap（路线图）

本文从 **产品/能力** 视角汇总 **DAgents 后端** 当前已落地能力与后续演进路线，重点对齐 **企业本地 Agent 控制台 + 可治理 Skill Library** 的产品定位。版本细节以 **[CHANGELOG.md](../CHANGELOG.md)** 为准，契约类说明见 **[docs/README.md](./README.md)**。**0.x** 阶段仍允许 HTTP/OpenAPI/配置形态调整；本文件会随里程碑持续更新。

---

## 1. 产品定位

DAgents 的主线不是成为通用可视化 AI 应用搭建平台，也不是复制 Dify / Flowise / Langflow，而是成为：

> 面向企业本地环境、开箱即用、可治理、可复用、可持续沉淀能力的内部 Agent 助手系统。

一句话目标：

> DAgents 是一个本地优先的企业 Agent 控制台。它可以发现内网中的可复用 Agent，通过审批机制治理高风险操作，并将重复工作沉淀为可复用的 skills 和 scripts。

后续所有优化优先围绕四个关键词展开：

- **本地**：内网部署、私有模型、内部 API、内部日志/监控/数据库接入。
- **治理**：权限策略、审批流、审计日志、风险分级、可回放。
- **复用**：Agent Directory、A2A、团队能力发现、跨 Agent 协作。
- **沉淀**：将一次性排障、脚本和经验转化为可审批、可版本化的 skill。

---

## 2. 整体阶段

| 阶段 | 状态 | 说明 |
|------|------|------|
| **0.1.0** | **已发布** | 首个对外标记版本：核心 API、可选 SQLite、SSE、指标、Register Center（内存）、文档与单测基线（见 **CHANGELOG**）。 |
| **0.x** | **进行中** | 以企业本地闭环为主线，在 **1.0** 前允许不兼容调整；重要变更写入 **CHANGELOG**。 |
| **1.0** | **目标** | 企业本地闭环、Agent Directory、治理审计、触发器控制面、Skill Library 的核心 API 与配置形态相对稳定。 |

---

## 3. 已实现能力（概要）

以下按子域归纳，**不**替代各 **`docs/*.md`** 与 **`app/**/README.md`** 的字段级说明。

### 3.1 Agent 服务与 HTTP

- **FastAPI**：会话创建/释放、消息提交（含 **`resume`**）、**SSE** 流、取消当前 turn、健康检查等（**`app/harness/api/`**）。
- **进程内编排**：按 **`session_id`** 的 **`MessageQueue`** 串行消费、**`AgentService`** 与 **`MainAgentTurnOrchestrator`**（见 [agent-turn-loop.md](./agent-turn-loop.md)、[agent-input-output.md](./agent-input-output.md)）。
- **可选自登记**：启动/关闭生命周期内向 **Register Center** 登记或注销（配置齐全时，**`app/harness/api/app.py`**）。

### 3.2 模型运行时与工具

- **OpenAI 兼容 Chat Completions**：流式、`tools`、**`AsyncOpenAI`**（**`app/core/main_agent/model.py`**）；支持 **`LLM_API_BASE`** 指向兼容网关。
- **隐式 ReAct 单轮运行时**：**`OpenAIImplicitReActRuntime.run_turn`**，工具执行与审批在编排层（见 [agent-turn-loop.md](./agent-turn-loop.md)）。
- **工具集**：**`bash_run`**、受限 **FS** 四件套、**`load_skills`**、**`agent_peer`**、异步工具托管等（**`app/harness/tools/`**）；**当前模型可见工具名与顺序**见 [built-in-tools.md](./built-in-tools.md)。
- **审批**：工具级策略 + **`resume_value`** 与 **`approval_required`** SSE。
- **异步工具**：后台协程 + 结果回灌 **`async_tool_result`**；**`client_id`** 与 **`sse_client_id`** 对齐（见 **CHANGELOG [Unreleased]**）。

### 3.3 上下文、压缩与提示词

- **双层上下文模型**与 **summary 压缩**（静默 / 阻塞阈值、**`ctx.messages`** 区间替换）（[context-compression-and-state.md](./context-compression-and-state.md)）。
- **可选 SQLite 会话持久化**（**`AGENT_SESSION_STORE_ENABLED`**；路径固定 **`.runtime/memory/session.sqlite3`**）。
- **系统提示词**：静态段 + **`.runtime/prompt_context/`** 侧车（**`soul.md` / `user.md` / `custom.md`**）+ skills + 主机快照 + **JSONL 历史说明**等（**CHANGELOG [Unreleased]**、**`prompt.py`**）。

### 3.4 可观测与分发侧车

- **Prometheus**：可选 **`/metrics`**（**`app/observability/`**）；已覆盖 LLM/session/queue/tool/summary 与 A2A/Register Center 操作指标。
- **Register Center（MVP）**：默认内存登记表，可选 **`REGISTER_CENTER_STORE_PATH`** 单文件 JSON 持久化；按 **`discovery_group`** 查询、**`/v1/broadcast`**、**`/v1/relay`**（**`register_center/`**）。
- **A2A**：**`agent_discover` / `agent_send_message` / `agent_broadcast` / `agent_peer_approve_tools`**，**`direct` / `relay`** 投递模式（[a2a-and-register-center.md](./a2a-and-register-center.md)）。

### 3.5 工程与文档

- **CI**：单测、PyInstaller 相关 workflow（见 **`.github/workflows/`**）。
- **技术文档**：架构、入出站、上下文、编排循环、A2A、API、指标等（**`docs/`**）。
- **落地案例目录**：**`docs/cases/`**（索引见 **`docs/cases/README.md`**）。

---

## 4. 企业本地闭环路线

后续优先级按“能否支撑企业本地可治理闭环”排序，而不是按通用 Agent 框架能力横向扩展。

### Phase 0：Enterprise Local Closed Loop（P0）

目标：企业用户能在 10 分钟内于内网机器完成一次闭环：

> 启动 DAgents → 发现 Agent → 提交运维问题 → 请求工具审批 → 执行诊断脚本 → 查看结果 → 查看审计记录。

优先交付：

- **一键本地启动**：后端、Register Center、UI、示例 Agent、示例 skill/script、示例审批流一起启动。
- **Docker Compose / 本地启动脚本**：默认 SQLite，可选 PostgreSQL；支持 OpenAI-compatible provider 与本地模型网关。
- **安装与 CLI 入口**：继续保留 Windows / Linux 安装包方向，安装后通过命令启动；CLI 作为本地调试、运维和审批入口。
- **首次启动诊断**：检查端口占用、模型配置、Register Center 可达性、UI API base、policy 文件与 runtime 目录。
- **强 demo**：围绕服务延迟诊断、带审批的服务重启、会话沉淀 skill 三个场景做端到端演示。

### Phase 1：Agent Directory / Register Center 企业化（P0/P1）

目标：将 Register Center 从技术组件升级为企业 Agent 目录。

优先交付：

- **Agent 身份与能力模型**：登记信息扩展为 `agent_id`、`name`、`description`、`owner`、`team`、`capabilities`、`tools`、`skills`、`endpoint`、`auth_method`、`status`、`last_seen`、`version`、`risk_level`、`allowed_scopes`。
- **健康状态与心跳**：在线/离线、最近心跳、版本、错误摘要、最近任务。
- **Agent Directory UI**：展示在线/离线 Agent、团队、能力标签、工具、skills、权限范围、健康状态、调用入口。
- **可信全局总览**：管理员视角支持不按 `discovery_group` 过滤的全局视图；必须配套鉴权、分页、审计和多租户边界。
- **A2A 可观测增强**：展示 direct/relay、target session、final state、trace id、调用耗时和失败原因。

### Phase 2：Governance, Approval & Audit（P0/P1）

目标：让工具执行、A2A 调用、触发器唤起和 skill 发布都可治理、可审批、可审计。

优先交付：

- **Policy Read API**：暴露工具策略、shell 策略、effective decision 与策略来源。
- **前端只读 Policy 页面**：先展示权限列表、审批模式、风险等级、策略来源、最近审批/拒绝记录；暂不开放编辑。
- **受保护 Policy 编辑**：在审计、角色和权限边界明确后，再支持 UI 修改策略。
- **审批记录模型**：记录谁发起、哪个 Agent 请求、工具名、参数、模型理由、风险判断、谁批准/拒绝、执行时间与结果。
- **Audit Timeline**：会话级串联 user request、Agent reasoning、A2A discover/call、approval、tool execution、skill creation。
- **Replay / Export**：支持导出审计记录，并为后续 replay 留出数据模型。

### Phase 3：Trigger Plane / 自主行动控制面（P0/P1）

触发器是模型能否“自主行动”的关键组件。它不只是定时任务，而是 DAgents 从被动聊天助手走向企业内部自治助手的控制面。

目标：在显式用户消息之外，允许系统按受治理的条件唤起 Agent 执行任务，并保证每次自主行动都可解释、可限流、可审批、可审计、可回放。

优先交付：

- **触发器类型**：
  - 时间 / 日历触发：cron、一次性计划任务、维护窗口。
  - Webhook / 队列触发：CI、告警平台、工单系统、内部事件总线。
  - 文件 / 配置变更触发：配置漂移、日志文件变化、目录事件。
  - 指标阈值触发：Prometheus 指标、健康检查、SLO/错误率阈值。
  - Register Center 事件触发：Agent 上线/下线、能力变更、broadcast/relay 过滤匹配。
- **触发器资源模型**：`trigger_id`、`name`、`description`、`owner`、`team`、`source_type`、`condition`、`target_agent_id`、`session_template`、`task_template`、`risk_level`、`enabled`、`last_fired_at`、`next_fire_at`、`cooldown`、`max_concurrency`、`approval_policy`。
- **幂等与去抖**：按事件 key、时间窗口、状态版本去重，避免告警风暴或重复执行。
- **并发与队列语义**：触发器最终应映射为向既有 `session_id` 投递消息，或模板化创建会话并投递首轮任务；必须尊重 `MessageQueue` 串行消费。
- **权限与审批**：中高风险触发器启用、修改、首次执行和高风险工具调用都应进入审批流。
- **审计与回放**：记录触发来源、匹配条件、触发 payload、创建的 session/request、执行结果、审批链路和失败原因。
- **失败处理**：重试、退避、死信、暂停触发器、失败通知和人工接管入口。
- **Trigger UI**：展示触发器列表、启用状态、最近触发、下次触发、失败率、关联 Agent、关联 skill、风险等级和审计入口。

边界：触发器不能绕过工具审批和 policy；不能默认拥有生产写权限；不能在没有审计记录的情况下执行高风险动作。

### Phase 4：Skill Library / Scripts 生命周期（P1）

目标：将一次性脚本和排障经验沉淀为可复用、可审批、可版本化的组织能力。

优先交付：

- **Skill 元数据模型**：`id`、`name`、`description`、`input_schema`、`output_schema`、`required_tools`、`required_permissions`、`risk_level`、`owner`、`team`、`version`、`status`、`script_ref`、`examples`、`success_count`、`failure_count`。
- **生命周期**：创建、测试、审批、发布、禁用、版本管理、回滚、使用统计、失败记录。
- **Skill Library UI**：搜索、团队过滤、风险过滤、参数说明、权限要求、版本、最近使用、一键调用。
- **会话沉淀 skill**：从一次排障会话生成 candidate skill，经测试和审批后发布。
- **触发器联动**：允许触发器调用已发布 skill，但必须继承 skill 的风险等级、权限要求和审批策略。

### Phase 5：Ops-oriented Workflows（P1/P2）

目标：优先围绕企业运维场景形成强 demo 和可复用模板，而不是泛化工作流编排平台。

优先交付：

- **多节点服务诊断**：发现 monitoring/log/database Agent，聚合指标、日志和数据库状态，生成诊断结论。
- **带审批的修复动作**：重启 staging 服务、执行诊断脚本、生成变更影响说明和审批请求。
- **Incident timeline**：owner、mitigation、root cause、logs/metrics/traces 关联、postmortem draft。
- **Runbook skill**：将 incident 处理步骤沉淀为可测试、可审批、可回滚的 skill。
- **触发器驱动运维**：由告警或指标阈值自动创建诊断会话，但修复动作仍需按风险进入审批。

### Phase 6：Enterprise Hardening（P2）

目标：在闭环验证后补齐企业生产落地能力。

优先交付：

- **企业身份集成**：先做 `admin`、`operator`、`developer`、`viewer`、`agent-service-account`，后续接 LDAP/OIDC/SSO/RBAC。
- **模型路由**：OpenAI-compatible、Ollama/vLLM/LM Studio/私有网关；按任务和数据敏感级别路由；记录 token 和成本。
- **安全沙箱**：script sandbox、工作目录隔离、超时、资源限制、网络限制、环境变量白名单、secret masking、dry-run、diff preview、command allowlist/denylist。
- **Register Center HA**：共享存储、鉴权、健康剔除、备份恢复、多副本部署。
- **HTTP API 覆盖**：补齐关键路由自动化测试，尤其是审批、审计、触发器、Agent Directory、Skill Library。

---

## 5. 待实现 / 演进中 / 已知限制

### 5.1 代码中已标 TODO 或占位

| 项 | 说明 | 参考 |
|----|------|------|
| **阻塞压缩失败的用户可见反馈** | 编排层在阻塞压缩失败时，**TODO**：返回更明确的错误语义（当前策略以代码为准）。 | **`app/core/main_agent/agent.py`** |
| **外部记忆文件接入** | **`read_memory_file_cached`** 现为占位实现（恒返回空串），尚未拼入 **`get_system_prompt`**；与 **「Agent 内置记忆书」** 可合并演进。 | **`app/core/main_agent/prompt.py`** |

### 5.2 架构级缺口（非小修）

| 项 | 说明 |
|----|------|
| **Register Center 高可用与共享存储** | 已有默认内存表 + 可选单文件 JSON 持久化，能覆盖单实例重启恢复；多副本仍无共享状态。后续应在 Agent Directory 阶段合并设计数据库/一致性存储、鉴权、健康剔除。 |
| **HTTP API 单测覆盖** | **0.1.0 已知限制**：未在仓库内对全部路由做自动化覆盖；后续应优先覆盖治理、审计、触发器和 Agent Directory。 |
| **多运行时抽象** | 主路径深度绑定 **OpenAI SDK 形态**；短期不作为主线，除非企业私有模型接入需要统一抽象。 |
| **触发器控制面尚未落地** | 目前仍以显式消息提交为主，尚无统一的触发器资源模型、调度器、幂等去抖、死信、触发审计和 UI。 |
| **Skill 生命周期尚未产品化** | 当前 skills 更接近工具/提示词扩展，尚未形成可审批、可版本化、可回滚、可统计的企业 Skill Library。 |

### 5.3 文档与生态

| 项 | 说明 |
|----|------|
| **`docs/cases/` 案例正文** | 目录与约定已就绪；后续案例应优先围绕企业本地闭环、触发器驱动诊断、审批修复和 skill 沉淀。 |
| **1.0 稳定性门槛** | 兼容性承诺、废弃策略、发布节奏待与 **CHANGELOG / Releases** 对齐后写清。 |

---

## 6. 降级或暂缓方向

为保持差异化，以下方向短期不作为主线：

- 通用可视化工作流画布。
- 类 Dify / Flowise 的应用发布平台。
- 插件市场。
- 过早的大规模多模型抽象。
- 过度聊天 UI 打磨。
- 与企业治理、复用、沉淀无关的泛化 Agent 框架能力。

这些能力不是永远不做，但必须服务于企业本地控制台、治理、触发器和 Skill Library 闭环。

---

## 7. 如何参与路线图讨论

- **缺陷与功能请求**：通过本仓库 **Issues**（建议注明版本与环境）。
- **安全**：见根目录 **[SECURITY.md](../SECURITY.md)**。
- **已交付事实**：以 **Git / 代码 / CHANGELOG** 为准；本文件若与实现冲突，**以实现为准** 并欢迎 PR 修正本文。

---

**最后更新**：路线图已调整为 **企业本地 Agent 控制台 + 可治理 Skill Library** 主线，并将 **触发器 / 自主行动控制面** 提升为 P0/P1 核心组件。若与代码冲突，以 **Git / CHANGELOG** 为准。
