# Agent Instance 与 Body Binding 目标架构

DAgents v2 使用统一 Agent 模型：**Agent 是一个可寻址的 Brain + Body 实例**。Brain 运行在 Python Backend，Body 提供执行环境、资源边界、宿主机能力和本地硬约束。

```text
Agent Instance = Brain Runtime + Brain Profile + Body Binding

Brain Runtime: LLM 推理、Agent loop、上下文管理、A2A reasoning
Brain Profile: 模型策略、提示词策略、工具选择策略、上下文策略
Body Binding: 该 Agent 唯一绑定的执行环境与资源边界
```

## 1. 统一 Agent 模型

v2 不再把 Agent 定义为两类。所有 Agent 都是 `agent_id` 标识的 Agent Instance。

| 概念 | 说明 |
|------|------|
| Agent Instance | A2A、session、RC 发现面对的可寻址对象 |
| Brain Runtime | Backend 中共享的推理运行时，不单独拥有 Agent 身份 |
| Brain Profile | 某个 Agent 的推理配置、prompt 拼接策略、context 策略 |
| Body | 执行环境、资源边界、host environment、工具链和本地硬约束 |
| Body Binding | Agent Instance 与一个 active Body 的一对一绑定 |

关键不变量：

- 一个 Agent Instance 同一时间只绑定一个 active Body。
- 如果 Body 的 prompt、resource、context、environment、skills 或权限域不同，应注册为不同 Agent Instance。
- A2A 调用目标是 Agent Instance，不是 Body，也不是某种 Agent 类型。
- 调用方不关心目标 Agent 的 Body 部署形态；目标 Agent 自己通过 Control Plane 路由执行。

## 2. Tool kind

Agent Instance 只绑定一个 Body，但它可以拥有多种工具。执行位置由工具声明决定，而不是由 Body 把整个 Agent 固定成某类执行体。

| tool.kind | 说明 | 执行位置 |
|-----------|------|----------|
| `backend` | 依赖 Backend、共享状态或控制平面的工具 | Backend executor |
| `body` | 依赖绑定 Body 的 shell、文件系统、本地脚本或宿主机资源 | Body control channel |

`tool.kind` 是执行路由属性。Body Binding 定义“这个 Agent 的终端/宿主执行环境是谁”，Tool Manifest 定义“每个工具在哪里执行”。

## 3. 目标拓扑

```text
                    ┌────────────────────┐
                    │  Register Center   │
                    │ Agent discovery    │
                    └─────────┬──────────┘
                              │
              ┌───────────────┴────────────────┐
              │                                │
              ▼                                ▼
    ┌──────────────────┐              ┌──────────────────┐
    │ Python Backend A │              │ Python Backend B │
    │ Brain + Control  │              │ Brain + Control  │
    └────────┬─────────┘              └────────┬─────────┘
             │                                 │
             └──────────┬──────────────────────┘
                        ▼
              ┌──────────────────┐
              │  Shared State    │
              │ sessions/bodies  │
              └──────────────────┘

Agent code-review-01 ── tools: backend + body ── Backend executor / Body control channel
Agent k8s-ops-01    ── tools: backend + body ── Backend executor / Go Proxy outbound channel
```

Backend 多副本共享状态。某个 Body 的 Proxy 长连接会落在一个 Backend 实例上；其他 Backend 通过共享状态发现该 Body 的 presence，并在执行 `tool.kind == "body"` 的工具时通过持有连接的 Backend 转发任务。

## 4. Register Center 与 Backend 边界

Register Center 负责：

- Agent Instance 注册、续租、注销。
- 按 `discovery_group`、`capabilities`、`schedulable` 返回候选 Agent。
- 存储 Agent 元数据，例如 `body.host_info`、`body.capabilities`、`tools`。

Register Center 不负责：

- 判断工具是否允许执行。
- 路由工具执行请求。
- 管理 Proxy control channel。
- 存储 session、connection 或 SSE 状态。

Backend Control Plane 负责执行路由。RC 可以知道 Agent 绑定了什么 Body，但不参与执行路径决策。

## 5. Go Proxy 生命周期

Go Proxy 是 Body 的宿主机执行代理，负责让 `tool.kind == "body"` 的工具可以在该 Body 上执行。

```text
启动
  → 读取 Body 配置和本地策略硬约束
  → 扫描环境能力
  → 向 Backend 注册 Agent + Body Binding
  → 建立 outbound control channel
  → 周期性心跳和状态上报
  → 接收执行任务
  → 返回执行结果
  → 退出时注销或等待 Backend 超时清理
```

### 5.1 Proxy 注册

Proxy 启动后向 Backend 注册它承载的 Agent Instance 与 Body：

