# A2A 经 Manage 中继（Task 模型）

本文定义 **主 Agent 之间** 的 A2A 协作规则：**一律经 Manage**，**禁止 Agent Node 直连其他 Agent Node**。

**不做** 直连 peer HTTP；统一 **Task → Inbox → Reply** 语义。

**子 Agent**（临时、Node 内创建）不参与本节；其通信仅在 **同一 Agent Node 进程内** 完成（见 [child-agent-tools.md](../architecture/child-agent-tools.md)）。

---

## 1. 设计原则

| 原则 | 说明 |
|------|------|
| **无 Node-to-Node HTTP** | 调用方 Node 不得请求 callee 的 `endpoint` |
| **Manage 为唯一信令中枢** | 发现、投递 Task、拉 Inbox、查状态、回 Reply 均走 Manage API |
| **Node 仅出站连 Manage** | 与注册/心跳一致；callee 通过 **long poll inbox** 收 Task |
| **`expose_to_peers` 控制可达性** | 仅影响 Manage 是否允许 **向该 Agent 投递** Task |
| **审计** | create / inbox.deliver / ack / reply 写入 Manage audit |

---

## 2. 总体流程

```text
Agent A (Node)                         Manage                         Agent B (Node)
      │                                   │                                  │
      │  GET /v1/registry/agents/discover │                                  │
      │──────────────────────────────────►│                                  │
      │◄──────────────────────────────────│  agents[]（无 peer endpoint）     │
      │                                   │                                  │
      │  POST /v1/a2a/tasks               │                                  │
      │  { from:A, to:B, content }        │                                  │
      │──────────────────────────────────►│  校验 expose、online、写入 B inbox │
      │◄──────────────────────────────────│  { task_id, status: queued }     │
      │                                   │                                  │
      │                                   │  GET /v1/a2a/inbox?wait=25s      │
      │                                   │◄─────────────────────────────────│
      │                                   │─────────────────────────────────►│ tasks[]
      │                                   │                                  │ 本地 turn loop
      │                                   │  POST .../tasks/{id}/reply       │
      │                                   │◄─────────────────────────────────│
      │  GET /v1/a2a/tasks/{id}           │                                  │
      │──────────────────────────────────►│                                  │
      │◄──────────────────────────────────│  { status: completed, result }   │
```

**禁止的路径**（架构违规）：

```text
Agent A ──HTTP──► Agent B   ❌
```

---

## 3. 与 `expose_to_peers` 的关系

| `expose_to_peers` | Manage discover | 作为 A2A **目标** 接收 Task |
|-------------------|-----------------|---------------------------|
| `true` | 出现在 peer 目录 | 允许（online 时） |
| `false` | 运维可见；peer 目录不可见 | Manage 拒绝投递，`403 target_not_exposed` |

作为 **调用方** 创建 Task 不受 `expose_to_peers` 限制（须已注册）。

---

## 4. Manage API

完整字段见 [manage-architecture.md](../design/manage-architecture.md) §3.2。

| 方法 | 路径 | 调用方 | 说明 |
|------|------|--------|------|
| POST | `/v1/a2a/tasks` | Node A | 创建 Task，进入目标 inbox |
| GET | `/v1/a2a/inbox` | Node B | long poll 拉取 pending（`?wait=` 最长 60s） |
| POST | `/v1/a2a/tasks/{id}/ack` | Node B | 可选：标记 processing |
| POST | `/v1/a2a/tasks/{id}/reply` | Node B | 提交执行结果 |
| GET | `/v1/a2a/tasks/{id}` | Node A / B | 查询状态与结果 |

可选（Phase 2）：`POST /v1/a2a/broadcast` — 扇出 Task 到多 inbox。

**Task 体（摘要）**

```json
{
  "from_agent_id": "A",
  "to_agent_id": "B",
  "kind": "invoke",
  "content": "简短指令",
  "blob_ids": [],
  "caller_session_id": "sess-a",
  "idempotency_key": "optional-key",
  "ttl_seconds": 3600,
  "trace_id": "..."
}
```

- **`kind`**：`invoke`（需 reply）| `notify`（可仅 ack）
- **大 payload**：先 `POST /v1/blobs`，Task 引用 `blob_ids`

---

## 5. Agent Node 侧实现要点

### 5.0 HITL 中继（invoke + awaiting_caller）

callee inbox turn 若触发 HITL，**不** 以空 `completed` 结束；改为：

1. callee `POST .../reply`，`status=requires_input`，Task 进入 **`awaiting_caller`**
2. `result_text` 为 JSON（含 `hitl_kind`、`event_type`、`event_data`、`callee_session_id`、`caller_session_id`）
3. caller `agent_invoke` 轮询识别后，在 **caller_session_id** 对应 TUI 展示 HITL；`POST .../caller_notify` → **`caller_notified`**
4. 用户审批后 caller `POST .../caller_resume` → **`caller_responded`**（`pending_caller_resume`）；callee `GET .../caller_input` 取 `resume_value` → **`processing`**
5. turn 续跑后最终 `reply(completed, result_text)`

