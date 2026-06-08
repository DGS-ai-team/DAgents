# A2A（Agent 间协作）与 Register Center

> **现网状态（v0.2.0+）**  
> - **Register Center**（**`register_center/`**）仍为独立 Python 服务：登记、**`/v1/broadcast`**、**`/v1/relay`**、可选 JSON 持久化。  
> - **Go Agent Node 不集成 RC**：不自登记、不注册 **`agent_peer`** 工具。  
> - **Python FastAPI Agent API 已移除**；下文 §3–§6 描述的 **`agent_*` 工具与自登记** 为 **历史行为**，源码见 [archive/python-agent-runtime/](./archive/python-agent-runtime/)。  
> - **端到端 A2A 闭环** 计划在 **Manage** 阶段恢复，见 [future/a2a-via-manage.md](./future/a2a-via-manage.md)。

本文说明 **Register Center 控制面**（现行）与 **A2A 工具链**（已归档）的设计与 HTTP 契约。RC 实现入口：**`register_center/rc_app.py`**；历史 A2A 工具：**`app/harness/tools/agent_peer.py`**（归档）。

---

## 1. 组件分工（现行 vs 历史）

### 1.1 现行（v0.2.x）

```text
┌─────────────────────┐     可选独立启动
│  Register Center    │     python run_register_center.py
│  /v1/agents         │
│  /v1/broadcast      │
│  /v1/relay          │
└──────────┬──────────┘
           │ HTTP（外部调用方或未来 Manage）
           ▼
    （无仓库内 Agent 自动登记端）

┌─────────────────────┐
│  Go Agent Node      │  本地 turn loop；临时子 Agent（同进程）
│  + Client           │  非 A2A peer 工具
└─────────────────────┘
```

| 组件 | 职责 |
|------|------|
| **Register Center** | 目录与中继 HTTP API；详见 §2 与本目录 **`register_center/README.md`**。 |
| **Go Agent Node** | 本地助手运行时；**`create_temporary_agent`** 等为 **同进程子 Agent**，非跨实例 A2A。 |

### 1.2 历史（0.1.x Python Agent API，已移除）

```text
                    ┌─────────────────────────┐
                    │   Register Center       │
                    │  POST/GET/DELETE        │
                    │  /v1/agents             │
                    │  POST /v1/broadcast     │
                    │  POST /v1/relay         │
                    └───────────┬─────────────┘
                                │ 目录查询 / 中继
        ┌───────────────────────┼───────────────────────┐
        ▼                       ▼                       ▼
┌───────────────┐       ┌───────────────┐       ┌───────────────┐
│  Agent A API  │       │  Agent B API  │       │  Agent C API  │
│  /v1/messages │◄──────│  （对端执行）  │       │               │
│  /v1/streams  │  HTTP │               │       │               │
└───────┬───────┘       └───────────────┘       └───────────────┘
        │
        │ 模型调用 agent_discover / agent_send_message / …
        ▼
   OpenAI tool 层（agent_peer）
```

| 组件 | 职责 |
|------|------|
| **Register Center** | 默认以内存表维护 **`agent_id` + `base_url` + `discovery_group`（可多组）**，可通过 **`REGISTER_CENTER_STORE_PATH`** 启用 JSON 文件持久化；**不提供**全量 Agent 列表（查询必须带 **`discovery_group`**）；提供 **`/v1/broadcast`**、**`/v1/relay`**。 |
| **主 Agent（调用方）** | 通过 **`agent_*`** 工具发 HTTP；点对点投递时把 **`AgentPeerEnvelope`** JSON 放进 **`POST /v1/messages` 的 `content`**；用 **`connection_id` + `session_id`** 连接对端 **`GET /v1/streams`** 汇总 SSE。 |
| **对端 Agent** | 与普通会话相同：入队 **`message`** → 编排器解析 **`content`**；若触发工具审批，调用方可用 **`agent_peer_approve_tools`** 向对端 **`POST resume`**。 |

