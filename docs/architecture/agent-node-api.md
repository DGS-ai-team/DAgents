# Agent Node HTTP API

本文描述 **Agent Node（Go）** 对外 HTTP/SSE 接口，与 `node/internal/api/` 对齐维护。Manage 远期 API 见 [manage-api-sketch.md](../future/manage-api-sketch.md)。

## 1. 设计原则

| 原则 | 说明 |
|------|------|
| **一进程一 `agent_id` 一端口** | 监听地址即该 Agent 身份；不在此端口暴露子 Agent |
| **Client 仅本地** | 默认只 bind `127.0.0.1`；Client 与 Node 同包读同一 `local.endpoint` |
| **思考与工具在 Node 内** | 无「Backend 代执行」路径；tool call 由 turn loop 本地完成 |
| **A2A 经 Manage** | 非子 Agent 禁止 peer 入站；`expose_to_peers` 仅控制是否可作为 A2A **目标** |
| **会话态在 Node** | session 上下文、队列、持久化由 Node 负责（SQLite 或等价） |

### 1.1 基础路径

| 前缀 | 调用方 | 说明 |
|------|--------|------|
| `/v1/...` | Client（本地） | 会话、消息、SSE、HITL resume |
| `/health` | 探活 | 负载均衡 / 运维脚本 |
| `/v1/internal/...` | Node 进程内 | 子 Agent；**不**对外 HTTP |

### 1.2 通用错误体

```json
{
  "error": {
    "code": "session_not_found",
    "message": "session sess-xxx 不存在",
    "details": { "session_id": "sess-xxx" }
  }
}
```

常见 `code`：`invalid_session`、`turn_busy`、`policy_denied`、`approval_required`、`a2a_target_not_exposed`、`llm_error`、`tool_error`。

### 1.3 认证（Phase 递进）

| 调用方 | Phase 1 | Phase 2+ |
|--------|---------|----------|
| Client（本地） | 可选 `127.0.0.1` 免鉴权；或 `Authorization: Bearer <local_client_token>` | mTLS / Unix socket |

**无** 其他 Agent Node 入站认证（peer 不直连本 Node）。

---

## 2. Client Plane（本地）

### 2.1 健康与元数据

```http
GET /health
```

```json
{ "status": "ok", "agent_id": "ops-win-01", "version": "0.2.0" }
```

```http
GET /v1/agent/info
```

```json
{
  "agent_id": "ops-win-01",
  "expose_to_peers": true,
  "capabilities": ["shell", "filesystem", "skills"],
  "manage_registered": true
}
```

### 2.2 Session

```http
POST /v1/sessions
Content-Type: application/json

{ "session_id": null }
```

响应：

```json
{
  "session_id": "sess-7f2a...",
  "agent_id": "ops-win-01",
  "created": true
}
```

- `session_id` 可选；省略则由 Node 生成。
- Client **不需要** `connection_id`（旧 v2 Backend 模型已废弃）；SSE 按 **单 Client 单连接** 或 `session_id` 分桶（Phase 1 建议：**一个 TUI 一个 SSE 连接，多 session 事件带 `session_id`**）。

```http
GET /v1/sessions
DELETE /v1/sessions/{session_id}
POST /v1/sessions/{session_id}/cancel
POST /v1/sessions/{session_id}/clear-context
GET /v1/sessions/{session_id}/context
GET /v1/sessions/{session_id}/child-agents
GET /v1/sessions/{session_id}/child-agents/{child_session_id}
POST /v1/sessions/{session_id}/child-agents/{child_session_id}/cancel
```

### 2.3 消息与 resume

```http
POST /v1/messages
Content-Type: application/json

{
  "session_id": "sess-7f2a...",
  "request_type": "message",
  "content": "列出当前目录"
}
```

`request_type`：`message` | `resume`。

resume 示例（审批）：

```json
{
  "session_id": "sess-7f2a...",
  "request_type": "resume",
  "resume_value": {
    "kind": "tool_approval",
    "execution_id": "exec-...",
    "decision": "approved"
  }
}
```

响应：

```json
{ "accepted": true, "session_id": "sess-7f2a...", "priority": "human" }
```

### 2.4 SSE 事件流

```http
GET /v1/streams?session_id=sess-7f2a...
Accept: text/event-stream
Last-Event-ID: 42
```

- Phase 1 可简化为 **全局单流**（一个 Client 一个 Node 实例通常一个活跃 session）。
- 帧格式见 [client-events-and-hitl.md](./client-events-and-hitl.md)（修订版：去掉 `connection_id` 必填，保留 `session_id` / `execution_id`）。

核心事件：`assistant`、`reasoning`、`tool_call`、`tool_result`、`approval_required`、`user_information_required`、`execution_progress`、`error`、`done`。

### 2.5 Skills（可选 HTTP；也可仅 tool）

```http
GET /v1/sessions/{session_id}/skills
POST /v1/sessions/{session_id}/skills/load
POST /v1/sessions/{session_id}/skills/unload
```

与工具 `load_skills` 语义一致；HTTP 供 Client 设置页使用。

---

## 2.8 临时子 Agent（Client / 用户）

