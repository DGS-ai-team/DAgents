# Manage 通信逻辑全量参考

> **设计原则**（[manage-architecture.md](./design/manage-architecture.md)、[a2a-via-manage.md](./future/a2a-via-manage.md)）：  
> **Node 仅出站连 Manage**；**禁止 Node-to-Node**；**Client 不连 Manage**。  
> 文档描述 **v0.3.x 现网实现**；与方案差异见 §8。

---

## 1. 三组件与谁连谁

```text
┌─────────────┐         ┌──────────────────────────────────┐         ┌─────────────┐
│   Client    │  HTTP   │           Manage (:8020)          │         │ Agent Node  │
│ (TUI/REPL)  │────────►│ Registry │ A2A Store │ Admin     │◄────────│   (Go)      │
└──────▲──────┘  本地   │ Console  │ Audit                 │  出站   └──────▲──────┘
       │                 └──────────────────────────────────┘                │
       └─────────────────────────────────────────────────────────────────────┘
                    Client 只连本机 Node；Node 之间不经 HTTP 直连
```

| 参与方 | 连 Manage？ | 连 Node？ | 说明 |
|--------|-------------|-----------|------|
| **Client（TUI/REPL）** | 否 | 是（`local.endpoint`） | 会话、SSE、HITL 全在 Node |
| **Agent Node** | 是（出站） | 是（入站：仅 Client） | 注册/心跳/A2A 均 Node→Manage |
| **Console / 运维脚本** | 是 | 否 | 浏览器只打 Manage |
| **Agent Node ↔ Agent Node** | 经 Manage 信箱 | **禁止直连** | discover 不返回 peer URL |

**「单向」**：协作数据面均为 Node **主动出站**；Manage **不 push Task、不代理 Node session**。

---

## 2. 全量 HTTP 端点矩阵

### 2.1 系统 / Platform

| 方法 | 路径 | 调用方 → Manage | 说明 |
|------|------|-----------------|------|
| GET | `/health` | 运维 / Console / 脚本 | 健康 + agent 计数 |
| GET | `/metrics` | Prometheus | 指标 |
| GET | `/v1/admin/audit` | Console / admin | 内存审计列表 |
| GET | `/`、`/console` | 浏览器 | 重定向 Console |

### 2.2 Registry（`/v1/registry/...`）

| 方法 | 路径 | 发起方 | 方向 | 说明 |
|------|------|--------|------|------|
| POST | `/v1/registry/agents` | **Node** | Node → Manage | 注册 / upsert |
| POST | `/v1/registry/agents/{id}/heartbeat` | **Node** | Node → Manage | 周期心跳 |
| POST | `/v1/registry/agents/{id}/deregister` | **Node** | Node → Manage | 优雅停机 |
| GET | `/v1/registry/agents/discover` | **Node** | Node → Manage | A2A `agent_discover` |
| GET | `/v1/registry/agents/{id}` | Console / 运维 / Node | → Manage | 详情（**含 `base_url`**，运维用） |
| GET | `/v1/registry/agents` | Console / 运维 | → Manage | 分页列表 |
| PATCH | `/v1/registry/agents/{id}/groups` | **Console / 脚本** | → Manage | 分配 `discovery_group`（**Node 不传**） |
| DELETE | `/v1/registry/agents/{id}` | 运维 admin | → Manage | 强制删除 |

鉴权 Header（可选）：`x-dagents-agent-id`、`x-dagents-a2a-token`（见 `manage/platform/auth.py`）。

### 2.3 A2A Task（`/v1/a2a/...`）

| 方法 | 路径 | 发起方 | 方向 | 说明 |
|------|------|--------|------|------|
| POST | `/v1/a2a/tasks` | **Caller Node** | Node → Manage | 创建 Task（`agent_invoke`） |
| GET | `/v1/a2a/inbox?wait=` | **Callee Node** | Node → Manage | long poll 收 Task |
| POST | `/v1/a2a/tasks/{id}/ack` | **Callee Node** | Node → Manage | 标记 processing |
| POST | `/v1/a2a/tasks/{id}/reply` | **Callee Node** | Node → Manage | 提交结果 / `requires_input` |
| GET | `/v1/a2a/tasks/{id}` | Caller / Callee Node | Node → Manage | 轮询状态 |
| POST | `/v1/a2a/tasks/{id}/caller_resume` | **Caller Node** | Node → Manage | HITL 中继：用户 resume |
| GET | `/v1/a2a/tasks/{id}/caller_input?wait=` | **Callee Node** | Node → Manage | HITL 中继：取 resume 载荷 |

**未实现**：`POST /v1/a2a/broadcast`、`/v1/blobs/*`。

### 2.4 Admin 观测（`/v1/admin/...`）

