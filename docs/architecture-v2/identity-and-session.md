# 身份与会话模型

v2 的身份模型要解决五个问题：调用方不能伪造连接和会话，session 必须有明确归属，A2A 会话必须可追踪，Body Binding 必须稳定，Proxy 连接必须能在多 Backend 中定位。

## 1. 身份层级

```text
agent_id              Agent Instance 的长期身份，向 RC 注册
body_id               Agent 绑定的执行环境身份
backend_instance_id   Backend 实例身份，用于多副本协调
connection_id         用户、A2A 或系统连接身份，由 Backend 生成
session_id            对话上下文身份，由 Backend 生成
proxy_connection_id   Go Proxy 控制通道身份，由 Backend 生成
execution_id          单次工具执行身份，由 Backend 生成
```

除 `agent_id` 和配置声明的 `body_id` 需要在注册时校验外，其余运行期 ID 都由服务端生成。外部调用方不能自造或占用这些 ID。SSE 推送订阅不再引入独立客户端订阅 ID，由 `connection_id + session_id` 授权关系表达。

## 2. Agent Instance 身份

`agent_id` 是统一 Agent Instance 的稳定身份，用于 RC 发现、A2A 寻址和 session 归属。Agent Instance 绑定 exactly one active Body；如果 Body 基础设施不同，应注册为不同 Agent Instance。

```python
class AgentRecord(BaseModel):
    agent_id: str
    schedulable: bool = True
    base_url: str
    discovery_group: list[str]
    capabilities_hint: list[str]
    brain_profile: BrainProfileRef
    body: AgentBodyRef
    tools: list[ToolRef]
    registered_at_unix: int
    expires_at_unix: int


class BrainProfileRef(BaseModel):
    profile_id: str
    model_policy: str
    prompt_profile: str
    context_policy: str


class AgentBodyRef(BaseModel):
    body_id: str
    capabilities: list[str]
    resources: list[str]
    environment: dict[str, str]
    host_info: dict[str, str] | None = None
    policy_profile: str


class ToolRef(BaseModel):
    name: str
    kind: Literal["backend", "body"]
    capabilities: list[str] = []
    policy_profile: str | None = None
```

约束：

- `agent_id` 必须全局唯一。
- 一个 Agent 同一时间只能绑定一个 active Body。
- `tool.kind == "body"` 的工具必须有 active Body/ProxyConnection 才能执行。
- `schedulable: false` 的 Agent 不应出现在普通 A2A discover 结果中，除非调用方有显式权限。

## 3. Backend 实例身份

多 Backend 部署下，每个实例有 `backend_instance_id`。它用于标识：

- 哪个 Backend 持有某条 SSE 连接。
- 哪个 Backend 持有某个 Proxy control channel。
- 某个 execution 当前由哪个实例负责推进。

```python
class BackendInstanceRecord(BaseModel):
    backend_instance_id: str
    base_url: str
    status: Literal["online", "draining", "offline"]
    started_at: float
    last_heartbeat_at: float
```

共享状态层保存 Backend presence。实例退出或失联后，其他实例可以根据状态判断哪些 connection、stream 或 execution 需要失败、重连或接管。

## 4. Connection 身份

Connection 表示一个逻辑连接，不等同于 TCP 连接。

```python
class ConnectionRecord(BaseModel):
    connection_id: str
    agent_id: str
    backend_instance_id: str
    conn_type: Literal["user_tui", "web_ui", "api_client", "a2a_caller", "a2a_callee", "proxy"]
    session_ids: set[str]
    created_at: float
    last_event_at: float
    expires_at: float
```

约束：

- `connection_id` 由 Backend 生成。
- 请求中的 `connection_id` 必须属于请求声明的 `agent_id`。
- connection 只能访问它关联的 session。
- connection 断开后进入宽限期，超时清理相关 stream 订阅和短生命周期 session。

## 5. Session 身份

Session 表示用户或 A2A 与某个 Agent Instance 的一次对话上下文。

```python
class SessionRecord(BaseModel):
    session_id: str
    agent_id: str
    body_id: str
    owning_backend_instance_id: str | None
    connection_ids: set[str]
    created_at: float
    last_activity_at: float
    ttl_seconds: int
    peer_agent_id: str | None = None
    peer_session_id: str | None = None
    persisted: bool = True
```

在共享状态多 Backend 目标形态下，session 元数据必须进入共享状态。上下文正文可以按阶段选择：

- Phase 1：单 Backend 本地 SQLite。
- Phase 2：共享 session store 或可被 owning Backend 恢复的持久化存储。
- Phase 3：支持跨实例接管和集中历史查询。

## 6. SSE 授权与推送路由

v2 不再引入独立客户端订阅 ID。SSE 订阅身份由 `connection_id` 表达，允许推送范围由 connection 关联的 session 集合表达。

```python
class StreamSubscriptionRecord(BaseModel):
    connection_id: str
    backend_instance_id: str
    allowed_session_ids: set[str]
    created_at: float
    expires_at: float
```

SSE 连接规则：

