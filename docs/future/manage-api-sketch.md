# Manage HTTP API 草图

本文描述 **Manage（Python）** 对外 API：**Agent Node 注册/心跳/审计上报**、**Agent 发现**、**运维查询**。

**Client 不调用 Manage**（见 [three-component-model.md](./three-component-model.md)）。

## 1. 设计原则

| 原则 | 说明 |
|------|------|
| **控制面 + A2A 数据面信令** | 不转发 Client 会话；**A2A 消息经 Manage inbox 中继**（见 [a2a-via-manage.md](./a2a-via-manage.md)） |
| **Node 仅出站连 Manage** | Node 不要求 peer 入站；A2A 收信靠 **inbox 轮询** |
| **必须注册** | 所有 Agent Node 启动时注册；`expose_to_peers` 只影响发现，不影响注册义务 |
| **审计 append-only** | Node 上报事件；Manage 持久化；运维只读查询 |
| **复用 Register Center 经验** | groups、agent card、A2A token 等可从现有 `register_center/` 演进 |

### 1.1 基础路径

| 前缀 | 调用方 |
|------|--------|
| `/health` | 探活 |
| `/metrics` | Prometheus（可选） |
| `/v1/agents/...` | Agent Node（register/heartbeat）、运维、其他 Node（discover） |
| `/v1/audit/...` | Agent Node（ingest）、运维（query） |
| `/v1/a2a/...` | Agent Node | **A2A 中继**（send / inbox / reply / status） |

### 1.2 认证

| 调用方 | 方式 |
|--------|------|
| Agent Node | `Authorization: Bearer <node_token>`（注册时签发或预配置） |
| 运维 / Dashboard | `Authorization: Bearer <admin_token>` 或 SSO |
| Agent Node discover | 同 `node_token` 或 `x-dagents-a2a-token` |

Phase 1 可：`MANAGE_NODE_TOKEN` 环境变量单 token；生产再分 agent 级 token。

### 1.3 通用错误体

```json
{
  "error": {
    "code": "agent_not_found",
    "message": "...",
    "details": {}
  }
}
```

---

## 2. Agent 注册与心跳

### 2.1 首次注册（Node 启动）

```http
POST /v1/agents/register
Authorization: Bearer ***
Content-Type: application/json

{
  "agent_id": "ops-win-01",
  "expose_to_peers": true,
  "groups": ["ops", "windows"],
  "host_info": {
    "os": "windows",
    "os_version": "2012 R2",
    "arch": "amd64"
  },
  "capabilities": ["shell", "filesystem", "skills"],
  "manifest_version": 1,
  "node_version": "0.2.0",
  "card": {
    "name": "Windows Ops Agent",
    "description": "AD 与 PowerShell 运维"
  }
}
```

响应：

```json
{
  "agent_id": "ops-win-01",
  "registered": true,
  "heartbeat_interval_seconds": 30,
  "audit_batch_max": 100,
  "server_time_unix": 1760000000
}
```

规则：

- `agent_id` 必须与 Node 配置一致；**一端口一 id**。
- `endpoint`（可选）：仅 **运维探活 / Dashboard 展示**；**不**用于 A2A peer 路由。
- `expose_to_peers=false` 时仍 `registered: true`，但 Manage **拒绝**向该 Agent 投递 A2A。

### 2.2 心跳

```http
POST /v1/agents/{agent_id}/heartbeat
Authorization: Bearer ***
Content-Type: application/json

{
  "status": "online",
  "active_sessions": 2,
  "active_executions": 0,
  "manifest_version": 1,
  "last_error": null,
  "metrics": {
    "cpu_percent": 12.5,
    "mem_mb": 256
  }
}
```

响应：

```json
{
  "agent_id": "ops-win-01",
  "next_heartbeat_seconds": 30,
  "policy_version": 3
}
```

Manage 侧：

- 超时未心跳 → `status=offline`（供运维列表；不影响已建立的 Node 本地 Client 会话）。
- 可选：`policy_version` 变化时 Node 拉取 `GET /v1/policy/agents/{agent_id}`。

### 2.3 注销（优雅停机）

```http
POST /v1/agents/{agent_id}/deregister
Authorization: Bearer ***

{ "reason": "shutdown" }
```

