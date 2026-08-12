# Agent 实例模型与 Node 重构（v0.8+）

> **状态**：设计冻结（2026-07）；**v0.9.1 勘误**见文首「现行修正」  
> **范围**：Node + Web UI 先行；Manage / Workgroup 已接入预览  
> **破坏性**：v0.* 允许不保留 session / agent_id 旧语义与数据迁移

## 现行修正（v0.9.1，请先读）

| 项 | 现行 | 下文过时处 |
|----|------|------------|
| **沙箱** | **已移除**产品与运行时（无 `node/internal/sandbox`）；Agent 共用 Node `fs_root`，边界靠工具组 + policy | §3 图、§4.2–4.4、Phase 2/3 勾选、§12 模板「沙箱」列 |
| **人机入口** | **Web UI**（`/ui/`）为主；`dagents-client` 仅 probe/update/version | 若仍写 TUI 对话为默认，以根 README 为准 |
| **跨机协作** | **Workgroup**（Manage Leader + Node Worker） | §1「Manage / A2A 后续」→ 已见 [workgroup-and-node-gateway.md](./workgroup-and-node-gateway.md) · [handbook/07](../handbook/07-Workgroup协作.md) |
| **Placement** | 产品路径拆除 | 仍正确标为废弃 |

本文其余章节保留 v0.8 设计过程与阶段勾选，**实现以 CHANGELOG / 代码为准**。

本文替代已移除的「三组件 + Session 中心 + 多 Client」设计（`three-component-model.md`、`local-assistant.md`、`client-packaging.md` 等）。

---

## 1. 决策摘要

| 项 | 决策 |
|----|------|
| 部署模型 | **单 Node 进程、多 Agent 实例** |
| 对话模型 | **1 Agent = 1 主对话**（不再有用户可见的多 session） |
| 进程身份 | 原 `agent_id` → **`node_id`**（标记不同服务器上的 Node） |
| 用户 Agent | 新 **`agent_id`**（实例 UUID，Node 内唯一） |
| 交互入口 | **仅 Web UI**（`/ui/`）；对话型 TUI/CLI 已移除 |
| 创建方式 | **Agent 模板** → 实例化配置 → 新建 Agent |
| 名称 | **`display_name`** 可在 UI 修改，不影响 `agent_id` |
| 沙箱 | **已移除**（见文首现行修正）；隔离靠独立 Node / Workgroup 成员工作区 |
| Manage | Registry + **Workgroup**；旧 A2A inbox 另轨 |
| 数据 | **不保证**旧 session 数据迁移；可清空重建 |
| 工作组（现行） | 跨机器协作 **只经工作组**；见 [workgroup-and-node-gateway.md](./workgroup-and-node-gateway.md) |
| 远程 Agent / Placement | **不做**（跨机走 Workgroup） |

---

## 2. 术语

| 术语 | 含义 |
|------|------|
| **Node** | 单进程运行时：HTTP/SSE、LLM、工具、存储 |
| **`node_id`** | Node 全局身份（原 `config.agent_id`）；持久化于 `<fs_root>/node/node_id` |
| **Agent 模板** | 只读蓝图：`packaging/agent-templates/*.yaml` |
| **Agent 实例** | 用户创建的对话实体；含 `agent_id`、`display_name`、有效配置快照 |
| **Child Agent** | 父 Agent 内临时子任务 runtime（保留，挂父 `agent_id`） |
| **Session**（内部） | 实现细节可保留为 `agent_id` 的别名或 1:1 内部 id；**不对用户暴露** |

---

## 3. 架构总览

```text
┌─────────────────────────────────────────────────────────────┐
│  Web UI（唯一人机入口）  GET /ui/                            │
└───────────────────────────────┬─────────────────────────────┘
                                │ HTTP / SSE
┌───────────────────────────────▼─────────────────────────────┐
│  Agent Node（单进程）                                          │
│  node_id: srv-prod-01                                         │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐           │
│  │ Agent A     │  │ Agent B     │  │ Agent C     │  ...      │
│  │ 通用助手     │  │ 代码审查     │  │ 运维执行     │           │
│  │ 工具组约束   │  │ 工具组约束   │  │ 工具组约束   │           │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘           │
│         │ 1 主对话        │                │                  │
│  AgentManager + per-agent AgentRuntime（TurnOptions / Registry）│
└───────────────────────────────┬─────────────────────────────┘
                                │ manage.enabled
                    ┌───────────▼───────────┐
                    │  Manage · Workgroup    │
                    └───────────────────────┘
```