契约详述见 **[child-agent-tools.md](./child-agent-tools.md)**。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/v1/sessions/{parent_session_id}/child-agents` | 列出该父 session 下**未交付**的活跃子 Agent |
| GET | `/v1/sessions/{parent_session_id}/child-agents/{child_session_id}` | 查询单个子 Agent 状态 |
| POST | `/v1/sessions/{parent_session_id}/child-agents/{child_session_id}/cancel` | 用户/Client 停止子 Agent（与工具 `cancel_child_agent` 等价） |

- 子 Agent 由父 Agent 工具 **`create_temporary_agent`** 创建，**无**独立 SSE；事件 **`child_agent_created` / `child_agent_completed` / `child_agent_cancelled`** 发往**父** session 的 `GET /v1/streams`。
- 子 Agent **生命周期**在**向父 Agent 交付结果**后结束并回收；交付时发送结束类 SSE。

父 Agent 工具（非 HTTP）：`create_temporary_agent`、`wait_child_agents`、`child_agent_status`、`cancel_child_agent`。

---

## 3. A2A（经 Manage，无入站 API）

非子 Agent 的 A2A **不**在本 Node 暴露 HTTP 路由；由工具层调用 **Manage**（见 [a2a-via-manage.md](./a2a-via-manage.md)、[manage-api-sketch.md](./manage-api-sketch.md) §4）。

| Node 内工具 | Manage API |
|-------------|------------|
| `agent_discover` | `GET /v1/agents/discover` |
| `agent_send_message` | `POST /v1/a2a/messages` + 轮询 `GET /v1/a2a/messages/{id}` |
| `agent_broadcast` | `POST /v1/a2a/broadcast`（Phase 2） |

**Inbox 轮询**（后台 goroutine，与 heartbeat 同周期或独立）：

```http
GET {manage_url}/v1/a2a/inbox?agent_id={self}&limit=10
```

拉取到的消息入本地 **A2A session** 队列，走 turn loop；处理完成后：

```http
POST {manage_url}/v1/a2a/messages/{message_id}/reply
```

---

## 4. 子 Agent 实现分层（Go Node）

临时子 Agent **不监听端口**、**不注册 Manage 独立条目**（继承主 `agent_id`）。

| 层 | 说明 |
|----|------|
| **HTTP** | §2.8 list / get / cancel（用户与 Client） |
| **工具** | `create_temporary_agent` 等（父 Agent turn loop） |
| **进程内** | `node/internal/childagent/`：`Create` / `Deliver` / `Cancel` / `Wait` |

字段、SSE、生命周期见 **[child-agent-tools.md](./child-agent-tools.md)**。

子 Agent turn 与工具执行共享：

- 同一 FS 根 / shell 环境
- 同一 LLM 客户端（审计日志带 `parent_session_id`、`child_agent_kind=temporary`）

---

## 5. 工具执行（无独立 Client API）

工具由 turn loop 内部调度，**不**对 Client 暴露 `POST /tools/execute`。

执行生命周期通过 SSE 表达：

```text
tool_call →（可选 approval_required）→ execution_progress → tool_result → done
```

Node 内部分层（实现参考，非 HTTP）：

```text
TurnOrchestrator
  → PolicyEngine（本地 + Manage 下发的静态策略文件）
  → ToolRegistry（bash、fs、skills、triggers、agent_discover、agent_send_message、…）
  → Executor（os/exec、fs、sandbox）
  → AuditReporter → Manage
```

---

## 6. 与 Manage 的出站调用（Node 作为客户端）

见 [manage-api-sketch.md](./manage-api-sketch.md)：

- `POST /v1/agents/register`、`POST /v1/agents/{id}/heartbeat`
- `POST /v1/audit/events`
- **A2A**：`GET /v1/agents/discover`、`POST /v1/a2a/messages`、`GET /v1/a2a/inbox`、`POST .../reply`、`GET /v1/a2a/messages/{id}`

**无** WebSocket control channel；**无** peer Node 直连。

---

## 7. 配置示例（与 Client 共享）

```yaml
# /etc/dagents/agent.yaml（示意）
agent_id: ops-win-01
listen:
  host: 127.0.0.1
  port: 18765
expose_to_peers: true
groups: [ops, windows]
manage:
  url: https://manage.example.com
  node_token: "${MANAGE_NODE_TOKEN}"
fs_root: D:\agent-workspace
llm:
  provider: openai
  model: gpt-4.1
```

Client 同目录 `client.yaml` 仅引用：

```yaml
local:
  endpoint: http://127.0.0.1:18765
  agent_id: ops-win-01
```

---

## 8. Phase 1 最小落地集

| 优先级 | API |
|--------|-----|
| P0 | `/health`、`POST /v1/sessions`、`POST /v1/messages`、`GET /v1/streams` |
| P0 | Manage 注册/心跳/审计（出站） |
| P1 | HITL resume、skills HTTP、A2A inbox 轮询 + Manage 工具 |
| P2 | **临时子 Agent**（[child-agent-tools.md](./child-agent-tools.md)）、execution_progress 细粒度事件 |