---

## 3. 发现与运维列表

### 3.1 Agent 发现（供 A2A）

```http
GET /v1/agents/discover?groups=ops,windows&caller_agent_id=peer-linux-01
Authorization: Bearer ***
```

响应（**仅 `expose_to_peers=true` 且 online**；**不含** callee 直连 URL）：

```json
{
  "agents": [
    {
      "agent_id": "ops-win-01",
      "groups": ["ops", "windows"],
      "capabilities": ["shell", "filesystem"],
      "card": { "name": "Windows Ops Agent", "description": "..." }
    }
  ]
}
```

---

## 4. A2A Task 中继（非子 Agent）

**所有跨 Agent 协作经 Manage Task**；Node **不得**直连其他 Node。协议详见 [a2a-via-manage.md](./a2a-via-manage.md)。

**不兼容** 旧 `messages` / `message_id` 命名。

### 4.1 创建 Task

```http
POST /v1/a2a/tasks
x-dagents-agent-id: peer-linux-01
Content-Type: application/json

{
  "from_agent_id": "peer-linux-01",
  "to_agent_id": "ops-win-01",
  "kind": "invoke",
  "content": "请检查 C:\\logs\\app.log 最后 50 行",
  "caller_session_id": "sess-a-...",
  "idempotency_key": "optional",
  "ttl_seconds": 3600
}
```

响应：

```json
{
  "task_id": "a2a-task-...",
  "status": "queued",
  "to_agent_id": "ops-win-01"
}
```

Manage 校验：`to` 存在、online、`expose_to_peers=true`；否则 `403 target_not_exposed` 或 `404 target_not_found`。

### 4.2 收取 inbox（被调方 long poll）

```http
GET /v1/a2a/inbox?agent_id=ops-win-01&limit=10&wait=25
x-dagents-agent-id: ops-win-01
```

```json
{
  "tasks": [
    {
      "task_id": "a2a-task-...",
      "from_agent_id": "peer-linux-01",
      "kind": "invoke",
      "content": "...",
      "created_at_unix": 1760000100,
      "expires_at_unix": 1760003700
    }
  ],
  "pending_count": 0
}
```

被调方 Node 拉取后本地入队 turn loop；可选 `POST /v1/a2a/tasks/{id}/ack`。

### 4.3 回复

```http
POST /v1/a2a/tasks/{task_id}/reply
x-dagents-agent-id: ops-win-01
Content-Type: application/json

{
  "agent_id": "ops-win-01",
  "status": "completed",
  "result_text": "...",
  "callee_session_id": "sess-b-..."
}
```

### 4.4 查询状态（调用方轮询）

```http
GET /v1/a2a/tasks/{task_id}?caller_agent_id=peer-linux-01
x-dagents-agent-id: peer-linux-01
```

```json
{
  "task": {
    "task_id": "a2a-task-...",
    "status": "completed",
    "result_text": "...",
    "from_agent_id": "peer-linux-01",
    "to_agent_id": "ops-win-01"
  }
}
```

### 4.5 广播（Phase 2，可选）

```http
POST /v1/a2a/broadcast
```

Manage 向多个 `to_agent_id`（或 groups）写入 Task inbox；仍 **无** Node 间直连。

---

## 5. 运维列表

### 5.1 全量列表（含未暴露）

```http
GET /v1/agents?include_offline=true&include_hidden=true
Authorization: Bearer <admin_token>
```

```json
{
  "agents": [
    {
      "agent_id": "ops-win-01",
      "expose_to_peers": true,
      "status": "online",
      "last_heartbeat_unix": 1760000030,
      "groups": ["ops", "windows"],
      "host_info": { "os": "windows" }
    },
    {
      "agent_id": "batch-internal-02",
      "expose_to_peers": false,
      "status": "online",
      "note": "仅运维可见，不可作为 A2A 目标"
    }
  ]
}
```

### 5.2 单个 Agent 详情

```http
GET /v1/agents/{agent_id}
```

---

## 6. 审计

### 6.1 事件上报（Node 主动）

