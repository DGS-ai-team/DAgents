# 三组件模型：Client / Agent Node / Manage

本文档描述 v2 **重设计**后的目标形态，取代旧文档中「Python Backend = Brain + Control、Go Proxy = 出站 Body」的假设。

## 1. 总览

系统拆为三个可独立部署的组件，均以外部 API 协作：

```text
┌─────────────┐     本地 API      ┌──────────────────┐
│   Client    │ ────────────────► │   Agent Node     │
│  (Go TUI)   │ ◄──────────────── │  (Go，Agent 本体)  │
└─────────────┘   SSE / HTTP      └────────┬─────────┘
                                             │
                        注册 / 心跳 / 审计 / A2A  │
                        （仅出站，不直连 peer）   ▼
                                    ┌──────────────────┐
                                    │     Manage       │
                                    │ (Python，控制面)  │
                                    └──────────────────┘
                                             ▲
                                             │ 同上（A2A 经 Manage 中继）
                                    ┌────────┴─────────┐
                                    │ 其他 Agent Node   │
                                    └──────────────────┘
```

| 组件 | 实现语言 | 职责摘要 |
|------|----------|----------|
| **Client** | Go | 人机交互 TUI；**仅**连接本机 Agent Node |
| **Agent Node** | Go | LLM、turn loop、工具执行、会话；对外 API；向 Manage 注册与上报 |
| **Manage** | Python（保留现有仓库中的管理面能力） | 注册中心、**A2A 中继**、发现、审计存储、运维视图；**不参与** Client 会话路径 |

**同机多实例**：一台宿主机可运行多个 Agent Node，**每个进程绑定独立端口，且对应唯一 `agent_id`**。Client 与某一 Node **同包发布**，读取同一份本地配置（host/port）连接该 Node。

---

## 2. Agent Node：思考与执行

### 2.1 职责

- **LLM 调用**与 **Agent turn loop**（ReAct、多步工具、上下文压缩等）。
- **工具选择与执行**（文件、shell、skills、触发器、A2A 等）均在 Node 内完成。
- 对外提供 **HTTP API**（及 SSE 流式事件），**仅供本机 Client** 使用。
- 向 **Manage** 注册、心跳、**主动上报审计**；**A2A 仅经 Manage**（见 [a2a-via-manage.md](./a2a-via-manage.md)）。

### 2.2 端口与 `agent_id`

| 规则 | 说明 |
|------|------|
| **一端口 = 一 `agent_id`** | 每个 Agent Node 进程监听一个端口，身份即该 `agent_id` |
| **临时子 Agent** | 由主 Agent 在 Node 内创建；与主 Agent **共用** Node 资源（进程、端口、FS 根等）；**不单独暴露**端口或对外 API |
| **对其他 Agent 可见性** | 由 **配置** 决定是否在 Manage 发现结果中标记为「可被其他 Agent 调用」；**无论是否暴露，都必须注册到 Manage** |

子 Agent 的会话、工具权限、TTL 等由 Node 内策略约束（详见后续 `temporary-child-agents.md` 修订版）。

### 2.3 与其他 Agent 通信（非子 Agent）

- **禁止 Agent Node 直连**：不得 HTTP 调用其他 Node 的地址。
- 调用方通过 **Manage** `discover` 获取可协作的 `agent_id` 列表（**不含** peer 路由 endpoint）。
- 发送 / 收信 / 查状态 / 回复均走 **Manage A2A API**；被调方 Node 通过 **轮询 inbox** 收消息并本地 turn loop 处理。
- 仅 **`expose_to_peers=true`** 的 Agent 可作为 A2A **目标**；不可暴露的 Agent 仍注册 Manage 供运维查看。

详见 [a2a-via-manage.md](./a2a-via-manage.md)。

---

## 3. Client：只连本地 Agent

| 规则 | 说明 |
|------|------|
| **连接范围** | Client **永远只**连接 **本机** Agent Node（`127.0.0.1` 或本机 bind 地址） |
| **与 Manage** | Client **不**与 Manage 通信（无注册、无发现、无审计查询） |
| **发布形态** | Client 与目标 Agent Node **绑定发布**；共享配置文件中的 `host`、`port`、`agent_id`（或等价 local endpoint） |
| **能力** | 发消息、收 SSE、审批/HITL resume、会话管理（均经 Node API） |

运维人员查看全局 Agent 列表、审计、健康状态时使用 **Manage 控制台/API**，而非 Client。

---

## 4. Manage：注册、发现、审计、运维

### 4.1 职责