---

## 2. Register Center

### 2.1 运行与源码位置

- 启动：**`python run_register_center.py`**（默认 **`0.0.0.0:8010`**，可用 **`REGISTER_CENTER_HOST` / `REGISTER_CENTER_PORT`** 覆盖）。  
- 源码目录：**`register_center/`**（详见该目录 **`README.md`**、**`REFERENCE.md`**）。

### 2.2 核心 HTTP 路由（与实现对齐）

| 方法 | 路径 | 说明 |
|------|------|------|
| **GET** | **`/health`** | 健康检查与当前登记数量。 |
| **GET** | **`/metrics`** | Prometheus 文本指标；包含 Register Center relay/broadcast A2A 指标。 |
| **POST** | **`/v1/agents`** | 登记或 **覆盖** 同一 **`agent_id`**；请求体含 **`base_url`**、**`discovery_group`**（字符串或字符串列表）与可选 **`ttl_seconds`**。 |
| **GET** | **`/v1/agents?discovery_group=...`** | **必填** 分组参数；返回该组内 Agent 列表（精确匹配分组）。 |
| **GET** | **`/v1/agents/{agent_id}?discovery_group=...`** | 调用方分组必须在目标记录的 **`discovery_group`** 中，否则 **404**（防跨组探测）。 |
| **DELETE** | **`/v1/agents/{agent_id}`** | 注销。 |
| **POST** | **`/v1/broadcast`** | 体字段含 **`message`**、**`discovery_group_ids`**、**`source`**；中心按组 **`store.list`** 合并去重后，对每个目标 **`POST {base_url}/v1/messages`**，且 **`content` 为广播正文本身**（**非** `AgentPeerEnvelope`）。每个目标使用中心生成的独立 **`session_id` / `connection_id`**。 |
| **POST** | **`/v1/relay`** | 体字段含 **`target_agent_id`**、**`caller_groups`**（可空）、与下游一致的 **`session_id` / `connection_id` / `request_type` / `source` / `priority`**；**`message`** 中继带 **`content`**，**`resume`** 中继带 **`resume_value`**；校验目标存在且 **`caller_groups` 与目标分组有交集**（若 **`caller_groups` 非空**）后转发到 **`{base_url}/v1/messages`**。 |

**存储（MVP）**：默认登记表为 **进程内内存**；设置 **`REGISTER_CENTER_STORE_PATH`** 后启用单文件 JSON 持久化，写入、删除和 TTL 清理会原子写回文件。多实例 Register Center 仍 **不**共享状态；生产多副本需替换共享存储或前置负载策略。

### 2.3 主 Agent API 自登记（历史，Python API 已移除）

在 **`app/harness/api/app.py`**（归档）的 lifespan 中：若配置了 **`REGISTRY_URL`**、**`AGENT_PUBLIC_BASE_URL`**、非空 **`DISCOVERY_GROUPS`**、非空 **`AGENT_ID`**，则启动时 **`POST {REGISTRY_URL}/v1/agents`**；关闭时注销。**Go Node 当前不自登记**；未来可由 Manage 或专用 sidecar 承担。

---

## 3. A2A：配置项（历史 Agent 侧）

> 以下环境变量服务于 **已移除的 Python Agent API** 与 **`agent_peer`** 工具；Register Center 服务自身仍使用 **`REGISTER_CENTER_*`** 等（见 **`register_center/README.md`**）。