```http
POST /v1/audit/events
Authorization: Bearer ***
Content-Type: application/json

{
  "agent_id": "ops-win-01",
  "events": [
    {
      "event_id": "aud-...",
      "ts_unix_ms": 1760000010123,
      "kind": "tool_execution",
      "session_id": "sess-...",
      "execution_id": "exec-...",
      "tool_name": "bash_run",
      "policy_decision": "auto",
      "status": "completed",
      "summary": "command=whoami exit=0",
      "peer_agent_id": null
    },
    {
      "event_id": "aud-...",
      "kind": "a2a_inbound",
      "caller_agent_id": "peer-linux-01",
      "status": "accepted"
    }
  ]
}
```

响应：

```json
{ "accepted": 2, "duplicates": 0 }
```

- `event_id` 幂等；Manage 去重。
- Phase 1 存储：SQLite / JSONL；Phase 2+ 可换 PostgreSQL / ClickHouse。

### 6.2 运维查询

```http
GET /v1/audit/events?agent_id=ops-win-01&since=2026-05-01T00:00:00Z&limit=100
Authorization: Bearer <admin_token>
```

```http
GET /v1/audit/events/{event_id}
```

---

## 7. 策略配置（Phase 2，可选）

Manage 集中存储；Node 心跳返回 `policy_version` 后拉取：

```http
GET /v1/policy/agents/{agent_id}
Authorization: Bearer ***
```

```json
{
  "version": 3,
  "tool_rules": [
    { "tool_name": "bash_run", "mode": "require_approval" },
    { "tool_name": "read_file", "mode": "auto" }
  ]
}
```

Phase 1 可在 Node 本地 `policy.yaml` 实现，Manage 仅审计。

---

## 8. 与现有 Register Center 的映射

| 现有（`register_center/`） | Manage 目标 |
|---------------------------|-------------|
| Agent upsert / list | `POST /v1/agents/register` + `GET /v1/agents` |
| groups 过滤 | `GET /v1/agents/discover?groups=` |
| relay / broadcast HTTP 直连 Agent | **改为** `POST /v1/a2a/tasks` + inbox long poll（[a2a-via-manage.md](./a2a-via-manage.md)） |
| health | `/health` |

Python 仓库保留并收敛为 **Manage 服务**；`app/core`、`app/harness/service` 的 Brain 逻辑 **不**保留在 Manage。

---

## 9. 数据模型摘要

### AgentRecord（Manage 持久化）

| 字段 | 说明 |
|------|------|
| `agent_id` | 主键 |
| `endpoint` | 可选；**仅运维**探活/展示，**不**用于 A2A 路由 |
| `expose_to_peers` | 是否可作为 A2A **目标**、是否出现在 discover |
| `groups` | 发现分组 |
| `status` | online / offline / unknown |
| `last_heartbeat_unix` | 最后心跳 |
| `host_info` | OS、glibc、版本等（**老旧服务器兼容审计**） |
| `capabilities` | shell、fs、… |
| `card` | A2A 展示 |

### AuditEvent

| 字段 | 说明 |
|------|------|
| `event_id` | 幂等键 |
| `agent_id` | 上报方 |
| `kind` | tool_execution / a2a_inbound / a2a_outbound / policy / … |
| `session_id` / `execution_id` | 关联 Node 会话 |
| `summary` | 运维可读摘要 |

---

### A2ATask（Manage 持久化）

| 字段 | 说明 |
|------|------|
| `task_id` | 主键 |
| `from_agent_id` / `to_agent_id` | 发送方 / 目标 |
| `kind` | invoke / notify |
| `status` | queued / delivered / processing / completed / failed / expired |
| `content` / `result_text` | 请求与回复正文 |
| `idempotency_key` | 调用方幂等键（可选） |
| `ttl_seconds` | 过期 |

---

## 10. Phase 1 最小落地集

| 优先级 | API |
|--------|-----|
| P0 | `POST /v1/registry/agents`、`POST .../heartbeat`、`GET /v1/registry/agents/discover` |
| P0 | `POST /v1/a2a/tasks`、`GET /v1/a2a/inbox`、`POST .../tasks/{id}/reply`、`GET .../tasks/{id}` |
| P0 | `POST /v1/audit/events` |
| P1 | `GET /v1/registry/agents`（运维）、`GET /v1/audit/events`（查询） |
| P2 | policy 下发、deregister、`POST /v1/a2a/broadcast` |