- **Agent 注册与心跳**：Node **启动时**首次注册；之后 **定时心跳** 更新 presence、capabilities、版本、host/port 等。
- **A2A 中继**：非子 Agent 间消息 **仅经 Manage** 投递；Node 不直连 peer。
- **发现**：供 Node A2A 工具与运维查询；尊重 `expose_to_peers`。
- **审计**：Node **主动上报** 执行、策略、A2A 等审计事件；Manage 持久化与检索。
- **运维视图**：Agent 列表、运行状态、历史审计（给人看，不给 Client 用）。

### 4.2 非职责

- 不运行 LLM turn loop。
- 不代理 Client 消息、不持有 session 推理态（会话在 Agent Node）。
- 不向 Agent Node 下发工具 execute（旧 control channel **废弃**）。
- **不**代表 Manage「完全不碰消息」：A2A 信令与 inbox **由 Manage 持久化与转发**（见 [a2a-via-manage.md](./a2a-via-manage.md)）。

### 4.3 Python 代码库定位

**保留并实现 Manage 相关功能**；原 `app/core/main_agent`、`app/harness/service` 中面向 Brain/会话/工具执行的逻辑 **迁移到 Go Agent Node**，不在 Python 中保留双份。

Manage 侧可复用/演进的方向包括：

- Register Center 式 HTTP API（注册、发现、groups）
- 审计 ingest 与查询 API
- 策略/审批配置的集中存储（若 Phase 1 仍由 Manage 下发给 Node）
- 运维只读 Dashboard 所需 API

具体模块切分见 [migration-from-v1.md](./migration-from-v1.md) 后续修订。

---

## 5. 生命周期与数据流

### 5.1 Node 启动

```text
1. 读取本地配置（agent_id、listen_port、manage_url、expose_to_peers、…）
2. POST Manage → register（agent_id、capabilities、manifest 摘要；`endpoint` 可选，仅运维）
3. 启动 HTTP/SSE 服务（**仅本机 Client**），等待本地连接
4. 后台：heartbeat → Manage；inbox 轮询 ← Manage A2A
5. 后台：审计 → Manage
```

### 5.2 Client 会话

```text
Client → POST /sessions、/messages（本地 Node）
Node   → turn loop → 工具本地执行 → SSE 推送事件 → Client
```

### 5.3 A2A（跨 Agent，非子 Agent）

```text
Agent A → Manage：discover / POST /v1/a2a/tasks
Manage  → 写入 Agent B inbox
Agent B → Manage：GET /v1/a2a/inbox（long poll）→ 本地 turn loop
Agent B → Manage：POST /v1/a2a/tasks/{id}/reply
Agent A → Manage：GET /v1/a2a/tasks/{id} 取结果
审计：A、B 各自上报 Manage
```

**禁止** `Agent A → Agent B` 直连。详见 [a2a-via-manage.md](./a2a-via-manage.md)。

---

## 6. 与旧 v2 概念对照

| 旧概念 | 新归属 |
|--------|--------|
| Python Backend Brain | **Agent Node（Go）** |
| Python Backend Control Plane（session 执行路由） | **Agent Node** 内会话与调度；Manage 仅元数据 |
| Go Proxy 出站 Body | **取消**；宿主机能力即 Node 本地 executor |
| Register Center | **Manage** 发现 API |
| Python TUI / textual | **Client（Go TUI）** |
| `connection_id` + 多 Backend SSE | 简化为 **单 Node 本地 SSE**；多 Backend 议题延后 |
| control channel WebSocket | **废弃**（Node 不再连 Manage/Backend 收 execute） |

---

## 7. 相关文档（已落地草图）

- [agent-node-api.md](../architecture/agent-node-api.md) — Agent Node HTTP/SSE API  
- [manage-api-sketch.md](./manage-api-sketch.md) — Manage 注册/发现/审计 API  
- [a2a-via-manage.md](./a2a-via-manage.md) — A2A 经 Manage 中继（禁止 Node 直连）
- [documentation-plan.md](./documentation-plan.md) — 文档规划与阅读顺序

## 8. 架构决策记录（ADR 摘要）

1. **LLM / turn loop 在 Agent Node（Go）** — Node 负责思考与工具调用。
2. **一端口一 `agent_id`** — 临时子 Agent 不暴露；可配置 peer 可见性；**必须**注册 Manage。
3. **Manage：注册 + 心跳 + 审计 ingest** — Client 不访问 Manage。
4. **Client 只连本地 Node** — 与 Node 同发布、同配置。
5. **Python 仓库仅保留 Manage** — Brain/执行迁移至 Go Node。
6. **A2A 经 Manage 中继** — 非子 Agent **禁止 Node 直连**；inbox 轮询收信。