| 环境变量 / 配置字段 | 含义 |
|---------------------|------|
| **`REGISTER_CENTER_STORE_PATH`** | Register Center 服务自身配置；可选 JSON 存储文件路径，未配置时使用进程内内存表。 |
| **`REGISTRY_URL`** | Register Center 根 URL；**`agent_discover` / `agent_broadcast` / `relay` 投递** 依赖（**`direct` 模式下的 `agent_send_message` 发现列表** 也依赖它刷新缓存）。 |
| **`AGENT_ID`** | 本实例在目录中的唯一 ID（常与 **`.runtime/agent/agent_id`** 解析逻辑配合，见 **`Settings`**）。 |
| **`DISCOVERY_GROUPS`** | 本 Agent 所属发现分组（CSV）；**为空** 时跳过自登记，且 **`agent_discover`** 等工具会失败。 |
| **`AGENT_PUBLIC_BASE_URL`** | 登记到目录的 **`base_url`**（供它人调用本 Agent）。 |
| **`AGENT_REGISTRY_TTL_SECONDS`** | 自登记记录 TTL（5–3600 秒）；API 登记成功后按半 TTL 周期向 Register Center 续租。 |
| **`AGENT_PEER_DELIVERY_MODE`** | **`direct`**（默认）：调用方进程根据目录缓存解析 **`base_url`** 后直接 **`POST /v1/messages`**；**`relay`**：**`agent_send_message`** 与 **`agent_peer_approve_tools`** 改为 **`POST {REGISTRY_URL}/v1/relay`**，由中心转发（适合调用方 **无法直连** 对端 **`base_url`** 的网络拓扑）。 |
| **`AGENT_PEER_SHARED_TOKEN`** | 可选共享令牌；配置后 Register Center A2A 路由、Agent 入站 A2A 消息与 A2A SSE 通道需携带 **`x-dagents-a2a-token`**。 |
| **`AGENT_PEER_CACHE_TTL_SECONDS`** | **`agent_id → 记录`** 的进程内列表缓存 TTL；过期后再次 **`GET /v1/agents`** 回源。 |
| **`AGENT_PEER_HTTP_RETRY_ATTEMPTS`** | 只读 A2A HTTP 请求（发现列表、Agent Card）遇到连接错误或 408/429/5xx 时的最大尝试次数，默认 2，范围 1–5；不会自动重试 `/v1/messages` 这类非幂等 POST，避免重复入队。 |
| **`AGENT_PEER_STREAM_TIMEOUT_SECONDS`** | **`agent_send_message` / `agent_peer_approve_tools`** 拉对端 SSE 的超时。 |
| **`AGENT_PEER_BROADCAST_STREAM_TIMEOUT_SECONDS`** | **`agent_broadcast`** 并发拉各目标 SSE 的单目标超时。 |

---

## 4. A2A 工具一览（历史，已归档）

> 源码：**`docs/archive/python-agent-runtime/`** 下对应 **`agent_peer.py`**。Go Node 无等价工具；同进程协作见 [architecture/child-agent-tools.md](./architecture/child-agent-tools.md)。

| 工具名 | 作用 |
|--------|------|
| **`agent_discover`** | 对 **`DISCOVERY_GROUPS`** 中每个分组请求 **`GET /v1/agents`**，合并去重；可选拉取各 Agent **`.well-known/agent-card.json`** 摘要写入 **`agent_card`**；这些只读请求按 **`AGENT_PEER_HTTP_RETRY_ATTEMPTS`** 做有界重试。 |
| **`agent_send_message`** | 构造 **`AgentPeerEnvelope`**（**`intent=delegate`** 等），**`session_id`** 使用 **`peer-{caller}-{target}-{随机}`**，避免与对端用户会话混用；**`direct`** 或对 **`relay`** 投递后，以 **`Last-Event-ID: -1`** 连接对端 **`/v1/streams?connection_id=...`** 回放并汇总 **`assistant` / `reasoning` / `tool_result` / `approval_required` / `error` / `done`**。若出现 **`approval_required`**，返回 **`task.state=requires_input`** 与 **`approvals[]`**。 |
| **`agent_broadcast`** | **`POST /v1/broadcast`**；再根据返回的每个目标的 **`base_url` / `session_id` / `connection_id`** **并发** 拉 SSE，聚合 **`stream_outputs`** 与审批信息。 |
| **`agent_peer_approve_tools`** | 向对端提交 **`request_type=resume`**，**`resume_value`** 与 **`app/schemas/approval.py`** 一致（**`approve` / `reject` / `selection`**）；按 **`AGENT_PEER_DELIVERY_MODE`** 选择直连对端 **`/v1/messages`** 或经 Register Center **`/v1/relay`** 中继，然后继续回放/收集对端 SSE。 |