```json
{
  "agent_id": "k8s-ops-01",
  "body": {
    "body_id": "body-k8s-ops-01",
    "capabilities": ["kubernetes", "helm", "shell"],
    "resources": ["prod-kubeconfig", "ops-workspace"],
    "environment": {
      "workspace": "/opt/dagents/workspace"
    },
    "host_info": {
      "os": "Windows Server 2012 R2",
      "arch": "amd64",
      "hostname": "ops-win-01"
    },
    "policy_profile": "production-ops"
  },
  "tools": [
    {"name": "agent_discover", "kind": "backend"},
    {"name": "shell_exec", "kind": "body"},
    {"name": "file_read", "kind": "body"},
    {"name": "file_write", "kind": "body"}
  ],
  "schedulable": true
}
```

Backend 返回服务端生成的连接身份：

```json
{
  "proxy_connection_id": "pconn-...",
  "agent_id": "k8s-ops-01",
  "body_id": "body-k8s-ops-01",
  "control_channel_url": "/v1/proxy/control/pconn-..."
}
```

### 5.2 控制通道

Proxy 使用服务端返回的 `proxy_connection_id` 建立长连接控制通道。控制通道语义：

- 连接由 Proxy 主动发起。
- Backend 在该连接上下发任务。
- Proxy 通过同一连接或配套结果接口返回结果。
- 心跳、任务进度、取消信号都绑定到该连接。
- 任务必须同时绑定 `agent_id`、`body_id` 和当前 active `proxy_connection_id`。

Phase 1 可以选择 WebSocket；后续可替换为 HTTP/2 stream 或 gRPC stream，但协议语义不变。

## 6. 工具执行流程

```text
1. 用户或 A2A 消息进入 Agent session
2. Brain Runtime 按该 Agent 的 Brain Profile 推理并产生 ToolCall
3. Control Plane 查询 session、Agent、Tool Manifest、Body Binding、Body presence 和 policy
4. policy 返回 auto / require_approval / deny
5. 若允许执行：
   - tool.kind == backend → Backend executor
   - tool.kind == body    → ProxyManager 下发 ExecutionRequest 到绑定 Body
6. 执行结果归一化为 ToolResult
7. Brain Runtime 继续推理
8. SSE 将过程和结果推给授权 connection
```

ExecutionRequest 示例：

```json
{
  "execution_id": "exec-...",
  "agent_id": "k8s-ops-01",
  "body_id": "body-k8s-ops-01",
  "session_id": "sess-...",
  "tool": "shell_exec",
  "params": {
    "command": "kubectl get pods -A",
    "cwd": "/workspace",
    "timeout_seconds": 30
  },
  "policy_decision_id": "pol-..."
}
```

ExecutionResult 示例：

```json
{
  "execution_id": "exec-...",
  "status": "completed",
  "exit_code": 0,
  "stdout": "...",
  "stderr": "",
  "duration_ms": 842
}
```

## 7. 离线、超时和背压

### 7.1 Body 离线

Backend 在共享状态中维护 Body presence。若 Body 超过配置时间未收到心跳或控制通道断开：

- 标记 Body offline。
- 绑定该 Body 的 Agent 不再接受依赖该 Body 的新执行任务。
- 正在等待的 execution 标记为 interrupted 或 timeout。
- 若该 Agent `schedulable: true`，RC 中的记录应尽快续租失败或被注销。

### 7.2 执行超时

每个 ExecutionRequest 必须有超时。超时后：

- Backend 向 Proxy 发送 cancel。
- Proxy 尝试终止本地进程树。
- 结果标记为 `timeout`，并记录审计事件。

### 7.3 背压

每个 Body 应有最大并发和队列限制：

- 超过并发限制时返回 `busy` 或排队。
- 超过队列限制时拒绝新任务。
- Backend 将队列深度、执行延迟和拒绝次数暴露为指标。

## 8. 数据模型扩展

AgentRecord 建议字段：

```python
class AgentRecord(BaseModel):
    agent_id: str
    base_url: str
    discovery_group: list[str]
    capabilities_hint: list[str]
    schedulable: bool = True
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

ProxyConnection 建议字段：

```python
class ProxyConnection(BaseModel):
    proxy_connection_id: str
    agent_id: str
    body_id: str
    backend_instance_id: str
    status: Literal["online", "offline", "draining"]
    capabilities: list[str]
    host_info: dict[str, str]
    last_heartbeat_at: float
    max_concurrency: int
    active_executions: int
```

## 9. 迁移路径

1. 新增 Agent Body Binding 元数据，不再引入 Agent 类型二分。
2. 新增 Tool Manifest，用 `tool.kind` 区分 Backend tool 和 Body tool。
3. 新增 Proxy 注册和 control channel，用于注册 Agent 绑定的 Body。
4. 在工具执行入口增加统一路由层：按 `tool.kind` 路由，再按 Body Binding 定位 Body tool 的执行目标。
5. 引入共享状态后支持多 Backend、跨实例 Proxy 路由和集中 Body presence。