| 方法 | 路径 | 发起方 | 方向 | 说明 |
|------|------|--------|------|------|
| GET | `/v1/admin/a2a/tasks` | Console | Browser → Manage | 只读 Task 列表 |

> **已禁用**：`/v1/admin/nodes/{id}/sessions` 与 `.../context` 代理已移除。

### 2.5 Go Node 本地 API（与 Manage 相关部分）

| 方法 | 路径 | 调用方 | 说明 |
|------|------|--------|------|
| GET | `/v1/agent/info` | Client | 含 `manage_registered` |
| GET | `/v1/sessions`、`/v1/sessions/{id}/context` | Client | 本地运维 / TUI（**不经 Manage**） |
| POST `/v1/sessions/.../messages`、SSE 等 | Client | 本地 turn，**不经 Manage** |

---

## 3. 注册与心跳（Node → Manage）

```text
Node 启动 (manage.enabled=true)
  │
  ├─ server.go: NewRegistrar + Start(ctx)
  │
  ├─ POST /v1/registry/agents
  │     Body: agent_id, base_url, capabilities, tools, expose_to_peers,
  │           card (agent-card.json), metadata, version ...
  │     Header: x-dagents-agent-id, x-dagents-a2a-token (optional)
  │     ← heartbeat_interval_seconds（Manage 建议间隔，通常 30s）
  │
  ├─ 后台 ticker: POST .../heartbeat
  │     Body: ttl_seconds, version, tools, expose_to_peers
  │     404 → 重新 register
  │
  └─ 停机 Stop(): POST .../deregister (reason=shutdown)
```

**关键字段语义**

| 字段 | 谁写入 | 用途 |
|------|--------|------|
| `agent_id` | Node 配置 | 全局身份 |
| `base_url` | Node `manage.registration.base_url`（空则 `local.endpoint`） | Console 展示与人工跳转；**A2A 路由不用** |
| `expose_to_peers` | Node 配置 | 是否可作 A2A **被调方** |
| `discovery_group` | **Manage**（PATCH groups） | discover / invoke 分组交集校验；**Node 注册不传** |
| `card` | Node `agent-card.json` + config 合并 | Console 展示 name/description |

**源码**：`node/internal/manage/registrar.go`、`manage/registry/store.py`。

---

## 4. A2A 协作（Node ↔ Node，信令经 Manage）

### 4.1 标准 invoke（无 HITL）

```text
Node B (caller)                    Manage                         Node A (callee)
     │                                │                                  │
     │ GET /v1/registry/agents/discover                                  │
     │───────────────────────────────►│                                  │
     │◄ agents[]（无 base_url）       │                                  │
     │                                │                                  │
     │ POST /v1/a2a/tasks             │                                  │
     │  from=B, to=A, kind=invoke     │                                  │
     │───────────────────────────────►│ 校验 online + expose + groups   │
     │◄ { task_id, status: queued }   │ 写入 A 的 inbox (queued)         │
     │                                │                                  │
     │                                │ GET /v1/a2a/inbox?wait=25        │
     │                                │◄─────────────────────────────────│
     │                                │────────────────► tasks[]         │
     │                                │                                  │ ack → turn → reply
     │                                │ POST .../reply (completed)       │
     │                                │◄─────────────────────────────────│
     │ GET /v1/a2a/tasks/{id} (poll)  │                                  │
     │───────────────────────────────►│                                  │
     │◄ { status: completed, result_text }                              │
```

**禁止路径**：`Node B ──HTTP──► Node A`（discover 响应 `AgentDiscoverRecord` **不含** `base_url`）。

**创建 Task 校验**（`registry/store.can_a2a_invoke`）：

1. target **online** 且 `expose_to_peers=true`
2. caller / target 均有非空 `discovery_group` 且 **集合有交集**

**工具链**

| 角色 | 组件 |
|------|------|
| Caller | `tool_a2a.go` → `a2aclient.Client` |
| Callee | `inbox_poller.go` → `compliance_executor.go` → `task_replier.go` → `session/a2a_inbox.go` |

### 4.2 HITL 中继（跨 Agent 需用户输入）

当 callee turn 需要 HITL（`ask_user_information` / 审批等）且无法在对端 TUI 完成时：

```text
1. Callee: POST .../reply  status=requires_input
2. Task → awaiting_caller
3. Caller: WaitForInvokeResult 识别 → A2ACallerHITLBridge 推 SSE 到 **caller 本地 TUI**
4. 用户在 caller TUI resume
5. Caller: POST .../caller_resume
6. Callee: GET .../caller_input?wait=  → EnqueueResume → 续跑 turn
7. Callee: POST .../reply  status=completed
8. Caller: GET task → 得到 result_text
```

**要点**：HITL UI 在 **caller 侧 Client↔Node** 本地完成；Manage 只做 **Task 状态与 resume 载荷中继**。