---

## 5. 协议信封：`AgentPeerEnvelope`（历史）

定义见归档 **`app/schemas/agent_peer.py`**。点对点消息的 **`content`** 为该对象的 **`model_dump()` JSON 文本**，便于对端模型识别 **调用方 `agent_id` / `session_id` / `discovery_groups`**、**`trace_id`**、**`intent`** 与 **`payload`**（如纯文本 **`message`**）。

**注意**：**`intent` 等字段当前主要服务协议一致性与排障**；对端是否解析信封取决于系统提示与实现，并非 Register Center 路由条件。API 入站识别到合法信封时会将正文规范为 `payload.content`，并把原始信封写入 SSE `meta.peer_envelope` 便于 trace。

---

## 6. 典型流程（文字，历史 Python A2A）

### 6.1 点对点（`agent_send_message`）

1. 调用方模型发起工具：**`target_agent_id` + `message`**。  
2. 运行时装配 **`AgentPeerEnvelope`**，生成 **`peer_session_id` / `peer_connection_id`**。  
3. **`direct`**：**`_resolve_target_agent`**（缓存 + **`GET /v1/agents`**）得到 **`base_url`** → **`POST {base_url}/v1/messages`**。  
4. **`relay`**：**`POST {REGISTRY_URL}/v1/relay`**，由中心调用对端 **`/v1/messages`**。  
5. 用响应中的 **`session_id` / `connection_id`**（及已知 **`base_url`**）以 **`Last-Event-ID: -1`** 订阅 **`/v1/streams`**，回放历史后直到 **`done`** 或超时。  
6. 若对端 **`approval_required`**：本侧再调 **`agent_peer_approve_tools`**；审批 **`resume`** 同样按 **`direct` / `relay`** 投递。

### 6.2 广播（`agent_broadcast`）

1. **`POST /v1/broadcast`**：中心向所有命中 **`discovery_group_ids`** 的 Agent 各发一条 **`message`**（正文为工具入参 **`message`**）。  
2. 调用方根据返回列表 **并行** 打开多条 SSE，按 **`agent_peer_broadcast_stream_timeout_seconds`** 控制等待时间。  
3. 若某目标 **`requires_input`**，在返回的 **`approvals[]`** 中按 **`target_session_id`** 继续 **`agent_peer_approve_tools`**。

---

## 7. 与其它文档的关系

| 文档 | 内容 |
|------|------|
| [architecture/overview.md](./architecture/overview.md) | 现网选型：Go Node vs Register Center |
| [architecture/child-agent-tools.md](./architecture/child-agent-tools.md) | 同进程临时子 Agent（非 A2A） |
| [future/a2a-via-manage.md](./future/a2a-via-manage.md) | 远期经 Manage 的 A2A |
| [archive/python-agent-runtime/](./archive/python-agent-runtime/) | Python Agent **`/v1/messages` / turn loop** |
| [agent-input-output.md](./agent-input-output.md) | 跳转桩 → 归档 |
| [agent-turn-loop.md](./agent-turn-loop.md) | 跳转桩 → 归档 |
| [api-reference.md](./api-reference.md) | 跳转桩 → 归档 |
| **`register_center/README.md`** | 中心侧接口速查 |

---

**说明**：**Register Center HTTP 契约**以 **`register_center/`** 与 **`CHANGELOG.md`** 为准；**Agent 侧 A2A 工具**以归档 Python 运行时为准；现网 Agent 运行时以 **Go Node**（[agent-node-api.md](./architecture/agent-node-api.md)）为准。
