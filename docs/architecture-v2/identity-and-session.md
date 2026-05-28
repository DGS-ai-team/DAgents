# 身份与会话模型

## 1. 当前模型

DAgents 当前使用两层标识：

```
session_id  — 标识一个对话会话（上下文 + 消息队列）
client_id   — 标识一个 SSE 推送通道（InMemoryEventBus 订阅键）
```

**问题**：
- `client_id` 由调用方任意指定，后端不校验，存在碰撞和伪造风险
- 缺少"谁属于哪个 Agent"的正式模型
- A2A 场景下 peer session 与主 session 没有显式关联
- Go Proxy 没有身份体系

## 2. 新模型：四层身份

```
┌─────────────────────────────────────────────┐
│                  agent_id                    │  ← Agent 身份
│         "k8s-ops-01" / "code-review-02"     │     向 RC 注册
├─────────────────────────────────────────────┤
│               connection_id                  │  ← 连接通道
│   "conn-abc123" / "conn-def456"             │     后端生成，关联 agent
├─────────────────────────────────────────────┤
│               session_id                     │  ← 对话上下文
│   "sess-xyz789" / "sess-uvw012"             │     消息队列 + 持久化
├─────────────────────────────────────────────┤
│               client_id                      │  ← SSE 推送端点
│   "client-111" / "client-222"               │     订阅通道，关联 session
└─────────────────────────────────────────────┘
```

### 2.1 agent_id

Agent 的全局唯一标识。在 Register Center 注册时声明，贯穿整个生命周期。

```python
class AgentRecord(BaseModel):
    agent_id: str                                    # 全局唯一
    agent_type: Literal["server", "terminal"] = "server"
    schedulable: bool = True
    base_url: str                                    # A2A 消息接收端点
    discovery_group: list[str]                       # RC 发现分组
    capabilities_hint: list[str]                     # 能力标签
    host_info: dict[str, str] | None = None          # terminal agent 宿主机信息
    registered_at_unix: int
    expires_at_unix: int
```

### 2.2 connection_id

连接通道标识。每个物理连接（Go TUI、A2A SSE、Go Proxy）一个 connection_id。**后端生成，调用方不可指定。**

```python
class ConnectionRecord(BaseModel):
    connection_id: str                               # 后端 uuid
    agent_id: str                                    # 归属
    conn_type: Literal["user_tui", "a2a_caller", "a2a_callee", "proxy"]
    client_id: str                                   # 关联的 SSE 通道标识
    sessions: set[str]                               # 此连接关联的 session_id 列表
    created_at: float
    last_event_at: float
```

**conn_type 说明**：

| 类型 | 含义 | 典型场景 |
|------|------|----------|
| `user_tui` | 用户终端连接 | Go TUI 或浏览器 UI 连接后端 |
| `a2a_caller` | A2A 主调方 SSE | Agent A 调用 Agent B 时拉取 B 的 SSE |
| `a2a_callee` | A2A 被调方 SSE | Agent B 被调用后，A 连 B 的流 |
| `proxy` | Go Proxy 连接 | 终端 Agent 的工具执行通道 |

### 2.3 session_id

对话会话标识。包含完整的 `OpenAIConversationContext`、消息队列、SQLite 持久化。

```python
class SessionRecord(BaseModel):
    session_id: str
    agent_id: str                                    # 归属的 Agent
    connection_ids: set[str]                         # 哪些连接可以订阅此 session
    created_at: float
    last_activity_at: float
    peer_agent_id: str | None = None                 # A2A 场景的对端 Agent
    peer_session_id: str | None = None               # A2A 场景的对端 session
    ttl_seconds: int = 86400                         # 默认 24h
```

### 2.4 client_id

SSE 推送端点标识。一个 connection 至少有一个 client_id。A2A 场景下，调用方生成新的 client_id 用于拉取对端 SSE。

```python
# client_id 由后端在连接建立时生成
# GET /v1/streams?client_id=xxx → InMemoryEventBus 查表推送
#
# 一个 session 可以推送到多个 client_id
# （多个终端同时观看同一个 Agent 的输出）
```

## 3. 身份流程

### 3.1 用户终端连接

