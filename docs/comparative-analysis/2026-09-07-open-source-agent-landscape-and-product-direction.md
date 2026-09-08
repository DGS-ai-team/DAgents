# 开源 Agent 项目格局与 DAgents 产品演进方向

> **检查日期**：2026-09-07
> **文档性质**：外部项目研究与产品方向建议，不定义 DAgents 当前 API 或已实现行为
> **DAgents 基线**：v0.10.7；实现事实以代码、测试、[`../architecture.md`](../architecture.md) 和 [`../roadmap.md`](../roadmap.md) 为准
> **外部事实时效性**：同类项目变化较快，采用本文件结论前应重新检查其官方仓库和文档

## 1. 执行摘要

DAgents 已经拥有本地 Agent、工具、审批、终端、MCP、Skills、Memory、Trigger、浏览器、Computer Use、子 Agent、Workgroup、Manage 和桌面 Shell，功能宽度足以形成完整产品。下一阶段的主要矛盾不再是“是否缺少某个工具”，而是：

1. 产品定位是否足够清晰；
2. 执行、取消、恢复和跨组件状态是否可靠；
3. 安全边界是否配得上企业内网定位；
4. Workgroup 是否能从技术能力转化为用户可理解的协作产品；
5. 开源生态、文档、分发和信任体系是否成熟。

本报告的核心建议是将 DAgents 定位从宽泛的“本地优先通用 Agent 工作台”进一步收敛为：

> **面向 Windows/Linux 内网与异构终端的、可审计、可接管、可恢复的 Agent 执行网络。**

可用更短的产品概念表达为“受控 Agent Fabric”。它不是另一个 coding agent、聊天渠道网关或工作流画布，而是让多个 Agent 在组织自己的机器上安全执行并协作的运行与治理层。

## 2. 对标范围与比较方法

### 2.1 为什么不能只对标一个项目

DAgents 同时覆盖 Agent Runtime、本地工作台、执行环境、记忆、扩展和多机控制面，没有单一开源项目与其边界完全相同。因此采用分层对标：

| DAgents 层次 | 主要对标对象 | 借鉴重点 |
|---|---|---|
| Node Agent Runtime | OpenAI Codex、DeepSeek Harness | Turn/Step/Item、事件、工具、审批、取消、上下文 |
| 本地执行与 Workspace | OpenHands、Codex | Local/Container/Remote 执行、隔离、进程生命周期 |
| 桌面与通用 Agent 体验 | Goose、OpenClaw | 安装、onboarding、扩展、诊断、常驻运行 |
| Memory | Letta、OpenClaw | 分层记忆、可检查性、版本、整理和召回 |
| 开发者交互 | Cline | checkpoint、撤销、Plan/Act、任务和工具可视化 |
| Manage / Workgroup | DAgents 自有方向 | 跨机器 AgentRef、出站连接、治理、审计和可靠协作 |

Roo Code 的官方仓库已于 2026-05-15 归档，不再作为长期演进主基线；其模式化交互仍可作为历史设计参考。

### 2.2 判断标准

本报告从以下维度比较：

- 用户第一心智与目标人群；
- Agent loop 和状态真相来源；
- 本地、容器和远程执行边界；
- 权限、审批与沙箱；
- Memory 与上下文管理；
- 多 Agent 与跨机器协作；
- MCP、Skills、插件和 SDK；
- 桌面体验、安装、升级和诊断；
- 开源社区、文档、案例和产品可信度。

## 3. 同类开源项目格局

### 3.1 OpenAI Codex：可靠的 coding-agent 协议和执行环境

Codex 的核心优势不是工具数量，而是把一次任务拆成稳定的 Thread、Turn 和 Item 生命周期。客户端通过服务端事件展示 `turn/started`、`item/started`、增量、`item/completed` 和 `turn/completed`；审批由服务端请求客户端决议，最终 Item 是权威结果。它还把 workspace、网络、命令和权限配置放在明确的 sandbox/permission 模型中。

DAgents 最应借鉴：