> **勘误**：上图原「sandbox:on/off」已删除；现行无沙箱后端。

---

## 4. 配置分层

```text
Node 全局配置（config.yaml）
  listen, log, manage（连接，非实例）, ui
  node_id
  fs_root（Node 级根；Agent 工作区在其下）
        │
        ▼
Agent 模板 defaults（packaging/agent-templates/<id>.yaml）
  agent.role / description
  llm（profiles + active）
  tools.enabled_groups
  skills, hooks, compression, child_agents
  # sandbox：已移除；勿再写入模板 defaults
        │
        ▼
Agent 实例（agents 表 + agents/<agent_id>/）
  display_name（可改）
  template_id
  config_snapshot（实例化后 YAML/JSON）
  messages / history
```

### 4.1 Node 全局（`config.yaml`）

保留在进程级、**不**随 Agent 切换：

- `node_id`（原 `agent_id` 字段重命名）
- `listen` / `local.endpoint`
- `manage.*`（仅连接参数；注册语义后续改）
- `ui.enabled`
- `log.level`
- `fs_root`（Node 根目录）

### 4.2 Agent 模板 schema（草案）

```yaml
id: code-reviewer
display_name: 代码审查助手
description: 只读代码、输出审查意见，默认沙箱运行
version: 1

defaults:
  agent:
    role: reviewer
    description: 专注代码质量与安全审查
  llm:
    active: default
    profiles:
      default:
        provider: deepseek
        model: deepseek-chat
        api_key_env: OPENAI_API_KEY
        multimodal_enabled: false
    max_tool_loops: 24
  tools:
    enabled_groups: [fs_read, git]
  skills:
    enabled: true
    preload: []
  hooks: {}
  compression: {}
  child_agents:
    enabled: false

# 勿再写 sandbox.*（产品已移除）
```

### 4.3 Agent 实例字段

| 字段 | 说明 |
|------|------|
| `agent_id` | UUID，`agents` 表主键 |
| `display_name` | UI 展示名，`PATCH` 可改 |
| `template_id` | 创建时模板 id |
| `sandbox_*` 列 | **遗留 DB 列**；读写固定关闭，勿依赖 |
| `config_snapshot` | 实例化后的完整有效配置 |
| `created_at` / `updated_at` | |
| `archived` | 软删除 |

### 4.4 沙箱语义（历史 · 已移除）

> **v0.9.1**：沙箱运行时与配置面已删除。下列 YAML / 表格仅保留设计史；**勿再实现或对外承诺**。  
> 现行边界：Node 全局 `fs_root` + 工具组 + policy；跨机隔离用 **独立 Node** 或 **Workgroup 成员工作区**。

<details>
<summary>展开历史沙箱设计（已废弃）</summary>

```yaml
sandbox:
  enabled: true
  backend: process   # process | docker（历史）
  workspace_subdir: data
  allow_bash: false
  allow_network_tools: false
  fs_root_isolation: true
```

`sandbox.enabled: true` 时（历史行为）：

| 能力 | 行为 |
|------|------|
| **工作区** | `fs_root` 解析为 `<node_fs_root>/agents/<agent_id>/` |
| **文件工具** | 仅能读写该 Agent 工作区 |
| **Bash** | 默认 `allow_bash: false` |
| **非沙箱** | 共享 Node `fs_root` |

Docker 后端（历史）：`packaging/sandbox/Dockerfile`、常驻容器 + `docker exec` 等 — **代码已删除**。

</details>

---

## 5. 存储布局

```text
<node_fs_root>/
  node/
    node_id                 # 原 agent/agent_id
  agents/
    <agent_id>/
      memory/               # 可选 per-agent sqlite 或统一 agents.db 外置
      data/                 # 沙箱工作区
      policy/
      history/
      skills/               # 可选 per-agent skills 覆盖
  agents.db                 # Agent 元数据 + messages（或扩展现有 sqlite）
  agent-templates/          # 可选用户自定义模板（覆盖内置）
```

**破坏性迁移**：删除或重建 `memory/sessions.db`；旧 `sessions` 表在启动时迁入 `agent_runtimes`。

---

## 6. HTTP API（Node + UI）