### 4.3 Task 状态机

```text
queued → delivered → processing → completed | failed | expired
                      ↓
               awaiting_caller → processing → completed
```

Manage 侧：`manage/a2a/store.py`（poll 时 mark delivered；后台 TTL sweep）。

---

## 5. Client / TUI 通信（不经 Manage）

```text
用户 ──► Client (dagents tui/chat) ──HTTP/SSE──► 本机 Node
                                              │
                                              ├─ 本地 turn / 工具 / HITL
                                              │
                                              └─ agent_invoke ──► Manage ──► 对端 Node inbox
```

- Client **不**配置 `manage.url`。
- `manage_registered` 来自 Node `GET /v1/agent/info` 或 health 探针。
- 子 Agent（`create_temporary_agent`）仅在 **同一 Node 进程内**，不经 Manage。

---

## 6. Console 通信（Browser → Manage）

`manage/console/frontend/src/api.js` 典型调用：

| 用途 | API |
|------|-----|
| 健康 / 列表 | `/health`、`GET /v1/registry/agents` |
| 分组 | `PATCH /v1/registry/agents/{id}/groups` |
| A2A 观测 | `GET /v1/admin/a2a/tasks` |
| 审计 | `GET /v1/admin/audit` |

Console **从不直连 Node**（Agent 详情抽屉中的 `base_url` 链接供人工打开 Node，非 Manage 代理）。

---

## 7. 与「Manage–Node 单向」设计对照

### 7.1 符合设计的部分

| 项 | 说明 |
|----|------|
| A2A 数据面 | 全部经 Manage inbox；无 Node-to-Node HTTP |
| discover | 不返回 peer `base_url` / endpoint |
| 注册 / 心跳 / deregister | Node 出站 |
| Inbox 收信 | Callee **long poll** Manage（非 Manage push） |
| Client 边界 | 不连 Manage |
| 旧 RC relay/broadcast | 已删除；`MANAGE_LEGACY_DIRECT_RELAY` 无路由实现 |

### 7.2 其余说明

| 项 | 说明 |
|----|------|
| **`base_url`** | Node 注册上报，Console 展示与人工跳转；**A2A 路由不用**。Admin session 代理已禁用，Manage 不再因 Console 出站访问 Node。 |
| **Node 本地 HTTP 入站** | 仅 **Client** 会话需要 Node 监听端口。 |
| **方案未落地** | 设计中的 Node→Manage **审计 ingest**、**Skills 同步**、**Blob API** — 现无 Go 出站实现。 |
| **Inbox handler** | 仅 `metadata.role=compliance` 等注册 handler 会跑 turn；其它 role 收 Task 可能仅日志。 |
| **开放模式鉴权** | 未配置 token 时 Console 开放浏览（MVP）。 |

**结论**：现网 **Manage ↔ Node 协作路径** 为 Node 出站（注册、心跳、A2A inbox/reply）；Manage **不**再主动 HTTP 访问 Node。

---

## 8. 配置速查（Node 侧）

```yaml
manage:
  enabled: true
  url: http://127.0.0.1:8020
  registration:
    base_url: http://<本机可达地址>:18765   # Console 展示 / 人工跳转；留空则用 local.endpoint
    interval_seconds: 30
    ttl_seconds: 60
    # discovery_group 由 Manage Console PATCH，不在此配置
  a2a:
    enabled: true
    inbox_poll_seconds: 5
  node_token: ""   # 可选，对应 x-dagents-a2a-token
```

---

## 9. 源码索引

| 路径 | 用途 |
|------|------|
| `manage/manage_app.py` | FastAPI 装配 |
| `manage/registry/routes.py` | Registry HTTP |
| `manage/registry/store.py` | 存储、discover、A2A 分组校验 |
| `manage/a2a/routes.py` | A2A HTTP |
| `manage/a2a/store.py` | Inbox、状态机、long poll |
| `manage/admin/routes.py` | Admin A2A 列表（无 Node 代理） |
| `manage/platform/auth.py` | 鉴权 |
| `node/internal/manage/registrar.go` | 注册 / 心跳 |
| `node/internal/manage/inbox_poller.go` | Inbox long poll |
| `node/internal/manage/compliance_executor.go` | Inbox turn |
| `node/internal/manage/task_replier.go` | ack / reply / caller_input |
| `node/internal/a2aclient/client.go` | discover / create / poll / resume |
| `node/internal/tools/tool_a2a.go` | `agent_discover` / `agent_invoke` |
| `node/internal/api/server.go` | 启动 registrar / poller |
| `cases/a2a-manage-docker/` | 双 Node 联调案例 |

**相关设计文档**：[manage-architecture.md](./design/manage-architecture.md)、[a2a-via-manage.md](./future/a2a-via-manage.md)、[three-component-model.md](./design/three-component-model.md)。
