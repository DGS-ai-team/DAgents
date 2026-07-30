# Agent 实例模型与 Node 重构（v0.8+）

> **状态**：设计冻结（2026-07）  
> **范围**：Node + Web UI 先行；Manage / A2A 后续大改  
> **破坏性**：v0.* 允许不保留 session / agent_id 旧语义与数据迁移

本文替代已移除的「三组件 + Session 中心 + 多 Client」设计（`three-component-model.md`、`local-assistant.md`、`client-packaging.md` 等）。

---

## 1. 决策摘要

| 项 | 决策 |
|----|------|
| 部署模型 | **单 Node 进程、多 Agent 实例** |
| 对话模型 | **1 Agent = 1 主对话**（不再有用户可见的多 session） |
| 进程身份 | 原 `agent_id` → **`node_id`**（标记不同服务器上的 Node） |
| 用户 Agent | 新 **`agent_id`**（实例 UUID，Node 内唯一） |
| 交互入口 | **仅 Web UI**（`/ui/`）；移除 Go/Python TUI 与 CLI 对话模式 |
| 创建方式 | **Agent 模板** → 实例化配置 → 新建 Agent |
| 名称 | **`display_name`** 可在 UI 修改，不影响 `agent_id` |
| 沙箱 | 每 Agent 可配置 **`sandbox.enabled`**，隔离工作区与工具能力 |
| Manage / A2A | 后续 Phase；本阶段 Node 侧预留字段，不保证与现网 Manage 兼容 |
| 数据 | **不保证**旧 session 数据迁移；可清空 `sessions.db` 重建 |
| Placement（进行中） | 同组远端 Node 放置 Agent + 屏幕旁观：见 [remote-agent-placement.md](./remote-agent-placement.md)；将重构 Manage 控制面 / Edge Tunnel，**不等于** `sandbox.backend=remote` |
| 工作组 + 长连接（设计中） | Agent 实例级协作组 + Leader/@ 派任务；Node↔Manage WebSocket：见 [workgroup-and-node-gateway.md](./workgroup-and-node-gateway.md) |

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
│  │ sandbox:off │  │ sandbox:on  │  │ sandbox:on  │           │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘           │
│         │ 1 主对话        │                │                  │
│  AgentManager + per-agent AgentRuntime（TurnOptions / Registry）│
└───────────────────────────────┬─────────────────────────────┘
                                │ 后续 Phase
                    ┌───────────▼───────────┐
                    │  Manage / A2A（重构）   │
                    └───────────────────────┘
```

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
  sandbox（enabled + 约束）
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

sandbox:
  enabled: true          # 默认对该模板开启沙箱
  workspace_subdir: data # 相对 agents/<agent_id>/
  allow_bash: false
  allow_network_tools: false   # browser、a2a 等
  fs_root_isolation: true      # 工具仅能访问本 Agent 工作区
```

### 4.3 Agent 实例字段

| 字段 | 说明 |
|------|------|
| `agent_id` | UUID，`agents` 表主键 |
| `display_name` | UI 展示名，`PATCH` 可改 |
| `template_id` | 创建时模板 id |
| `sandbox.enabled` | 是否沙箱（可由模板默认，创建时可选覆盖） |
| `config_snapshot` | 实例化后的完整有效配置 |
| `created_at` / `updated_at` | |
| `archived` | 软删除 |

### 4.4 沙箱语义

```yaml
sandbox:
  enabled: true
  backend: process   # process | docker（默认 process；docker 为后续增强）
  workspace_subdir: data
  allow_bash: false
  allow_network_tools: false
  fs_root_isolation: true
  # docker 专用（backend=docker 时）：
  # image: dagents-sandbox:latest
  # network: none          # none | bridge | …
  # memory: 512m
  # cpus: "1.0"
```

`sandbox.enabled: true` 时：

| 能力 | 行为 |
|------|------|
| **工作区** | `fs_root` 解析为 `<node_fs_root>/agents/<agent_id>/`（非 Node 全局 `.runtime`） |
| **文件工具** | 仅能读写该 Agent 工作区；禁止逃逸到兄弟 Agent 目录 |
| **Bash** | 默认 `allow_bash: false`；模板可显式开启并配合 policy |
| **Browser / A2A** | 默认关闭（`allow_network_tools: false`） |
| **Policy** | 每 Agent 独立 `agents/<agent_id>/policy/`（可从模板种子复制） |
| **非沙箱** | `sandbox.enabled: false` 时共享 Node `fs_root`（兼容「全权限助手」） |

实现上：`AgentRuntime` 构造 `tools.Registry` 时注入 **effective FSRoot** 与 **enabled_groups 过滤**。

#### 4.4.1 沙箱后端（可插拔，Docker 后续）

| `backend` | 说明 | 阶段 |
|-----------|------|------|
| **`process`（默认）** | 应用层：effective FSRoot + 工具组白名单 + policy | Phase 2 必达 |
| **`docker`（可选）** | 常驻容器 + `docker exec`；工作区 bind-mount；空闲回收 | **已实现** |