### 6.1 新 API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/v1/agent-templates` | 列出内置 + 用户模板 |
| GET | `/v1/agent-templates/{id}` | 模板详情 |
| POST | `/v1/agents` | `{ template_id, display_name? }` 创建（忽略历史 `sandbox` 字段） |
| GET | `/v1/agents` | 列表 |
| GET | `/v1/agents/{agent_id}` | 元数据 + 配置摘要 |
| PATCH | `/v1/agents/{agent_id}` | `{ display_name }` |
| DELETE | `/v1/agents/{agent_id}` | 归档或硬删 |
| GET | `/v1/agents/{agent_id}/hydrate` | 历史 + HITL（替代 session hydrate） |
| POST | `/v1/agents/{agent_id}/messages` | 发消息 |
| GET | `/v1/streams?agent_id=` | SSE |
| POST | `/v1/agents/{agent_id}/cancel` | 取消 turn |
| … | skills / compress / clear-context | 路径中 `session_id` → `agent_id` |

### 6.2 移除 / 不实现

- `POST/GET /v1/sessions`（已完成：路由已删除 → 404）
- Go `dagents-client tui|chat`
- Python `dagents chat`
- `local.agent_id` 客户端校验字段 → 改为可选 `node_id` 校验

### 6.3 健康与元信息

```json
GET /health
{ "node_id": "...", "version": "..." }

GET /v1/node/info
{ "node_id", "capabilities", "agent_count", ... }
```

原 `GET /v1/agent/info` 拆分为 **Node 级** 与 **Agent 级** `GET /v1/agents/{id}/info`。

---

## 7. Web UI

### 7.1 路由

```text
/ui/                      → 默认 Agent 或列表
/ui/agents                → Agent 列表 + 新建
/ui/agents/:agentId       → 主聊天（替代 /chat/:sessionId）
/ui/settings/node         → Node 全局（listen、manage）
/ui/settings/agents/:id   → 实例配置（Phase 2+，先只读展示）
```

### 7.2 交互

- 侧栏：**我的 Agent**（非「历史会话」）
- **新建 Agent**：选模板卡片 → 输入名称 → 可选「沙箱运行」勾选（默认跟模板）
- **重命名**：Agent 标题区或设置内联编辑
- 移除所有 session 文案与 `/new`、`/switch` 斜杠命令

---

## 8. 运行时重构要点

将 **Manager 级单例** 下沉为 **per-agent `AgentRuntime`**：

| 模块 | 现状 | 目标 |
|------|------|------|
| `session.Manager` | 多 session，共享 TurnOptions | `agent.Manager`，每实例独立 TurnOptions |
| `tools.Registry` | 进程级一个 | 每 Agent 一个（或 Registry 工厂 + 动态 FSRoot） |
| `llm.RuntimeSettings` | 进程级 | 每 Agent 或切换时 swap |
| `policy` | `fs_root/policy` | 沙箱 Agent 用 `agents/<id>/policy` |
| Child agent | 父 session | 父 `agent_id` |
| SSE Hub | `session_id` | `agent_id` |

活跃 Agent 可保持 consumer goroutine；非活跃可 idle 卸载（沿用 v0.6 session 卸内存思路，按 `agent_id`）。

---

## 9. Manage / A2A（后续 Phase，仅预留）

现网 Manage 以 **Node 注册 + agent_id** 为中心；重构后建议：

| 现网 | 目标 |
|------|------|
| 注册 `agent_id` = 进程 | 注册 **`node_id`** + Node 元数据 |
| A2A target = agent_id | target = **`node_id` + `agent_id`** 或 Manage 侧新路由 |
| `expose_to_peers` 进程级 | 可下沉到 Agent 实例或保持 Node 级开关 |

**本阶段**：`config.yaml` 保留 `manage` 块但可不启用；代码中 `node_id` 替换 `agent_id` 字符串即可，**不**要求 Manage 联调通过。

---

## 10. 移除项清单

### 10.1 代码（Phase 4）

| 路径 | 说明 |
|------|------|
| `client/cmd/dagents-client/` | ~~`tui`/`chat`~~ 已移除；保留 `probe`/`update`/`version` |
| `client/internal/tui/` | ✅ 已删除 |
| `app/cli/` | ✅ 已删除 |
| `packaging/linux/dagents` | ✅ 默认启动 Node + Web UI |
| Web UI `/chat/:sessionId` | ✅ 改为 `/agents/:agentId` |

### 10.2 配置字段重命名

| 旧 | 新 |
|----|-----|
| `agent_id`（顶层） | `node_id` |
| `AGENT_ID` 环境变量 | `NODE_ID`（可保留别名一个版本） |
| `<fs_root>/agent/agent_id` | `<fs_root>/node/node_id` |
| `local.agent_id` | 删除或 `local.node_id` |

### 10.3 已移除设计文档

见 `docs/design/README.md`；不再维护 Session 中心、双 TUI、Client 同包模型相关长文。

---

## 11. 分阶段实施

### Phase 0 — 设计落地