- UI 展示单元拥有独立、稳定、可持久化的身份；
- `started → delta → completed` 是统一生命周期，不依赖 UI 猜测；
- Turn 中断与单个 Item 取消语义分离；
- 审批请求绑定 thread、turn、item 和执行参数；
- 分页历史、恢复和实时事件分层；
- 沙箱权限与审批相互配合，而不是以审批替代隔离。

不应直接追逐：

- 与 OpenAI 模型深度绑定的私有能力；
- 以软件工程为唯一场景的交互设计；
- 为追求协议一致而照搬其内部命名。

官方来源：[Codex 仓库](https://github.com/openai/codex)、[App Server 协议](https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md)、[Codex Rust README](https://github.com/openai/codex/blob/main/codex-rs/README.md)。

### 3.2 OpenHands：可替换 Workspace 与生产隔离

OpenHands 将核心 SDK、工具、Workspace 和 Agent Server 分层。同一 Agent 代码可以使用 LocalWorkspace，也可以切换到 Docker 或 Remote Workspace；生产路径强调容器隔离、多用户执行和远程 Agent Server。

DAgents 最应借鉴：

- 将工作目录和执行环境区分为不同概念；
- 本地、容器、SSH/Remote 通过稳定 Provider 接口承载；
- Agent loop 不感知具体执行后端；
- 资源限制、环境生命周期和隔离由 Workspace/Execution 层负责；
- 本地快速模式与生产隔离模式共用上层语义。

不应直接追逐：

- 云端软件工程 Agent 的规模化调度；
- 将 Docker 作为所有个人用户的强制前置依赖。

官方来源：[OpenHands](https://github.com/OpenHands/OpenHands)、[SDK 架构](https://docs.openhands.dev/sdk/arch/overview)、[Sandbox 概览](https://docs.openhands.dev/openhands/usage/sandboxes/overview)、[设计原则](https://docs.openhands.dev/sdk/arch/design)。

### 3.3 DeepSeek Harness：事件溯源与全面插件化

DeepSeek Harness 当前仍标记为 Developer Preview，但已形成相当完整的插件化架构：append-only `SessionEvent` 是会话事实源，模型消息由事件投影生成；工具、LLM、Agent、sandbox、terminal、skills、memory、subagent 和 Web Client 均有明确的 service/provider seam。

这意味着 DAgents 早期“Harness 主要只是轻量 Harness”的认识已经过时。当前最值得借鉴的是：

- Durable Session Event 是恢复、审计、上下文和 UI 投影的共同来源；
- 实时扩展事件与持久会话事件分层；
- 插件注册、卸载和热更新有统一生命周期；
- subagent 是 Provider，父 Agent 不绑定具体启动方式；
- UI 从权威 Session Event 构建，而不是复制后端状态机；
- 工具、凭据、sandbox 和会话查询都有稳定能力接口。

DAgents 不应照搬“一切皆插件”。Node 的会话、审批、策略和持久化属于稳定内核；插件化应主要用于工具、Provider、Hook、Skill 和外部集成，避免把核心状态一致性变成插件组合问题。

官方来源：[Harness 仓库](https://github.com/deepseek-ai/deepseek-harness)、[架构](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/architecture.md)、[Session](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/subsystems/session.md)、[Subsystems](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/subsystems/README.md)、[Subagent](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/subsystems/subagent.md)。

### 3.4 Goose：通用本地 Agent 与 MCP 扩展生态

Goose 的产品承诺与 DAgents Node 最接近：本机运行、桌面/CLI/API、多模型、MCP 扩展、记忆、Computer Controller、Skills、subagent 和 Recipes。其优势在于安装体验、跨平台发布、扩展目录、动态扩展管理和可复用 Recipe。

DAgents 最应借鉴：

- Recipe 将 prompt、参数、扩展和依赖打包成可分享能力；
- 扩展安装前的来源、恶意包和权限检查；
- 操作系统 keyring 优先的凭据存储；
- Session 中动态启用扩展，同时保持默认配置稳定；
- 统一诊断和用户可操作的错误信息；
- 成本显示和自动压缩配置。

DAgents 不应与 Goose 比拼 MCP 数量。MCP 生态天然共享，差异化应放在 Agent 绑定、执行环境、策略、审批和跨机器治理上。

官方来源：[Goose](https://github.com/aaif-goose/goose)、[扩展](https://github.com/aaif-goose/goose/blob/main/documentation/docs/getting-started/using-extensions.md)、[Recipe Reference](https://github.com/aaif-goose/goose/blob/main/documentation/docs/guides/recipes/recipe-reference.md)、[权限说明](https://github.com/aaif-goose/goose/blob/main/documentation/docs/mcp/developer-mcp.md)。

### 3.5 OpenClaw：常驻 Gateway、渠道和运维产品化

OpenClaw 是自托管多渠道 Agent Gateway。它把 Gateway 作为 Session、路由和渠道连接的真相源，支持多个隔离 Agent、每个 Agent 独立 workspace/state/session、渠道绑定、插件、memory、cron、移动节点、doctor、安全审计和 sandbox。

DAgents 最应借鉴：

- onboarding、pairing、doctor 和 `security audit` 的完整产品路径；
- Agent、workspace、state、session 和 channel binding 的清晰用户模型；
- 配置 schema 同时驱动校验、文档和 UI；
- 常用配置与高级配置分层；
- 插件 ownership、冲突检查、registry 修复和信任提示；
- 远程暴露前的安全检查和可执行修复建议。

DAgents 不应立即追逐所有聊天渠道。若进入渠道，应优先服务企业通知、审批和任务入口，而不是复制 OpenClaw 的个人助手路线。

官方来源：[OpenClaw](https://github.com/openclaw/openclaw/blob/main/docs/index.md)、[多 Agent](https://github.com/openclaw/openclaw/blob/main/docs/concepts/multi-agent.md)、[Sandbox](https://github.com/openclaw/openclaw/blob/main/docs/gateway/sandboxing.md)、[安全暴露检查](https://github.com/openclaw/openclaw/blob/main/docs/gateway/security/exposure-runbook.md)、[插件](https://github.com/openclaw/openclaw/blob/main/docs/tools/plugin.md)。

### 3.6 Letta：可检查、可版本化的长期记忆

Letta 将长期记忆作为产品核心。其 MemFS 使用 Markdown 和 Git 保存记忆：关键系统记忆可进入稳定上下文，其他内容按需发现；用户和 Agent 可以检查、编辑、整理、版本化和恢复记忆，后台 Agent 可以执行 consolidation/dreaming。

DAgents 最应借鉴：

- 记忆必须可见、可解释、可编辑和可恢复；
- 每条记忆具有来源、版本、更新时间和作用域；
- 记忆整理与活动 Turn 分离；
- 冲突和重复通过候选、审查和版本处理；
- 关键小集合稳定注入，长尾记忆按需召回；
- 提供记忆健康检查，而不只提供 search/get 工具。

DAgents 不必把全部记忆迁移为文件。SQLite 更适合结构化索引、作用域、事务和冲突处理；可以增加可导出、可审查的 Markdown/Git 投影，而不是放弃现有结构化存储。

官方来源：[Letta](https://github.com/letta-ai/letta-code)、[Memory 与 Dreaming](https://github.com/letta-ai/letta-docs-md/blob/main/configuration/memory/index.md)、[MemFS](https://github.com/letta-ai/letta-docs-md/blob/main/concepts/memfs/index.md)。

### 3.7 Cline：开发工作流与可撤销体验

Cline 已经从 VS Code 扩展扩展到 CLI、SDK、Kanban、subagent teams、schedule 和聊天连接器。其用户优势是项目上下文、Plan/Act、命令审批、diff、checkpoint/undo 和工作树隔离。

DAgents 最应借鉴：

- 对文件变更提供 diff、变更集合和撤销入口；
- 对长期任务提供任务板，而不是只显示聊天消息；
- 计划、执行和验证阶段具有清晰视觉状态；
- Headless CLI 和结构化输出支持自动化；
- 多 Agent 任务拥有隔离工作目录和依赖关系；
- SDK 与 UI 共用同一个 Agent Core。

DAgents 不应以 IDE 插件作为主战场。其工作目录、桌面控制、远程终端和 Workgroup 已经超出 IDE Agent 边界。

官方来源：[Cline](https://github.com/cline/cline)、[Cline CLI](https://github.com/cline/cline/blob/main/apps/cli/README.md)、[Cline SDK](https://github.com/cline/cline/blob/main/sdk/README.md)。

## 4. 市场能力正在快速同质化

以下能力正在成为同类产品的基础配置，不足以单独构成 DAgents 的竞争壁垒：

- 本地运行和多模型 Provider；
- MCP、Skills、Rules 和插件；
- 文件、Shell、Terminal 和 Browser；
- 子 Agent 与任务委派；
- Memory 和上下文压缩；
- 桌面应用与 Web UI；
- 定时任务和自动化；
- HITL 和 auto-approve；
- 多模态与 Computer Use。

因此，DAgents 不应继续以功能清单作为主要产品叙事。真正可形成壁垒的是这些能力在内网、多机、异构系统和人工治理条件下是否能够可靠组合。

## 5. DAgents 当前基线

根据当前实现和文档，DAgents 已形成以下主链：

```text
Web UI / Desktop Shell
        ↓ HTTP + SSE
Agent Node
  Session → InputBox → Turn → Step → LLM / Tools / HITL
        ↓
  SQLite / history / runtime state
        ↓ Node 主动 WSS
Manage
  Registry / Workgroup / Timeline / Release / Console
```

当前边界的关键特点：

- Node 是本地 Agent、LLM、工具、审批和本地数据的真相源；
- Manage 是 Registry、Workgroup、Timeline 和集中管理的控制面；
- Manage 不主动反向调用 Node HTTP；
- Workgroup 成员引用 Node 上已经存在的 Agent；
- Agent 工作目录创建后固定，并与 Node runtime 管理目录区分；
- 普通用户输入进入 InputBox，工具结果在同一 Turn 链条内继续；
- MessageQueue 主要承载 resume、异步事实和恢复 continuation；
- Web UI 和 Shell 不应复制 Node 的业务状态机。

现行依据：[`../architecture.md`](../architecture.md)、[`../roadmap.md`](../roadmap.md)、[`../design/manage-architecture.md`](../design/manage-architecture.md)。

## 6. DAgents 的结构性优势

### 6.1 出站式多机控制架构

Node 主动连接 Manage，不要求内网节点开放回调地址，也不建立 Node-to-Node 直连。这适合 NAT、防火墙、分支机构和受限网络，是 DAgents 相较单机 Agent 和云沙箱平台最清晰的架构差异。

### 6.2 本地执行权与本地策略不可被控制面绕过

Agent 使用本机工作目录、终端、MCP、Skills、凭据和 Computer Use；Manage 可以编排和收紧策略，但不能绕过 Node 的本地 policy。这为“中央协调、边缘执行”提供了可信边界。

### 6.3 Workgroup 复用真实 Agent

工作组成员引用现有 Agent，而不是在 Manage 里复制 prompt、工具和模型配置。这样避免配置漂移，并保留 Agent 的真实运行环境、工作目录和本地权限。

### 6.4 Windows/Linux 和传统环境覆盖

Go Node、内嵌 Web UI、Windows x86/x64、Linux 包、SSH 通道和双桌面 Shell 对企业存量环境友好。尤其是旧 Windows、内网 Linux、无头服务器和离线部署，可以成为有价值的垂直优势。

### 6.5 HITL 是运行时能力，不只是前端交互

审批、询问、恢复和取消已经位于 Turn/持久状态链条中，具备继续演进成统一执行治理模型的基础。

## 7. DAgents 的主要劣势与风险

### 7.1 产品定位过宽

当前同时面向个人用户、开发者、企业内网、多机协作、桌面自动化和通用 Agent。用户难以快速判断 DAgents 最适合解决哪一个高价值问题。

风险：

- 产品首页只能罗列能力；
- 每个竞品都能在某一单项上表现得更成熟；
- 开发资源被终端、浏览器、记忆、渠道、工作组和发布平台同时分散；
- 用户无法形成稳定的推荐理由。

### 7.2 企业定位领先于安全成熟度

当前审批和工作目录不等同于 OS sandbox；Manage 仍存在开放模式、默认管理员账号和未完整实现的角色权限；Node 非 loopback 部署也缺少完整设备身份和组织鉴权。

在完成身份、RBAC、Secret Store、sandbox、审计和安全升级链之前，产品描述应使用“企业内网友好”或“可治理预览”，避免宣传为已经完成的企业级安全平台。

### 7.3 状态一致性成本较高

InputBox、MessageQueue、Turn、Step、工具 continuation、HITL、异步浏览器任务、子 Agent、Workgroup outbox、SSE hydrate 和 UI projection 同时存在。历史上多次出现审批卡片、取消、迟到回调、刷新恢复和消息序列问题，说明缺少一个足够统一的执行事件模型。

后续不能再以增加 Queue 特殊消息或 UI 补偿分支为主要修复方式，应逐步收敛为权威 Execution/Item 生命周期和 Durable Event 投影。

### 7.4 执行后端语义仍未完全统一

本地 Shell、Terminal、SSH/Linux channel、Browser、Computer Use、子 Agent 和 Workgroup 对取消、超时、结果未知、重启恢复和资源限制的定义仍有差异。若继续独立演进，将形成多个近似但不兼容的任务系统。

### 7.5 生态与外部开发入口弱

项目有 OpenAPI、Client、Skills、MCP、Hook 和外置工具，但还没有形成一个开发者可快速采用的稳定 SDK、插件契约、模板和兼容性政策。Manage 的 Skills/Plugins/ExternalTools 也尚未完全闭环到 Node 自动同步。

### 7.6 开源产品展示与社区基础弱

当前 GitHub About、README 首屏、截图、演示视频、示例场景和外部案例不足，无法让新用户快速理解真实体验。仓库工程规范已有较好基础，但对外可发现性和可信证据明显弱于同类项目。

### 7.7 多语言、多运行时带来维护成本

Go Node、Python Manage、Vue Web UI、Rust Tauri、Go 兼容 Shell 和 Python Browser Sidecar 共同组成产品。该结构有现实理由，但要求稳定契约、兼容矩阵和跨组件真实测试，否则小团队很容易被集成问题拖累。

## 8. 推荐的差异化定位

### 8.1 一句话定位

推荐：

> DAgents 让组织在自己的 Windows/Linux 机器上运行、管理和协作多个 Agent，同时保留本地执行权、人工审批与完整审计。

不推荐继续使用：

> 支持多 Agent 交互的通用 Agent 工具。

后者没有说明目标用户、使用环境、核心价值和与同类项目的差异。

### 8.2 四个产品支柱

#### Local Sovereignty

模型可以来自外部服务，但数据、工具、凭据和最终执行权默认属于 Node 所在机器。

#### Human-Governed Execution

每个副作用都可以回答：谁请求、哪个 Agent、哪个 Turn、在哪台机器、使用什么权限、谁批准、是否完成、能否取消和如何追溯。

#### Heterogeneous Agent Fleet

Windows 桌面、Linux 服务器、SSH 主机和无头节点可以加入同一个受控网络，但不要求互相开放端口或共享秘密。

#### Recoverable Collaboration

多 Agent 协作具有任务身份、事件游标、审批、取消、终态、恢复和审计，不以非结构化群聊替代可靠协作。

### 8.3 优先目标用户

建议优先服务：

1. 有多台 Windows/Linux 机器的开发、运维和内部自动化团队；
2. 不能把执行环境和全部数据迁往 SaaS 的内网组织；
3. 需要人工审批、操作审计和节点级权限边界的团队；
4. 需要 Agent 操作桌面、终端和远程主机，而不仅是修改代码的用户。

次要用户可以是个人高级用户，但不应以消费者多渠道助手作为首要路线。

## 9. 推荐目标架构

### 9.1 统一执行对象

建议引入跨工具类型的执行身份和生命周期：

```text
Execution
├─ execution_id
├─ owner: agent / session / turn / step / tool_call
├─ kind: shell / terminal / browser / computer / child_agent / workgroup
├─ target: local / ssh / container / workgroup-node
├─ status
├─ policy_decision / approval_id
├─ started_at / finished_at
├─ output_ref / artifact_refs
├─ error_code / retryable
└─ event_seq
```

统一状态至少包括：

```text
queued
running
awaiting_approval
awaiting_user
succeeded
failed
denied
cancelled
timed_out
interrupted
indeterminate
```

`indeterminate` 表示副作用可能已发生但无法确认，不能自动重试非幂等操作。

### 9.2 Durable Event 与 Realtime Event 分层

```text
Durable Event
  用户输入、assistant 最终消息、tool call/result、HITL、Turn 终态
  → 恢复、审计、上下文、历史、UI hydrate

Realtime Event
  token delta、PTY 输出、进度、心跳、临时连接状态
  → 实时 UI，可丢弃，可通过快照重新对账
```

避免把 UI SSE、模型历史和持久审计分别维护为互相补丁式同步的三份状态。

### 9.3 Execution Provider

```text
Tool
  → Policy Engine
  → Execution Provider
       ├─ LocalProvider
       ├─ SSHProvider
       ├─ ContainerProvider
       └─ WorkgroupProvider
  → Execution Event / Result
```

Provider 负责执行位置、进程、环境变量、资源限制和取消；Tool 负责模型参数和业务语义；Turn 负责同一轮模型 continuation；MessageQueue 不承担通用任务管理器职责。

### 9.4 Manage 保持控制面边界

Manage 应继续负责：

- Registry 与 Agent catalog；
- Workgroup 与可靠控制帧；
- 组织策略和节点分组；
- 审计、指标、版本和制品；
- 节点健康与兼容性视图。

Manage 不应负责：

- 直接执行 Node 本地工具；
- 保存可直接使用的 Node 本地秘密；
- 主动反向访问 Node HTTP；
- 复制 Node 的 Agent loop；
- 绕过本地 policy。

## 10. 产品化缺口

### 10.1 安全与身份

- Manage 首次启动强制设置管理员凭据；
- 删除生产路径匿名 admin 语义；
- Node/Manage 设备配对、证书或稳定设备身份；
- OIDC/SSO 与完整 RBAC；
- Node 非 loopback 监听的鉴权和安全检查；
- OS keyring 或稳定 Secret Store；
- 敏感信息脱敏和凭据轮换；
- 可选 Docker/Podman、Linux Landlock/bwrap、Windows restricted token sandbox；
- 文件 symlink/junction/UNC/TOCTOU 安全测试。

### 10.2 可靠性与恢复

- 所有执行对象统一终态；
- 取消、超时、重启、断线和迟到事件的一致语义；
- 大历史分页和增量 hydrate；
- Browser Sidecar 重启后的任务对账；
- Workgroup gap reconcile、fencing 和重复投递测试；
- Node/Manage schema 和 protocol compatibility matrix；
- 数据库迁移备份、失败回滚和升级演练。

### 10.3 运维与诊断

建议提供 `dagents doctor` 和 UI 诊断中心，覆盖：

- Node 端口和 runtime；
- LLM Provider 和模型调用；
- MCP 连接、工具目录和命名冲突；
- Browser Service；
- Computer Use 后端；
- SSH host key、DNS、TCP、认证和 shell；
- Manage/WSS 和 Workgroup 游标；
- 数据库、磁盘、日志、版本和迁移；
- sandbox 实际状态；
- 一键导出脱敏支持包。

### 10.4 用户产品能力

- 统一任务中心；
- 运行中、等待审批、失败、未知和已完成筛选；
- 预置 policy profile：只读、开发、运维、桌面控制、高风险；
- Workgroup 任务视图、依赖、负责人、进度和结果；
- 文件变更 diff、checkpoint 和可恢复撤销；
- Agent 导入、导出、克隆和迁移；
- Token、耗时和成本统计；
- 清晰的错误恢复入口，而不是只显示报错文本。

### 10.5 Memory 产品化

- 记忆来源和证据；
- Agent、Workspace、Global 作用域可视化；
- 创建、更新、最近命中和过期时间；
- 冲突候选和人工决议；
- 合并、替换、并存和撤销；
- 召回原因和本轮 token 成本；
- 重复、陈旧和超长记忆健康检查；
- Markdown/Git 导出或审查投影；
- 长对话、作用域隔离、冲突和超长策略评测集。

### 10.6 扩展生命周期

- Skills/MCP/Plugin/ExternalTool 的统一来源信息；
- 版本、依赖、签名、信任等级和兼容范围；
- 安装、启用、禁用、升级、回滚和卸载；
- 工具名长度与冲突的确定性映射；
- Node 与 Manage 同步状态；
- 插件 ownership 和重复能力诊断；
- 标准开发模板、示例和验证工具。

### 10.7 发布与供应链

- Windows 安装包代码签名；
- Linux/Manage 制品签名；
- SBOM；
- provenance/attestation；
- 自动生成校验和与签名验证说明；
- 升级失败回滚；
- 支持版本和兼容矩阵；
- 公开安全公告与修复时效。

### 10.8 开源产品与社区

- 更新 GitHub About 和 topics；
- README 增加真实截图、短视频和三条典型工作流；
- 独立文档站和搜索；
- 发布可复现的演示环境；
- `good first issue`、模块维护人和公开 Roadmap；
- 插件/Skill 示例仓库；
- 真实 LLM 与跨平台测试报告；
- 可比较的 benchmark/eval；
- 用户案例和失败边界说明。

## 11. 分阶段演进路线

### 阶段 A：可信本地执行

目标：把 Node 从“功能完整”提升到“异常情况下仍可解释、可恢复”。

交付：

1. 统一 Execution/Item 生命周期；
2. Durable/Realtime Event 分层；
3. doctor 与脱敏支持包；
4. Manage 默认安全收紧；
5. 可选 sandbox provider 首版；
6. 真 LLM、取消、审批、重启和 Browser/Terminal 故障测试；
7. 安装与升级回滚演练。

退出标准：

- UI 刷新不丢失权威状态；
- Turn 和工具取消后消息序列始终合法；
- Node 重启后所有运行对象进入确定终态或 `indeterminate`；
- 非 loopback 暴露有明确鉴权和告警；
- 安全边界不依赖文档提醒才能成立。

### 阶段 B：受控 Agent Fleet

目标：把 Workgroup 和 Manage 变成真正可运营的多机 Agent 产品。

交付：

1. Node/Agent 健康、版本和能力目录；
2. Workgroup Task/Assign 的任务化视图；
3. 组织策略 overlay 和资源配额；
4. 审批、Ask User 和取消的统一路由；
5. 断网、重连、gap、重复投递和旧连接 fencing 演练；
6. 审计导出与敏感信息脱敏；
7. Skills/Plugins/ExternalTools 的 Node 主动同步。

退出标准：

- Manage 重启或网络中断不会重复执行非幂等任务；
- 任一任务都能定位到 Agent、Node、Session、Turn 和审批人；
- 控制面只能收紧权限，不能绕过 Node；
- 用户无需阅读协议即可判断成员当前在做什么、是否需要自己处理。

### 阶段 C：开放执行生态

目标：让外部开发者可以基于 DAgents 构建能力，而不是修改核心仓库。

交付：

1. 稳定 Headless CLI 和生成式 SDK；
2. Execution Provider、Tool、Hook、Skill 和 Package 契约；
3. Recipe/Agent Template；
4. 包签名、兼容性检查和回滚；
5. 官方插件和行业示例；
6. 有限且高价值的企业渠道入口；
7. 公开 eval 和兼容测试矩阵。

退出标准：

- 外部扩展不需要修改 Node 核心；
- 插件升级失败可以回滚；
- API 和事件具有明确 semver 政策；
- 示例项目可以在短时间内完成安装、运行和验证。

### 阶段 D：1.0 产品化

目标：建立稳定支持边界。

最低门槛：

- 稳定数据迁移和升级回滚；
- 完整身份、RBAC 和审计；
- 至少一种可支持的 OS sandbox 路径；
- Node/Manage/Desktop/Browser 版本兼容矩阵；
- 真实网络和真实 LLM 回归；
- 已签名发布制品和 SBOM；
- 明确支持周期、安全响应和故障恢复文档；
- 至少三类可复现的真实用户案例。

## 12. 明确不建议作为近期主线

- 与 Codex/Cline 正面竞争纯编码体验；
- 复制 OpenClaw 的全部聊天渠道；
- 以 MCP 服务数量作为产品指标；
- 将 Node 核心状态机全面插件化；
- 通用可视化工作流画布；
- Node-to-Node 直接派活；
- Manage 反向调用 Node HTTP；
- 继续扩大 MessageQueue 的通用任务调度职责；
- 默认捆绑 LibreOffice 等大型第三方应用；
- 在没有隔离边界时宣传产品级沙箱；
- 在可靠性和安全性未闭环前继续横向增加高风险工具。

## 13. 产品与工程决策原则

后续功能进入 Roadmap 前，应回答：

1. 是否强化“本地执行权、人工治理、跨机器协作”之一？
2. 是否可以复用统一 Execution 和 Event，而不是新增特殊状态机？
3. 权威状态由谁拥有？重启后如何恢复？
4. 取消、超时、重复投递和未知副作用如何处理？
5. 能否通过 Provider、Tool、Hook 或 Package 扩展，而不修改 Turn 内核？
6. 是否增加安全攻击面？默认配置是否安全？
7. 用户是否能在 UI 中理解状态和下一步操作？
8. 是否有真实场景测试，而不仅是单元测试？
9. 新逻辑落地后，哪一条旧路径应被删除？

若一个功能不能强化核心定位，却同时增加 Node、Manage、Web UI 和 Shell 的跨层复杂度，应默认拒绝或延后。

## 14. 建议衡量指标

### 可靠性

- Turn 非预期失败率；
- 取消后非法消息序列次数；
- hydrate 后状态不一致次数；
- 重启后无法对账的 Execution 数量；
- Workgroup 重复执行率和 gap 恢复时间。

### 安全

- 默认配置高危检查数量；
- 未经审批的高风险执行数量；
- Secret 泄漏自动测试覆盖；
- sandbox 覆盖的执行比例；
- 审计事件完整率。

### 效果与成本

- 真实任务完成率；
- 平均模型 Step 和工具调用次数；
- Memory 召回准确率和无用 token；
- 每任务 token、时间和人工审批次数；
- 子 Agent/Workgroup 带来的净成功率提升。

### 产品

- 首次安装到成功 Turn 的时间；
- 首次 MCP、Terminal、Workgroup 配置成功率；
- 错误后用户自助恢复率；
- 版本升级成功率；
- 文档到功能的转化路径。

## 15. 最终结论

DAgents 不缺功能，缺的是把已有能力压缩成一个鲜明、可信、可验证的产品承诺。

最有价值的路线不是继续成为更大的通用 Agent，而是成为：

> **组织自有机器上的 Agent 运行、治理与协作基础设施。**

Node + Manage + Workgroup 的总体方向应保留。下一阶段应优先吸收 Codex 的权威 Item 生命周期、OpenHands 的 Workspace/Execution Provider、Harness 的 Durable Event 和能力 seam、OpenClaw 的 doctor/安全产品化、Letta 的可检查记忆以及 Cline 的任务与撤销体验。

只有当可靠性、安全、诊断、扩展生命周期和开源呈现闭环后，DAgents 的功能宽度才会真正转化为产品优势。
