# Agent 双运行时目标架构

DAgents v2 将 Agent 拆成大脑和身体：**大脑统一在 Python Backend，身体可以在 Backend 本机，也可以在远程宿主机的 Go Proxy**。

```text
Agent = Brain + Body

Brain: LLM 推理、上下文、Agent loop、A2A 协作
Body: shell、文件系统、本地工具链、宿主机权限域
```

## 1. Agent 类型

### 1.1 Server Agent

`agent_type: "server"` 表示大脑和身体都在 Python Backend。

| 属性 | 说明 |
|------|------|
| Brain | Python Backend |
| Body | Python Backend 本机 |
| 工具执行 | 本地工具执行器 |
| schedulable | 默认 true |
| 典型场景 | 代码审查、文档生成、通用问答、Backend 本机自动化 |

Server Agent 是当前 DAgents 行为的延续。

### 1.2 Terminal Agent

`agent_type: "terminal"` 表示大脑在 Python Backend，身体在 Go Proxy 所在宿主机。

| 属性 | 说明 |
|------|------|
| Brain | Python Backend |
| Body | Go Proxy 宿主机 |
| 工具执行 | Proxy control channel 下发 |
| schedulable | 由 owner 配置 |
| 典型场景 | 老 Windows 运维、RHEL 6 数据库、k8s 集群、本地专用工具链 |

Terminal Agent 可以选择是否加入 A2A 调度：

- `schedulable: true`：可被其他 Agent 发现和调用。
- `schedulable: false`：仅绑定用户或特定入口使用，不参与普通 A2A 发现。

## 2. 目标拓扑

```text
                    ┌────────────────────┐
                    │  Register Center   │
                    │  Agent discovery   │
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
              │ sessions/proxies │
              └──────────────────┘

    Go Proxy C ──outbound control channel──> one Backend instance
    Go Proxy D ──outbound control channel──> one Backend instance
```

Backend 多副本共享状态。某个 Proxy 的长连接会落在一个 Backend 实例上，但其他 Backend 可以通过共享状态发现它的 presence，并通过拥有该连接的 Backend 转发执行任务。

## 3. Register Center 与 Backend 边界

Register Center 负责：

- Agent 注册、续租、注销。
- 按 `discovery_group`、`capabilities`、`schedulable` 返回候选 Agent。
- 存储 Agent 元数据，例如 `agent_type`、`host_info`。

Register Center 不负责：

- 判断工具是否允许执行。
- 路由工具执行请求。
- 管理 Proxy control channel。
- 存储 session、connection 或 SSE 状态。

Backend Control Plane 负责执行路由。RC 可以知道 Agent 是 server 还是 terminal，但不参与执行路径决策。

## 4. Go Proxy 生命周期

```text
启动
  → 读取配置和本地策略硬约束
  → 扫描环境能力
  → 向 Backend 注册
  → 建立 outbound control channel
  → 周期性心跳和状态上报
  → 接收执行任务
  → 返回执行结果
  → 退出时注销或等待 Backend 超时清理
```

### 4.1 注册

Proxy 启动后向 Backend 注册：

```json
{
  "agent_id": "k8s-ops-01",
  "agent_type": "terminal",
  "schedulable": true,
  "capabilities": ["kubernetes", "helm", "shell"],
  "host_info": {
    "os": "Windows Server 2012 R2",
    "arch": "amd64",
    "hostname": "ops-win-01"
  }
}
```

Backend 返回服务端生成的身份：

```json
{
  "proxy_connection_id": "pconn-...",
  "agent_id": "k8s-ops-01",
  "control_channel_url": "/v1/proxy/control/pconn-..."
}
```

### 4.2 控制通道

Proxy 使用服务端返回的 `proxy_connection_id` 建立长连接控制通道。控制通道语义：

- 连接由 Proxy 主动发起。
- Backend 在该连接上下发任务。
- Proxy 通过同一连接或配套结果接口返回结果。
- 心跳、任务进度、取消信号都绑定到该连接。

Phase 1 可以选择 WebSocket；后续可替换为 HTTP/2 stream 或 gRPC stream，但协议语义不变。

## 5. 工具执行流程

```text
1. 用户或 A2A 消息进入 session
2. Brain Layer 推理并产生 ToolCall
3. Control Plane 查询 session、agent、proxy presence 和 policy
4. policy 返回 auto / require_approval / deny
5. 若允许执行：
   - server agent → 本地工具执行器
   - terminal agent → ProxyManager 下发 ExecutionRequest
6. 执行结果归一化为 ToolResult
7. Brain Layer 继续推理
8. SSE 将过程和结果推给订阅 client
```

ExecutionRequest 示例：

```json
{
  "execution_id": "exec-...",
  "agent_id": "k8s-ops-01",
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

## 6. 离线、超时和背压

### 6.1 Proxy 离线

Backend 在共享状态中维护 Proxy presence。若超过配置时间未收到心跳或控制通道断开：

- 标记 Proxy offline。
- terminal agent 不再接受新的远程执行任务。
- 正在等待的 execution 标记为 interrupted 或 timeout。
- 若该 Agent `schedulable: true`，RC 中的记录应尽快续租失败或被注销。

### 6.2 执行超时

每个 ExecutionRequest 必须有超时。超时后：

- Backend 向 Proxy 发送 cancel。
- Proxy 尝试终止本地进程树。
- 结果标记为 `timeout`，并记录审计事件。

### 6.3 背压

每个 Proxy 应有最大并发和队列限制：

- 超过并发限制时返回 `busy` 或排队。
- 超过队列限制时拒绝新任务。
- Backend 将队列深度、执行延迟和拒绝次数暴露为指标。

## 7. 数据模型扩展

AgentRecord 建议字段：

```python
class AgentRecord(BaseModel):
    agent_id: str
    base_url: str
    discovery_group: list[str]
    capabilities_hint: list[str]
    agent_type: Literal["server", "terminal"] = "server"
    schedulable: bool = True
    host_info: dict[str, str] | None = None
    registered_at_unix: int
    expires_at_unix: int
```

ProxyConnection 建议字段：

```python
class ProxyConnection(BaseModel):
    proxy_connection_id: str
    agent_id: str
    backend_instance_id: str
    status: Literal["online", "offline", "draining"]
    capabilities: list[str]
    host_info: dict[str, str]
    last_heartbeat_at: float
    max_concurrency: int
    active_executions: int
```

## 8. 迁移路径

1. 扩展 Agent 元数据字段，默认保持 `server`。
2. 新增 Proxy 注册和 control channel，不改变 server agent 执行路径。
3. 在工具执行入口增加统一路由层。
4. terminal agent 先支持少量工具和静态策略。
5. 引入共享状态后支持多 Backend、跨实例 Proxy 路由和集中 presence。