- [x] 本文档
- [x] 2–3 个内置模板 YAML 样例
- [x] 沙箱可插拔后端（`process` | `docker`）设计预留
- [x] `shared/config`：`node_id` 重命名
- [ ] OpenAPI / handbook 修订计划

### Phase 1 — 数据与 API

- [x] `agents` 表 + `agents.db`
- [x] 模板加载器 `packaging/agent-templates/` + `node/internal/agenttemplate`
- [x] `POST/GET/PATCH/DELETE /v1/agents` + `GET /v1/agent-templates`
- [x] `node_id` 配置与环境变量（`NODE_ID`，兼容读 `AGENT_ID`）
- [x] 过渡：创建 Agent 时同步内部 session（同 id）
- [x] Phase 1 单元测试（config / store / template / agents API）
- [x] 删除对外 `POST/GET /v1/sessions`（路由已移除）
- [x] Web UI Agent 列表面板（Phase 3）

### Phase 2 — 运行时 per-agent

- [x] `CreateWithOptions` + per-agent TurnOptions / Registry（挂在 session.Manager）
- [x] `agentruntime`：工具组约束、Build（**沙箱 FSRoot / Docker 后端已删除**，见文首）
- [x] messages / streams 接受 `agent_id`
- [x] `/v1/agents/{id}/hydrate|cancel|context` 路径
- [x] Phase 2 单元测试
- [ ] Child agent 完全按新 Agent 模型挂接（仍用父 runtime；后续细化）
- ~~Docker 沙箱后端~~ → **已移除（v0.8.x→0.9）**

### Phase 3 — Web UI

- [x] Agent 列表、模板向导、重命名
- [x] 路由 `/agents/:agentId`（`/chat` 重定向兼容）
- [x] `ensureAgentRuntime`：重启 / Release 后按快照恢复
- [x] messages / streams / hydrate 走 `agent_id`
- [x] Web UI 切至 `/v1/agents/{id}/…`（ack/skills/child-agents 等）
- [x] 彻底删除对外 `/v1/sessions` CRUD（路由已移除）
### Phase 4 — 删除 TUI/CLI + 打包

- [x] 删除 client TUI、app/cli（`dagents-client` 保留 probe/update）
- [x] 调整 CI / `dagents-local-assistant` 产物（node + webui；无 dagents-cli）
- [x] Agent 路径别名覆盖 ack/skills/child-agents 等；Web UI 切离 session CRUD
- [x] 更新 handbook、README、CHANGELOG（要点）
- [ ] OpenAPI / handbook 全文修订（持续；v0.9.1 已重写入口）

### Phase 5 — Manage / Workgroup（独立里程碑）

- [x] Manage 注册 `node_id`
- [x] **Workgroup** D0.5–D4 基座与预览闭环（见 workgroup 设计文）
- [ ] 旧 A2A 可观测 / Task 模型收口（另轨）
- [x] Placement 产品路径拆除
- [x] Heartbeat 可选 `local_agents` 公告

---

## 12. 内置模板（首批）

| id | 名称 | 要点 |
|----|------|------|
| `general` | 通用助手 | 默认工具组、多模态可选 |
| `code-reviewer` | 代码审查 | 只读向工具组 |
| `ops-runner` | 运维执行 | bash + 有限 FS |

样例文件目录：`packaging/agent-templates/`。

---

## 13. 风险

| 风险 | 缓解 |
|------|------|
| TurnOptions 共享假设遍布代码 | Phase 2 专 PR；先列 `grep TurnOptions` 调用链 |
| 路径逃逸 | 工具层相对 `fs_root` 校验；Workgroup 成员另有工作区消毒 |
| Manage 现网断裂 | 默认可关 `manage.enabled` |
| 文档滞后 | handbook / README 按预览清单同步；沙箱叙事见文首勘误 |

---

## 14. 相关文档（仍有效）

| 文档 | 说明 |
|------|------|
| [go-node-internals.md](../architecture/go-node-internals.md) | Node 内部 |
| [agent-node-api.md](../architecture/agent-node-api.md) | HTTP/SSE |
| [child-agent-tools.md](../architecture/child-agent-tools.md) | 子 Agent |
| [agent-hooks.md](./agent-hooks.md) | Hook |
| [workgroup-and-node-gateway.md](./workgroup-and-node-gateway.md) | **跨机协作现行规范** |
| [../handbook/07-Workgroup协作.md](../handbook/07-Workgroup协作.md) | 工作组用户向 |
| [manage-architecture.md](./manage-architecture.md) | Manage 架构 |
