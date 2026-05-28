# Message Queue 与执行控制

本文定义 architecture-v2 下消息队列、Agent Instance 路由和 Body 执行调度的职责边界。核心结论是：现有 per-session `MessageQueue` 思路应保留，但它只负责 Brain turn 串行化；Body 工具执行必须进入独立的执行控制层。

## 1. 核心原则

```text
Session Queue 负责同一 session 内 Brain turn 顺序处理。
Control Plane 负责 Agent Instance、session、connection、Body Binding 的路由和鉴权。
Execution Dispatcher 负责 Body 工具执行、策略、并发、超时、取消和结果回传。
```

不要把用户消息、Brain turn、Body execution、Proxy control channel、SSE event 全部塞进同一个队列模型。它们的生命周期、限流维度和失败处理不同。

## 2. 消息入口路由

用户、API 或 A2A 调用进入 Backend 后，不能直接按 `session_id` 投递到本地队列。必须先经过 Control Plane 校验：

```text
Incoming request
  → validate connection_id
  → validate session_id belongs to connection_id
  → load SessionRecord(session_id)
  → verify SessionRecord.agent_id == requested agent_id
  → resolve body_id and owning_backend_instance_id
  → route to owning Backend's Session Queue
```

推荐 `MessageEnvelope` 显式包含：

```python
class MessageEnvelope(BaseModel):
    agent_id: str
    body_id: str
    session_id: str
    connection_id: str
    request_type: Literal["message", "resume", "tool_result", "async_tool_result"]
    payload: dict
```

其中：

- `agent_id` 标识目标 Agent Instance。
- `body_id` 固定该 session 绑定的 Body。
- `session_id` 决定 Brain turn 的串行化边界。
- `connection_id` 用于鉴权、归属校验和 SSE 订阅授权。

## 3. Session Queue 的职责

Session Queue 是 Brain/session 层的本地执行结构，key 应为 `session_id`，并在入队前校验 `agent_id` 和 `connection_id` 归属。

它负责：

- 同一 session 内消息顺序处理。
- 避免 conversation context 并发修改。
- 将 `tool_result` 作为当前 Brain turn 的 continuation 优先处理。
- 管理 session consumer 的 pause、resume、cancel、stop。

它不负责：

- 判断工具在哪个 Body 上执行。
- 管理 Proxy control channel。
- 管理 Body 并发和队列深度。
- 作为多 Backend 的全局状态来源。

## 4. Body 执行调度

当 Brain Runtime 产生 ToolCall 后，Control Plane 创建 `ExecutionRecord` 并进入 Execution Dispatcher：

```text
Brain ToolCall
  → policy decision
  → create ExecutionRecord
  → dispatch by tool.kind
      ├── backend
      │     → Backend executor
      └── body
            → active ProxyConnection(body_id)
            → Backend holding proxy_connection_id
  → execution result
  → enqueue tool_result to original Session Queue
```

Execution Dispatcher 的路由输入至少包含：

- `execution_id`
- `agent_id`
- `body_id`
- `tool.kind`
- `session_id`
- `tool_name`
- `policy_decision_id`
- `proxy_connection_id`，仅 `tool.kind == "body"` 需要

执行结果不能直接修改 Brain context。它必须转换为 `tool_result` 或 `async_tool_result`，回到原 `session_id` 的 Session Queue，由 Brain loop 串行消费。

## 5. 队列数量和并发控制

现有单 Backend 下的最大队列数量控制应保留，但语义应调整为：

```text
max_active_session_consumers_per_backend
```

该限制保护 Backend 本地资源：

- session context 内存；
- LLM turn 并发；
- event stream 压力；
- 长时间挂起 session 的资源占用。

它不应控制 Body execution 并发。Body 层应使用独立限制：

```text
max_active_executions_per_body
max_execution_queue_depth_per_body
max_active_executions_per_proxy_connection
max_execution_output_bytes
max_execution_timeout_seconds
```

如果把 session queue limit 和 Body execution limit 混在一起，会导致两个错误：

1. Brain 空闲但 Body 忙时，session queue 被误认为过载。
2. 多个 session 共享同一个 Body 时，真正需要限流的是 Body 或 ProxyConnection，而不是 session 数。

## 6. 单 Backend Phase 1

Phase 1 可以保留 in-process Session Queue：

```text
session_id → local MessageQueue → local session consumer → Brain Runtime
```

最小改动：

- `MessageEnvelope` 增加 `agent_id`、`body_id`、`connection_id`。
- 入队前校验 `SessionRecord` 归属。
- 将现有队列上限重命名为 active session consumer 上限。
- 引入最小 `ExecutionRecord` 和 Execution Dispatcher。
- Body execution 完成后只通过 `tool_result` 回到 Session Queue。

## 7. 多 Backend Phase 2

多 Backend 下，Session Queue 仍可以是 owner Backend 的本地结构，但所有可恢复状态必须进入共享状态：

```text
SessionRecord.owning_backend_instance_id
ExecutionRecord.owner_backend_instance_id
Body presence
Proxy presence
SSE routing metadata，基于 connection/session subscription
```

请求落到非 owner Backend 时：

- 对普通消息：转发给 owning Backend，或投递到共享 session input stream。
- 对 SSE：订阅共享事件总线，或返回可重连信息。
- 对 Body tool execution：投递给持有 `proxy_connection_id` 的 Backend。

in-process queue 不能作为全局事实来源。它只是当前 owner Backend 推进某个 session turn 的本地执行机制。

## 8. 与 Brain + Body 架构的匹配关系

现有 `MessageQueue` 的合理部分是 per-session 串行处理。它天然匹配 Brain 层，因为 Brain 需要按 session 顺序读取和修改 conversation context。

需要重构的部分不是 queue 本身，而是职责边界：

| 当前关注点 | v2 归属 |
|------------|---------|
| 用户消息入队 | Control Plane 校验后进入 Session Queue |
| Brain turn 串行化 | Session Queue |
| ToolCall 策略判定 | Control Plane Policy |
| 工具执行路由 | Execution Dispatcher |
| Proxy 通道选择 | Control Plane + Proxy presence |
| 工具结果回传 | Session Queue 的 `tool_result` |
| SSE 推送 | Event routing / Client state |

因此 v2 不需要推翻当前 MessageQueue，而是要防止它继续扩张为通用任务队列。

## 9. 不变量

- 未校验 `connection_id` 与 `session_id` 归属前，消息不能入队。
- `session_id` 的 Brain turn 必须串行处理。
- `agent_id` 和 `body_id` 必须来自 `SessionRecord` 或服务端可信状态，不能由调用方任意覆盖。
- Body execution 必须有 `ExecutionRecord`。
- Body tool execution 必须绑定当前 active `proxy_connection_id`。
- execution result 必须回到原 session 的 Session Queue，由 Brain 串行消费。
- Session Queue limit 与 Body execution limit 必须分开配置和观测。
