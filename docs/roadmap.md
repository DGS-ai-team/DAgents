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
| **0.2.3** | **已发布** | Windows 安装包、Linux `install.sh`；`tools.bash_output_encoding`（GBK 解码）；子 Agent prompt 与双 TUI 展示（见 **CHANGELOG**）。 |
| **0.2.2** | **已发布** | HITL 工具审批修复；临时 Agent 协议整理；文档归档与 go-node-internals（见 **CHANGELOG**）。 |
| **0.2.0** | **已发布** | Go Agent Node + Client 本地助手主线；Python Agent 运行时移除；触发器日历调度（见 **CHANGELOG**）。 |
| **0.1.0** | **已发布** | 首个对外标记版本：Python 核心 API、SQLite、SSE、Register Center、文档与单测基线。 |
| **0.x** | **进行中** | 以 Go 本地助手 + 企业治理为主线；**1.0** 前允许不兼容调整。 |
| **1.0** | **目标** | 企业本地闭环、Agent Directory、治理审计、触发器控制面、Skill Library 的核心 API 与配置形态相对稳定。 |

---

## 3. 已实现能力（概要）

以下按子域归纳现行 **Go 本地助手栈**；字段级契约见 [architecture/overview.md](./architecture/overview.md)、[architecture/agent-node-api.md](./architecture/agent-node-api.md) 与各包 **`README.md`**。已移除的 **Python FastAPI Agent API** 行为见 [archive/python-agent-runtime/](./archive/python-agent-runtime/)。

### 3.1 Agent Node 与 HTTP/SSE

- **Go Agent Node**（**`node/`**）：**`GET /health`**、**`GET /v1/agent/info`**、会话创建/释放、消息提交（含 **`resume`**）、**SSE** 流、取消当前 turn、context/skills/child-agents 等（[agent-node-api.md](./architecture/agent-node-api.md)）。
- **进程内编排**：按 **`session_id`** 的 **`MessageQueue`** 串行消费、**`Orchestrator`** turn loop（[go-node-internals.md](./architecture/go-node-internals.md)）。
- **Client**：Go bubbletea TUI（**`dagents tui`**）+ REPL 兜底（**`--plain`**）；Python Textual（**`dagents chat`**）；均连本地 Node（[local-assistant.md](./architecture/local-assistant.md)）。
- **同包配置**：**`packaging/agent-client/config.yaml`**（**`agent_id` / `listen` / `llm` / `data_dir` / `tools.*`**）；Node 与双 Client 共用（[client-packaging.md](./architecture/client-packaging.md)）。

### 3.2 模型运行时与工具

- **OpenAI 兼容 Chat Completions**：流式、`tools`（**`node/internal/llm/`**）；**`llm.api_base`** 指向兼容网关。
- **Turn loop**：工具调用 → 本地 **`Execute`** → **`tool_result`** → 继续 loop；工具级审批与 **`ask_user_information`**（[go-node-internals.md](./architecture/go-node-internals.md)）。
- **工具集**（**`node/internal/tools/`**）：**`bash_run`**（bash/cmd/powershell、GBK/UTF-8 输出解码）、受限 **FS** 四件套、**`load_skills` / `unload_skills` / `clear_skills`**、**trigger_*** 系列、**临时子 Agent**（**`create_temporary_agent`** 等，非 A2A）；**`run_in_background`** 与后台 job 工具。清单见 [built-in-tools.md](./built-in-tools.md) 与 **`node/internal/tools/README.md`**。
- **审批（HITL）**：**`.runtime/policy/`** + **`approval_required`** SSE + Client **`resume`**（0.2.2 起修复父子 Agent 并发审批队列）。

### 3.3 上下文、压缩与提示词

- **Summary 压缩**：静默 / 阻塞阈值、SSE 压缩事件（[context-compression-and-state.md](./context-compression-and-state.md)）。
- **SQLite 会话持久化**：**`data_dir/sessions.db`**；原始消息 **JSONL** 审计（**`node/internal/store/`**、**`node/internal/history/`**）。
- **系统提示词**：静态段 + **`.runtime/prompt_context/`** 侧车 + skills 正文 + 主机快照；子 Agent 独立 **`BuildChildSystemPrompt`**。

### 3.4 触发器、子 Agent 与 Register Center

- **触发器（Go Node）**：**`interval` / `fire_at` / 日历 `schedule`**（含 **`cmd` 门控**）；**`trigger_*`** 工具 + 调度器（**`node/internal/triggers/`**；设计见 [triggers-design.md](./triggers-design.md)）。
- **临时子 Agent**：同进程 **`create_temporary_agent` → `wait_temporary_agents`**；非跨进程 A2A（[child-agent-tools.md](./architecture/child-agent-tools.md)）。
- **Register Center（独立 Python 服务）**：登记、**`/v1/broadcast`**、**`/v1/relay`**、可选 JSON 持久化（**`register_center/`**）。**Go Node 不直连 RC、不自登记**；Agent 侧 **`agent_peer`** 工具已随 Python API 移除，端到端 A2A 待 **Manage** 阶段（[future/a2a-via-manage.md](./future/a2a-via-manage.md)）。