| 方法 | 路径 | 调用方 |
|------|------|--------|
| POST | `/v1/a2a/tasks/{id}/caller_notify` | caller（node-b）已收到 HITL |
| POST | `/v1/a2a/tasks/{id}/caller_resume` | caller（node-b）用户 resume |
| GET | `/v1/a2a/tasks/{id}/caller_input?wait=` | callee（node-a）拉 resume |

**Caller TUI（v0.3.9+）**：`approval_required` / `user_information_required` 带 `a2a_relay=true` 时，在 **调用方** session 展示 HITL（含 `from <对端 Agent>` 工具行标识）；用户 resume 后经 `caller_resume` 回 Manage，callee `RunInboxTurn(resume)` 续跑。

**`requires_input` JSON 摘要**（`result_text`）：

```json
{
  "hitl_kind": "tool_approval",
  "task_id": "...",
  "callee_session_id": "a2a-...",
  "caller_session_id": "sess-b",
  "callee_agent_id": "node-a",
  "callee_agent_name": "合规助手",
  "event_type": "approval_required",
  "event_data": { "approval_id": "...", "approval_args": { "tool_calls": [] } }
}
```

`hitl_kind` 亦可为 `user_information`；`event_data` 结构与本地 HITL SSE 一致。

### 5.1 工具层

| 工具 | 行为 |
|------|------|
| `agent_discover` | `GET /v1/registry/agents/discover` |
| `agent_invoke` | `POST /v1/a2a/tasks` + 轮询 `GET /v1/a2a/tasks/{id}` |
| `agent_notify` | `POST /v1/a2a/tasks`（`kind=notify`） |

**无** `/v1/a2a/*` **入站 HTTP** 供其他 Node 调用。

### 5.2 Inbox long poll

- **`node/internal/manage/inbox_poller.go`**：独立 goroutine，`GET /v1/a2a/inbox?wait=25`；连续失败降级为短 poll（`manage.a2a.inbox_poll_seconds`）。
- 收到 Task 后映射为 **A2A session** 入队（handler 待接 session 层）。
- 配置：`manage.a2a.enabled`（默认随 `manage.enabled` 开启）、`inbox_wait_seconds`、`inbox_poll_seconds`。

**Manage 侧 inbox 效率（P0 优化项）**

| 机制 | 说明 |
|------|------|
| **per-agent pending 索引** | 内存 `dict[agent_id → task_id[]]`，poll 不再全表扫描 |
| **long poll** | `threading.Condition`，空闲时挂起，`wait` 最长 60s |
| **inbox 响应瘦身** | `content` 默认最多 4096 字符，超出截断并设 `content_truncated=true`；大正文走 `blob_ids` |
| **后台 TTL sweep** | 默认每 30s 扫描过期 Task；poll/create 不再每次扫全表 |
| **SQLite UPSERT** | 按 `task_id` 增量写入，非全表重写 |

Manage 环境变量：`MANAGE_A2A_INBOX_CONTENT_MAX_CHARS`（默认 4096）、`MANAGE_A2A_EXPIRE_SWEEP_SECONDS`（默认 30，设 `0` 关闭后台 sweep）。

### 5.3 注册字段 `base_url`

- **不参与** A2A 路由；仅运维探活 / Console 展示。

---

## 6. Task 状态机

```text
queued → delivered → processing → completed | failed | expired
                      ↘ awaiting_caller → caller_notified → caller_responded → processing → completed
```

| 状态 | 含义 |
|------|------|
| `queued` | Manage 已接受，等待 callee 拉 inbox |
| `delivered` | inbox 拉取时已标记（或 ack 前） |
| `processing` | callee 已 ack 或已拉取 caller resume，turn loop 运行中 |
| `awaiting_caller` | callee 需 caller HITL；`result_text` 含 HITL JSON |
| `caller_notified` | caller 已收到 HITL 并中继至本地 TUI |
| `caller_responded` | caller 已 `caller_resume`；`pending_caller_resume` 待 callee 拉取 |
| `completed` | reply 已写入，caller 可读取 |
| `failed` | callee 拒绝或执行失败 |
| `expired` | TTL 超时未处理 |

---

## 7. 与子 Agent 的边界

| 类型 | 通信路径 | 注册 Manage |
|------|----------|-------------|
| 主 Agent | Manage A2A Task | 必须 |
| 临时子 Agent | Node 内部 API / 内存队列 | **不**独立注册 |

---

## 9. 待做

| 能力 | 状态 |
|------|------|
| `POST /v1/a2a/broadcast` | 未实现 |
| progress 经 Manage 中继 | 未实现 |