Docker 后端要点：

- **不把整个 Node 放进容器**，只隔离危险工具执行路径（`bash_run`）。
- **操作系统**：默认镜像基于 **Alpine Linux 3.20**（`packaging/sandbox/Dockerfile`）。
- Agent 工作区挂载为容器内 `/workspace`；默认 `network: none`、宿主机 uid、可选 CPU/内存上限。
- **生命周期**：Agent **装入内存**时预创建常驻容器（`docker create` + `start`，`sleep infinity`）；`bash_run` 用 `docker exec`；**空闲 15 分钟**回收容器；卸出内存 / 删除 Agent 时立即 `docker rm -f`。
- 无 Docker 时：创建/启用 `backend: docker` 返回 `docker_unavailable`。
- 与 policy 叠层：容器外仍走 HITL 审批；容器内再加资源/网络限制。

模板示例：`ops-runner` 默认仍为 `backend: process`；具备 Docker 后可在 UI / 模板中切 `docker`。

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
| POST | `/v1/agents` | `{ template_id, display_name?, sandbox? }` 创建 |
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
- [x] `agentruntime`：EffectiveFSRoot、工具组沙箱约束、Build
- [x] **沙箱 FSRoot 隔离**（`process` 后端）
- [x] messages / streams 接受 `agent_id` 别名
- [x] `/v1/agents/{id}/hydrate|cancel|context` 路径别名
- [x] Phase 2 单元测试
- [ ] Child agent 完全按新 Agent 模型挂接（仍用父 runtime；后续细化）
- [x] Docker 沙箱后端（`bash_run` → `docker run`；见 `node/internal/sandbox`）

### Phase 3 — Web UI

- [x] Agent 列表、模板向导、重命名
- [x] 路由 `/agents/:agentId`（`/chat` 重定向兼容）
- [x] `ensureAgentRuntime`：重启 / Release 后按快照恢复沙箱
- [x] messages / streams / hydrate 走 `agent_id`
- [x] Web UI 切至 `/v1/agents/{id}/…`（ack/skills/child-agents 等）
- [x] 彻底删除对外 `/v1/sessions` CRUD（路由已移除；测试改走 agents / `sessions.Create`）
### Phase 4 — 删除 TUI/CLI + 打包

- [x] 删除 client TUI、app/cli（`dagents-client` 保留 probe/update）
- [x] 调整 CI / `dagents-local-assistant` 产物（node + webui；无 dagents-cli）
- [x] Agent 路径别名覆盖 ack/skills/child-agents 等；Web UI 切离 session CRUD
- [x] 更新 handbook、README、CHANGELOG（要点）
- [ ] OpenAPI / handbook 全文修订（持续）

### Phase 5 — Manage / A2A（独立里程碑）

- [x] Manage 注册 `node_id`（与 `agent_id` 双写；主键仍为 node 级；`GET /v1/registry/nodes/{node_id}`）
- [ ] A2A 路由与 inbox 模型（实例级 `to_agent_id` 另开；当前仍 node↔node）
- [ ] Triggers `target` 语义
- [x] Placement Control/Edge 读取 `node_id`（peers 回退 `agent_id`）
- [x] Heartbeat 可选 `local_agents` 公告（非 A2A 目标）

---

## 12. 内置模板（首批）

| id | 名称 | 沙箱 | 要点 |
|----|------|------|------|
| `general` | 通用助手 | 关 | 默认工具组、多模态可选 |
| `code-reviewer` | 代码审查 | 开 | 只读 FS、无 bash |
| `ops-runner` | 运维执行 | 开 | bash + 有限 FS，无 browser |

样例文件目录：`packaging/agent-templates/`（Phase 1 添加）。

---

## 13. 风险

| 风险 | 缓解 |
|------|------|
| TurnOptions 共享假设遍布代码 | Phase 2 专 PR；先列 `grep TurnOptions` 调用链 |
| 沙箱路径逃逸 | 统一 `EffectiveFSRoot(agent)`；工具层强制 `StatRelPath` |
| Manage 现网断裂 | Phase 1–4 默认 `manage.enabled: false` |
| 文档大面积失效 | handbook 按 Phase 同步改；本目录旧文已删 |

---

## 14. 相关文档（仍有效）

| 文档 | 说明 |
|------|------|
| [go-node-internals.md](../architecture/go-node-internals.md) | 实现时需同步改 Manager 章节 |
| [agent-node-api.md](../architecture/agent-node-api.md) | Phase 1 起按新 API 重写 |
| [child-agent-tools.md](../architecture/child-agent-tools.md) | 子 Agent 语义保留 |
| [agent-hooks.md](./agent-hooks.md) | Hook 机制仍适用（作用域改为 per-agent） |
| [manage-architecture.md](./manage-architecture.md) | **待 Phase 5 重写** |
