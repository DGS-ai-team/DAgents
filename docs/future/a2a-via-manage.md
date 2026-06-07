# A2A 经 Manage 中继（非子 Agent）

本文定义 **主 Agent 之间** 的 A2A 通信规则：**一律经 Manage**，**禁止 Agent Node 直连其他 Agent Node**。

**子 Agent**（临时、Node 内创建）不参与本节；其通信仅在 **同一 Agent Node 进程内** 完成（见 [temporary-child-agents.md](./temporary-child-agents.md) 修订版）。

---

## 1. 设计原则

| 原则 | 说明 |
|------|------|
| **无 Node-to-Node HTTP** | 调用方 Node 不得请求 callee 的 `endpoint` |
| **Manage 为唯一信令与投递中枢** | 发现、发消息、收消息、查状态、回回复均走 Manage API |
| **Node 仅出站连 Manage** | 与注册/心跳/审计一致；callee 通过 **轮询 inbox** 收消息，无需对 peer 开放入站 |
| **`expose_to_peers` 控制可达性** | 仅影响 Manage 是否允许 **向该 Agent 投递** A2A；仍须注册 Manage |
| **审计** | 发送/接收/回复各阶段由相关 Node **上报 Manage**（`a2a_outbound` / `a2a_inbound`） |

---

## 2. 总体流程

```text
Agent A (Node)                         Manage                         Agent B (Node)
      │                                   │                                  │
      │  GET /v1/agents/discover          │                                  │
      │──────────────────────────────────►│                                  │
      │◄──────────────────────────────────│  agents[]（无 peer endpoint）     │
      │                                   │                                  │
      │  POST /v1/a2a/messages            │                                  │
      │  { from:A, to:B, content }        │                                  │
      │──────────────────────────────────►│  校验 expose、online、写入 B inbox │
      │◄──────────────────────────────────│  { message_id, status: queued }  │
      │                                   │                                  │
      │                                   │  GET /v1/a2a/inbox (B 轮询)       │
      │                                   │◄─────────────────────────────────│
      │                                   │─────────────────────────────────►│ pending[]
      │                                   │                                  │ 本地 turn loop
      │                                   │  POST .../messages/{id}/reply    │
      │                                   │◄─────────────────────────────────│
      │  GET /v1/a2a/messages/{id}        │                                  │
      │──────────────────────────────────►│                                  │
      │◄──────────────────────────────────│  { status: completed, result }   │
```

**禁止的路径**（架构违规）：

```text
Agent A ──HTTP──► Agent B   ❌
```

---

## 3. 与 `expose_to_peers` 的关系

| `expose_to_peers` | Manage discover | 作为 A2A **目标** 接收消息 |
|-------------------|-----------------|---------------------------|
| `true` | 出现在 peer 目录 | 允许（online 时） |
| `false` | 运维可见；peer 目录不可见 | Manage 拒绝投递，`403 target_not_exposed` |

作为 **调用方** 发送 A2A 不受 `expose_to_peers` 限制（须已注册且 token 有效）。

---

## 4. Manage API 摘要

完整字段见 [manage-api-sketch.md](./manage-api-sketch.md) §4。

| 方法 | 路径 | 调用方 | 说明 |
|------|------|--------|------|
| GET | `/v1/agents/discover` | Node A | 返回可协作 Agent 列表（**不含**用于直连的 routing endpoint） |
| POST | `/v1/a2a/messages` | Node A | 创建跨 Agent 消息，进入目标 inbox |
| GET | `/v1/a2a/inbox` | Node B | 拉取待处理消息（建议心跳周期内调用） |
| POST | `/v1/a2a/messages/{id}/ack` | Node B | 可选：已接收、开始处理 |
| POST | `/v1/a2a/messages/{id}/reply` | Node B | 提交执行结果 |
| GET | `/v1/a2a/messages/{id}` | Node A / B | 查询状态与结果（幂等） |

可选：`POST /v1/a2a/broadcast`（Manage 扇出到多 Agent inbox，Phase 2）。

---

## 5. Agent Node 侧实现要点

### 5.1 工具层

| 工具 | 行为 |
|------|------|
| `agent_discover` | `GET Manage /v1/agents/discover` |
| `agent_send_message` | `POST Manage /v1/a2a/messages`；轮询 `GET .../messages/{id}` 直至终态 |
| `agent_broadcast` | `POST Manage /v1/a2a/broadcast`（若启用） |

**无** `/v1/a2a/*` **入站 HTTP** 供其他 Node 调用。

### 5.2 Inbox 轮询

- 推荐：独立 goroutine 每 `inbox_poll_seconds`（默认与 heartbeat 对齐）调用 `GET /v1/a2a/inbox`。
- 收到消息后映射为 **A2A session** 入队，走本地 turn loop（与 Client 消息共用队列或独立 `a2a` 优先级，实现细节见 `agent-node-internals.md`）。

### 5.3 注册字段 `endpoint`

- **不再**用于 A2A peer 路由。
- 可选保留供 **Manage 运维探活** 或 Dashboard 展示（Node 仅 bind `127.0.0.1` 时可省略或填 localhost）。

---

## 6. 消息状态机

```text
queued → delivered → processing → completed | failed | expired
```

| 状态 | 含义 |
|------|------|
| `queued` | Manage 已接受，等待 callee 拉 inbox |
| `delivered` | callee 已 ack 或已拉取 |
| `processing` | callee turn loop 运行中 |
| `completed` | reply 已写入，caller 可读取 |
| `failed` | callee 或 Manage 拒绝 |
| `expired` | TTL 超时未处理 |

---

## 7. 与子 Agent 的边界

| 类型 | 通信路径 | 注册 Manage |
|------|----------|-------------|
| 主 Agent | Manage A2A | 必须 |
| 临时子 Agent | Node 内部 API / 内存队列 | **不**独立注册；审计带 `parent_session_id` |

子 Agent **不得**通过 Manage 对外发送 A2A（除非未来显式扩展且仍经 Manage）。

---

## 8. 迁移说明（相对旧 Register Center）

现有 `register_center` 的 **relay HTTP 直连 Agent URL** 模式废弃，改为：

- RC/Manage **只存 inbox**，不 `httpx.post(callee.base_url, ...)` 调 peer Node。
- 与 [manage-api-sketch.md](./manage-api-sketch.md) §6 映射表一致。

---

## 9. Phase 1 最小集

| 优先级 | 能力 |
|--------|------|
| P0 | discover + send + inbox + reply + get status |
| P1 | ack、TTL、并发 inbox 上限 |
| P2 | broadcast、长任务 progress 经 Manage 转发 |