### 3.5 打包、工程与文档

- **分发**：GitHub Releases **`dagents-local-assistant-*`**（linux tarball / windows zip）；**v0.2.3** 起 **Windows Inno 安装包**、**Linux `install.sh`**（[client-packaging.md](./architecture/client-packaging.md)）。
- **CI**：**`go-ac.yml`**（Go 单测与交叉编译）、**`manual-package`**（Release 组装）；Register Center / Python CLI 单测保留。
- **技术文档**：架构总览、Node 内部、AC 计划、归档 Python 运行时等（**`docs/`**）。
- **落地案例目录**：**[`cases/`](../../cases/)**（索引见 **[`cases/README.md`](../../cases/README.md)**；每案含 Docker 复现环境）。

---

## 4. 企业本地闭环路线

后续优先级按“能否支撑企业本地可治理闭环”排序，而不是按通用 Agent 框架能力横向扩展。

### Phase 0：Enterprise Local Closed Loop（P0）

目标：企业用户能在 10 分钟内于内网机器完成一次闭环：

> 启动 DAgents（Node + Client）→ 提交运维问题 → 请求工具审批 → 执行诊断脚本 → 查看结果 → 查看 JSONL 审计。

**跨 Agent 发现/协作**（Register Center 目录、A2A 投递）在 **Manage 阶段**与 Agent Directory UI 一并交付；Phase 0 以 **单 Node 本地助手 + 可选 RC 独立启动** 为主。

优先交付：

- **一键本地启动**：Node、Client、示例 skill/script、示例审批流；Register Center 作为可选侧车（**`dagents register-center`** / 安装包入口）。
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

### 5.1 代码与验收缺口

| 项 | 说明 | 参考 |
|----|------|------|
| **RHEL 6 / Win2012 真机验收** | 静态构建与 SysV init 脚本已就绪；**真机 E2E 记录**仍 open（N7）。 | [rhel6-acceptance-checklist.md](./architecture/rhel6-acceptance-checklist.md)、[agent-client-refactor-plan.md](./design/agent-client-refactor-plan.md) |
| **长期记忆文件** | **`.runtime/memory/long_term.md`** 在 prompt 中有说明位，产品化读写与治理待 Phase 2+。 | **`node/internal/turn/prompt.go`** |
| **Manage / A2A 控制面** | Node 侧 **`manage.enabled: false`**；无 **`agent_peer`** 工具；RC relay/broadcast 需 Manage 或新 Agent 登记端才能形成闭环。 | [future/a2a-via-manage.md](./future/a2a-via-manage.md) |

### 5.2 架构级缺口（非小修）

| 项 | 说明 |
|----|------|
| **Register Center 高可用与共享存储** | 已有默认内存表 + 可选单文件 JSON 持久化，能覆盖单实例重启恢复；多副本仍无共享状态。后续应在 Agent Directory 阶段合并设计数据库/一致性存储、鉴权、健康剔除。 |
| **HTTP API 单测覆盖** | **0.x 已知限制**：Go Node 与 Register Center 均未全路由自动化覆盖；后续应优先覆盖治理、审计、触发器与 Agent Directory。 |
| **多运行时抽象** | 主路径深度绑定 **OpenAI 兼容形态**；短期不作为主线，除非企业私有模型接入需要统一抽象。 |
| **触发器企业控制面** | Go Node 已具备 **trigger 资源模型 + 调度器 + 工具**；尚缺 Webhook/指标阈值等类型、幂等去抖、死信、触发审计 UI 与跨租户治理。 |
| **Skill 生命周期尚未产品化** | 当前 skills 更接近工具/提示词扩展，尚未形成可审批、可版本化、可回滚、可统计的企业 Skill Library。 |

### 5.3 文档与生态

| 项 | 说明 |
|----|------|
| **[`cases/`](../../cases/) 案例** | 含 **`centos7-feature-tour`**（CentOS 7 + HTTP 特性导览）；后续可扩展 HITL、triggers 等专题 case。 |
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

**最后更新**：2026-06-08 — 对齐 **v0.2.3**（Go Node 本地助手、Windows/Linux 安装包、RC 独立服务）；**Agent 侧 A2A 工具已移除**，跨 Agent 协作列入 **Manage** 远期。若与代码冲突，以 **Git / CHANGELOG** 为准。