- `GET /v1/streams?connection_id=...` 必须校验 connection 是否存在且未过期。
- SSE 只能推送该 connection 关联 session 的事件。
- session 事件必须带 `agent_id`、`body_id` 和 `session_id`，便于前端或 TUI 归属展示。
- 多 Backend 下，如果请求落到非持有 SSE 的实例，该实例应基于共享状态转发、重定向或通过消息总线订阅事件。

`body_id` 标识 Agent Instance 的执行环境；`connection_id` 标识一次用户、A2A 或系统连接。真实部署中二者经常一一对应，但语义上仍保持区分：Body 负责执行边界，connection 负责访问授权和事件推送。

## 7. Proxy 身份

Proxy 注册后获得 `proxy_connection_id`。

```python
class ProxyConnectionRecord(BaseModel):
    proxy_connection_id: str
    agent_id: str
    body_id: str
    backend_instance_id: str
    status: Literal["online", "offline", "draining"]
    capabilities: list[str]
    host_info: dict[str, str]
    last_heartbeat_at: float
    expires_at: float
```

约束：

- Proxy 心跳使用 `proxy_connection_id`，不是裸 `agent_id`。
- 一个 Body 同一时间只能有一个 active ProxyConnection。
- 一个 Agent 同一时间只能绑定一个 active Body；v2 不支持多 Body 模式。
- Proxy 重连会生成新的 `proxy_connection_id`，旧连接进入 offline 或 expired。
- 执行任务必须绑定当前 active `body_id` 与 `proxy_connection_id`，防止发给旧连接。

## 8. 用户连接流程

```text
Client 启动
  → POST /v1/connections
     { agent_id, conn_type: "user_tui" }
  ← { connection_id }

Client 创建 session
  → POST /v1/sessions
     { agent_id, connection_id }
  ← { session_id, body_id }

Client 订阅 SSE
  → GET /v1/streams?connection_id=...

Client 发送消息
  → POST /v1/messages
     { connection_id, session_id, content }
```

Backend 必须校验 `connection_id`、`session_id`、`agent_id` 和 `body_id` 的归属关系。

## 9. A2A 会话流程

A2A 调用不能由调用方自造目标 session 或连接订阅。A2A 调用的目标是 Agent Instance；调用方不关心目标 Agent 的工具是在 Backend 还是 Body 执行，也不能绕过目标 Agent 的 Brain、session、policy 或 Body Binding。

```text
Agent A 需要调用 Agent B
  → Agent A Backend 通过 RC 找到 Agent B base_url
  → Agent A Backend 请求 Agent B Backend 创建 A2A session
  → Agent B Backend 生成 connection_id、session_id，并返回目标 body_id 摘要
  → Agent A Backend 使用返回的授权信息发送消息和订阅结果
  → Agent B 的 Brain Runtime 根据自己的 Brain Profile 与 Body Binding 处理请求
  → 调用结束后 Agent B Backend 清理短命 A2A session
```

目标 Backend 创建的 A2A 资源应具备：

- 短 TTL，默认 5 分钟。
- 不持久化完整长期上下文，除非策略要求审计保留。
- 明确记录 caller agent、target agent、source session 和 peer session。
- 与普通用户 session 分开计数和限流。

## 10. Execution 身份

每次工具执行都有 `execution_id`。

```python
class ExecutionRecord(BaseModel):
    execution_id: str
    session_id: str
    agent_id: str
    body_id: str
    tool_name: str
    tool_kind: Literal["backend", "body"]
    proxy_connection_id: str | None
    policy_decision_id: str
    status: Literal["pending", "waiting_approval", "running", "completed", "failed", "denied", "timeout", "cancelled"]
    created_at: float
    updated_at: float
```

ExecutionRecord 进入共享状态层，便于多 Backend 下追踪、审批、取消和审计。

## 11. 生命周期默认值

| 资源 | 默认 TTL | 清理规则 |
|------|----------|----------|
| Agent RC 注册 | 60s | 心跳续租，过期移除 |
| Body presence | 90s | Body 无心跳则 offline，拒绝新的 Body tool execution |
| Backend instance | 30s | 心跳续租，失联标记 offline |
| Connection | 60s 宽限期 | 断开后无重连则清理 |
| User session | 24h 无活动 | 可配置，持久化上下文可保留更久 |
| A2A session | 5min | 调用结束即清理 |
| Stream subscription | 随 connection | connection 清理时同步删除 |
| Proxy connection | 90s 无心跳 | 标记 offline，拒绝新执行 |
| Execution | 由工具超时决定 | 结束后保留审计摘要 |

## 12. 安全不变量

- 外部调用方不能指定 `connection_id`、`session_id` 或 `execution_id`。
- 每个请求都必须校验 connection 与 session 的归属。
- SSE 订阅只能接收该 connection 授权 session 的事件。
- Body tool execution 必须绑定当前 active `body_id` 与 `proxy_connection_id`。
- A2A 请求必须携带有效 A2A token，并由目标 Backend 创建受控会话。