```
Go TUI 启动
  → POST /v1/connections/register
     { "agent_id": "k8s-ops-01", "conn_type": "user_tui" }
  ← 返回 { "connection_id": "conn-abc", "client_id": "client-111" }

Go TUI 创建会话
  → POST /v1/sessions
     { "agent_id": "k8s-ops-01", "connection_id": "conn-abc" }
  ← 返回 { "session_id": "sess-xyz" }

Go TUI 连接 SSE
  → GET /v1/streams?client_id=client-111
  ← SSE 事件流开始推送

Go TUI 发送消息
  → POST /v1/messages
     { "session_id": "sess-xyz", "content": "帮我查一下集群状态" }
  → SSE 推送 assistant 回复
```

### 3.2 A2A 调用

```
Agent A 调用 Agent B
  → agent_send_message(target_agent_id="agent-b", message="...")

  后端内部：
  1. 生成 peer_session_id: "sess-peer-001"
  2. 生成 peer_client_id: "client-a2a-001"
  3. 向 Agent B 的 /v1/messages 发送：
     { "session_id": "sess-peer-001",
       "client_id": "client-a2a-001",
       "content": "..." }
  4. 后端创建 A2A connection 记录：
     ConnectionRecord(conn_type="a2a_caller", session="sess-peer-001", ...)
  5. 以 peer_client_id 连接 Agent B 的 SSE，收集回复
  6. 收集完成后返回给 Agent A
```

### 3.3 Go Proxy 连接

```
Go Proxy 启动
  → POST /v1/proxy/register
     { "agent_id": "k8s-ops-01", "capabilities": [...], "schedulable": true }
  ← 返回 { "connection_id": "conn-proxy-001" }

Go Proxy 心跳
  → POST /v1/proxy/heartbeat  (每 30s)
     { "connection_id": "conn-proxy-001" }

Python 后端需要执行工具
  → POST http://proxy-addr:9090/execute
     { "tool": "shell", "command": "kubectl get pods", "timeout": 30 }
  ← 返回 { "exit_code": 0, "stdout": "...", "stderr": "" }
```

## 4. 生命周期管理

| 资源 | 创建 | 销毁 |
|------|------|------|
| `agent_id` | RC 注册 + Backend 内部创建 | RC 注销（主动或 TTL 过期） |
| `connection_id` | 连接建立时 Backend 分配 | 连接断开 + 60s 宽限期后清理 |
| `session_id` | 用户/A2A 显式创建 | DELETE /v1/sessions 或 TTL 过期 |
| `client_id` | 连接注册时 Backend 分配 | connection 销毁时同步清理 |

**TTL 默认值**：

| 资源 | TTL | 可配置 |
|------|-----|--------|
| Agent RC 注册 | 60s（心跳续租） | `AGENT_REGISTRY_TTL_SECONDS` |
| Connection | SSE 断开后 60s 清理 | `CONNECTION_GRACE_PERIOD_SECONDS` |
| Session | 24h 无活动后清理 | `SESSION_TTL_SECONDS` |
| Client | 随 Connection 生命周期 | — |

## 5. A2A session 的特殊处理

A2A 会话与用户会话的关键区别：

| 维度 | 用户会话 | A2A 会话 |
|------|----------|----------|
| 创建者 | 用户终端 | Agent 工具层 |
| session_id 格式 | `sess-{uuid}` | `peer-{caller}-{target}-{random}` |
| TTL | 24h | 5min（短命，收集完即销毁） |
| 持久化 | SQLite（可选） | 不持久化（内存中） |
| 审批回溯 | 通过 resume + resume_value | 通过 agent_peer_approve_tools |

## 6. 多终端看同一 Agent

```
Agent A
  │
  ├── connection_1 (Go TUI, 桌面终端)
  │     └── session_A
  │
  └── connection_2 (Go TUI, 手机终端)
        └── session_A (共享同一会话)

两个终端连接同一个 session_id，
后端向 session 关联的所有 client_id 推送 SSE。
```

此场景需要 Agent 支持多终端协商。当前 MVP 阶段，建议每个 session 只关联一个活跃的 `user_tui` 连接。

## 7. 安全约束

| 约束 | 说明 |
|------|------|
| `client_id` 由后端生成 | 调用方不可指定，防止碰撞和占位攻击 |
| 连接归属校验 | 请求中的 `connection_id` 必须属于该 `agent_id` |
| Session 归属校验 | 请求中的 `session_id` 必须在 `connection.sessions` 中 |
| A2A token | 跨 Agent 请求需携带 `x-dagents-a2a-token`（与现有一致） |
| Proxy 认证 | Proxy 注册时使用 agent 级别 token |
