# 代理输入与输出（HTTP 入队 · SSE 出站）

> **【历史 · Python 已移除】** 本文描述已删除的 **Python FastAPI `AgentService`** 入队与 SSE 行为。  
> **当前 CLI / 本地助手** 连接 **Go Agent Node**；HTTP/SSE 契约与 **`done` 语义**见 **[architecture/agent-node-api.md](./architecture/agent-node-api.md)**（尤其 §2.4.1）。  
> 下文保留作 v1 编排对照；**勿**将文中「`tool_call` 后必有 `done`」等时序直接套用到 Go Node。

本文是 **DAgents 后端「入站」与「出站」** 的专题说明：**入站**指客户端经 HTTP 投递、进入 **`AgentService`** 的 **进程内优先级消息队列**；**出站**指处理链路产生的流式事件经 **内存总线** 以 **SSE** 推回客户端。字段级 HTTP/SSE 契约仍以 [api-reference.md](./api-reference.md) 为准；整体编排见 [architecture-and-flows.md](./architecture-and-flows.md)。

---

## 第一部分：入站 — 用户消息队列（进程内）

### 1.1 设计目标与边界

| 项目 | 说明 |
|------|------|
| **目标** | 同一 **`session_id`** 上 **串行** 处理入队请求，避免并发修改同一 **`OpenAIConversationContext`**；在积压时让 **工具结果** 优先于普通用户闲聊出队。 |
| **范围** | **单进程**、**内存** 队列；**不**做跨进程 broker、**不**对队列内容做磁盘持久化（会话上下文持久化由 **`SqliteMessageStore`** 另行承担）。 |
| **非职责** | **`MessageQueue`** 不内嵌消费者；**不**根据优先级自动 **`cancel_current_turn`**（取消在途推理须 **`POST /v1/sessions/{session_id}/cancel`**，见 [api-reference.md](./api-reference.md)）。 |

**源码位置**：**`app/harness/queue/message_queue.py`**（队列类）、**`app/harness/service/agent_service.py`**（装配、入队、消费循环）。

### 1.2 总体结构：「每会话一条队列 + 单消费者循环」

```text
session_id=A  →  MessageQueue_A  →  asyncio.Task(_session_consume_loop("A"))
                         ↑
              submit_message / submit_resume / 异步工具回调
```

- **`_session_queues`**：每个活跃 **`session_id`** 至多一个 **`MessageQueue`**。
- **`_session_consumer_tasks`**：每个队列一个 **`_session_consume_loop(session_id)`**；循环内 **`await q.receive()`** 再 **`await _handle_message(env)`**，故 **同一 session 内任意时刻最多一条 envelope 在业务层执行**。
- **首次入队**某 `session_id` 时：**`_get_or_create_session_queue_async`** 创建队列并 **`create_task(_session_consume_loop)`**。

### 1.3 载荷：`MessageEnvelope`

| 字段 | 作用 |
|------|------|
| **`session_id`** | 路由到哪一个 **`MessageQueue`**。 |
| **`request_type`** | **`message`**、**`resume`**、**`tool_result`**、**`async_tool_result`**。 |
| **`content` / `resume_value` / `tool_result` / `async_tool_result`** | 各类型业务载荷。 |
| **`connection_id`** | **不参与** 队列排序；出队后用于 **SSE 路由**。**`async_tool_result`** 入队路径下 **须非空**（与 **`AsyncToolJob.connection_id`** 一致，源自会话上最近一次非空入站 **`connection_id`**）；其它 **`request_type`** 在纯内部场景下仍可为 **`None`**（总线不投递）。 |
| **`source`** | 观测标签，不影响优先级。 |

### 1.3.1 会话上的 `sse_connection_id`（进程内）

- **`OpenAIConversationContext.sse_connection_id`**：在 **`AgentService._handle_message`** 中，当 **`env.connection_id`** 非空白时写入，表示「本会话当前绑定的 SSE 通道」；**不入库**，重启后为空直至再次收到带 **`connection_id`** 的入站消息。  
- **异步工具**：**`_decorate_async_tool`** 在 **`submit_coroutine`** 时读取 **`ctx.sse_connection_id`**；若从未建立（空白），**`submit_coroutine`** / 装饰层会 **`ValueError`**，避免终态无法 **`publish`** 到客户端。  
- **内部回灌**：**`async_tool_result`** 的 **`MessageEnvelope`** 带 **`connection_id`**，处理该 envelope 时若再次写入 **`ctx.sse_connection_id`**，可刷新通道（与入站人类消息一致）。

### 1.4 优先级：`MessagePriority`

**`MessageQueue`** 使用 **`asyncio.PriorityQueue`**，元素为 **`(priority_int, seq, envelope)`**：`priority_int` 越小越先出队；**`seq`** 保证同优先级 **FIFO**。

| 标签 | 整数值 | 典型用途 |
|------|--------|----------|
| **`tool_result`** | **-1** | 工具结果回灌；**`submit_message`** 在 **`request_type` 为 `tool_result`/`async_tool_result` 且 `priority==other`** 时升格为 **`tool_result`**。 |
| **`human`** | **0** | **`POST /v1/messages`** 默认下 **`request_type=message`**（**`MessageIn._fill_default_priority`**）。 |
| **`resume`** | **1** | 默认 **`request_type=resume`**。 |
| **`other`** | **10** | 显式或默认兜底。 |

**与打断推理**：**`human` 先于 `resume` 出队**只影响 **排队顺序**；**`submit_message` 不会**因 **`human`** 自动 **`cancel_current_turn`**。若 **`MessageIn`** 注释写「可打断」，指产品侧「先入队再调 cancel」的组合策略，**不是**服务端单靠优先级取消当前 turn。

### 1.5 `MessageQueue` 行为摘要

- **`enqueue`**：非阻塞 **`put_nowait`**；有界满 → **`QueueFull`**；已 **`stop`** → **`RuntimeError`**。
- **`receive`**：先过 **`_consume_gate`**，再 **`get`**；关闭后抛 **`RuntimeError`**。
- **`pause_consuming` / `resume_consuming`**：闸门暂停/恢复出队；**`AgentService` 当前业务路径未使用**（预留给队列类；单测见 **`tests/test_message_queue.py`**）。
- **`stop`**：**不**清空堆内未 **`receive`** 的消息（MVP）；与 session 释放、进程关停组合使用。

**`pending_metrics_rows`**：依赖 CPython **`PriorityQueue._queue`** 堆快照供观测，**非**对外稳定 API。

### 1.6 入队入口（`AgentService`）

| 方法 | 摘要 |
|------|------|
| **`submit_message`** | 构造 **`MessageEnvelope`** 并 **`enqueue`**（**`effective_priority`** 含工具升格逻辑）。 |
| **`submit_resume`** | **`request_type=resume`**，默认 **`priority="resume"`**。 |
| **`_enqueue_async_tool_result_message`** | 从 **`AsyncToolResultStore`** 的 **`payload.connection_id`** 取通道，**`MessageEnvelope.connection_id`** 与之相同后 **`enqueue`**（缺 **`connection_id`** 时抛错）。 |

HTTP：**`POST /v1/messages`** → **`submit_message` / `submit_resume`**（**`app/harness/api/app.py`**）。

### 1.7 消费循环与在途 turn

**`_session_consume_loop`**：**`receive` → `create_task(_handle_message)` → `await` 子任务**；**`cancel_current_turn`** 只 **`cancel`** 子任务，**消费者循环不退出**；服务 **`stop()`** 时取消消费者与子任务再 **`MessageQueue.stop()`**。

### 1.8 背压与会话数上限

- **`max_queue_size`**（**`<=0`** 无界）：单队列长度。
- **`agent_max_active_session_queues`**：超限则尝试按闲置时间淘汰其它 session；无淘汰对象则 **`RuntimeError`**。

### 1.9 入站小结

| 维度 | 要点 |
|------|------|
| 隔离 | **`session_id` → 独立队列 + 独立 consumer** |
| 顺序 | **串行 `receive` → `_handle_message`** |
| 优先级 | **`tool_result` < `human` < `resume` < `other`**（数值）；同优先级 FIFO |
| 与出站 | 出队后的 **`env.connection_id`** 随 **`_emit_stream_event`** 传入 API 回调，决定能否写入 SSE 总线（见第二部分）。 |

---

## 第二部分：出站 — SSE 与事件总线

### 2.1 设计要点

| 项目 | 说明 |
|------|------|
| **模式** | **单进程内存总线** **`InMemoryEventBus`**（**`app/harness/streaming/events.py`**）；**`GET /v1/streams`** 长连接订阅，**按 `connection_id` 分桶** **`publish`**。 |
| **不回放** | **`subscribe_all`** 仅推送 **订阅建立之后** 的事件。 |
| **`done` 不断连** | **`done`** 只表示一轮处理语义结束；**SSE 连接保持**，由客户端关闭或网络中断结束。 |
| **与入队的关系** | 事件携带的 **`connection_id` / `session_id`** 来自当前处理的 **`MessageEnvelope`**；若 **`env.connection_id` 为空/空白**，API 层 **`handle_stream_event` 直接 return**，**不向总线发布**（无通道可归因）。 |

### 2.2 端到端链路（出站）

编排 / 运行时在 **`AgentService._handle_message`** 路径上产出 **`AgentEventEnvelope`**，经服务层统一映射并转发：

1. **`MainAgentTurnOrchestrator`**（等）调用注入的 **`emit_envelope`** → 实为 **`AgentService._emit_envelope`**  
2. **`_map_event_envelope_to_stream`** → **`(event_type, data)`**（**`data` 内含 `meta`**，见 [api-reference.md](./api-reference.md) §4）  
3. **`_emit_stream_event`** → **`await handle_stream_event(env, event_type, data)`**（FastAPI **`lifespan`** 内闭包）  
4. **`handle_stream_event`**：校验 **`env.connection_id`** 后 **`bus.publish(connection_id=..., session_id=..., event_type=..., data=...)`**  
5. **`InMemoryEventBus.publish`**：构造 **`StreamEvent`**（含 **`seq`**、**`ts`**），**`put_nowait`** 到该 **`connection_id`** 桶内各订阅队列，并复制一份到 **`_all_subscribers`**（无 **`connection_id`** 的调试订阅）  
6. **`GET /v1/streams`** 的生成器从连接级 **`asyncio.Queue`** 取事件，格式化为 SSE 帧写出  

**源码锚点**：**`app/harness/service/agent_service.py`**（**`_emit_envelope` / `_emit_stream_event` / `_map_event_envelope_to_stream`**）；**`app/harness/api/app.py`**（**`lifespan` → `handle_stream_event`**、**`_to_sse`**）；**`app/harness/streaming/events.py`**（**`InMemoryEventBus` / `StreamEvent`**）。

### 2.3 `StreamEvent` 与 SSE 帧形状

- **顶层**：**`connection_id`**、**`session_id`**、**`type`**（与 SSE **`event:`** 行一致）、**`seq`**（按 **`connection_id` 单调递增**）、**`ts`**（UTC ISO）、**`data`**（业务载荷 + **`meta`**）。  
- **帧格式**与 **`data` 内各 `type` 扁平字段** 见 [api-reference.md](./api-reference.md) **§3.5、§4**。

### 2.4 `type` 与编排层 `event_type` 的映射关系

**`AgentService._map_event_envelope_to_stream`** 将 **`AgentEventEnvelope.event_type`** 映射为 SSE 上的 **`type`**（多数同名）；主要分支包括：**`assistant`**、**`reasoning`**、**`usage`**、**`tool_call`**、**`tool_result`**、**`approval_required`**、**`error`**、**`done`**，未命中则兜底 **`chunk`**。实现见 **`app/harness/service/agent_service.py`** 中该静态方法全文。

### 2.5 客户端联调要点

1. **`POST /v1/connections`** 获取并稳定复用 **`connection_id`**。  
2. 建立 **`GET /v1/streams?connection_id=...`**（缺参 **422**）。  
3. **`POST /v1/messages`** 使用 **同一 `connection_id`** 与目标 **`session_id`**，否则可能 **收不到 SSE**（服务层仍处理消息，但总线不投递）。  
4. 多 **`session_id`** 可共用 **一条** SSE，在客户端用 **`session_id`** 分流展示。

### 2.6 SSE 事件类型速查（前端分流）

每条 SSE 帧的 **`event:`** 行与 JSON 顶层 **`type`** 一致；业务字段在 **`data.data`**（内层，含 **`meta`**）。完整字段表见 [api-reference.md](./api-reference.md) **§4**。

| `type` | 典型触发时机 | 是否增量 | 前端建议 |
|--------|----------------|----------|----------|
| **`assistant`** | 模型流式正文分片 | 是（多次） | 按 **`session_id`** 追加 **`content`**；用 **`display_type`**（默认 **`markdown`**）选渲染器 |
| **`reasoning`** | 模型流式「思考」分片（若网关支持） | 是 | 与 **`assistant`** 分开展示或折叠；默认 **`display_type=reasoning`** |
| **`tool_call_delta`** | 流式 **`delta.tool_calls`** 片段 | 是 | 可选：渐进展示参数 JSON；**落库与执行仍以回合末的 `tool_call` 为准** |
| **`usage`** | 流式末包或独立 usage 分片 | 否 | 统计/计费；**可忽略展示**（CLI 即跳过） |
| **`tool_call`** | 单轮模型 **`final`** 含 **`tool_calls`** | 否（整包） | 展示待执行工具列表；解析 **`tool_calls[]`**（`id` / `name` / `arguments`） |
| **`tool_result`** | 同步 **`_invoke_tool`** 完成、审批拒绝占位、异步回灌、用户打断 pending | 否（每条工具一条） | 按 **`tool_call_id`** 更新卡片；注意 **`rejected`** / **`interrupted_by_user_message`** / **`partial`** |
| **`approval_required`** | 存在需人工审批的 **`tool_calls`** | 否 | 弹出审批 UI；读 **`approval_id`**、**`approval_args`**；随后 **`POST /v1/messages` `request_type=resume`** |
| **`error`** | 参数/上限/恢复失败等 | 否 | Toast 或会话内错误条；**通常紧跟一条 `done`** |
| **`done`** | **一轮**编排语义结束（见下文「`done` 合并」） | 否 | **回合边界**：收起流式区、启用输入、读 **`finish_reason`**；**不断开 SSE**（**Go Node** 见 [agent-node-api.md §2.4.1](./architecture/agent-node-api.md)，含 `turn_complete` / `awaiting`） |
| **`chunk`** | 未映射的编排事件（兜底） | 视 **`raw`** | 调试日志即可 |

**`done` 与流式正文的关系（重要）**：

| 事件 | 是否实时推到 SSE |
|------|------------------|
| **`assistant` / `reasoning` / `tool_call_delta` / `usage`** | **是**：`runtime` 每 yield 一条，编排层立即 **`_emit_envelope`** → 总线 → 前端 |
| **`done`** | **否（相对 `finish_reason` 时刻）**：`runtime` 在网关 **`finish_reason`** 分片时会 yield **`done`**，但编排层在 **`_run_turn_and_maybe_execute_tools`** 里对 **`done` 执行 `continue`（只记录 `finish_reason`，不 publish）**，待 **`run_turn` 异步迭代完全结束**（含读取 **`final`**、无工具时写入 **`messages`**）后，再合并发出**一条** SSE **`done`** |

因此：

- **纯文本回合**：`done` **不是**等工具执行或下一轮模型，而是等**本轮 `run_turn` 收尾**；通常紧跟最后一帧 **`assistant`/`usage` 之后**，中间仅有极短的 **`final` 落库** 延迟，**不是**把整段正文攒完才一次性下发 `assistant`。
- **含 `tool_calls` 回合**：`done` 会推迟到编排层处理完 **`tool_call` / `approval_required`** 等之后再发，避免前端先看到 `done` 再看到 `tool_call`。

**`data.meta`**：各事件均可能含 **`session_id`**、**`model`** 等（由 **`base_meta`** 与信封 **`meta`** 合并），用于与 HTTP 入参交叉校验。

### 2.7 场景时序图（按业务路径）

以下图中 **`SSE:xxx`** 表示经总线推到 **`GET /v1/streams`** 的事件（`event:` 与 `type` 均为 `xxx`）。同一轮内 **`assistant` / `reasoning` / `tool_call_delta`** 可出现 **0～N 次**。

#### 2.7.1 纯文本回复（无工具调用）

模型流式输出正文；**`assistant` 实时下发**，**`done` 在 `run_turn` 结束后下发一条**（编排层合并 `finish_reason`，非「全文生成完才发 assistant」）。

```mermaid
sequenceDiagram
    autonumber
    participant C as 前端
    participant API as FastAPI / 总线
    participant R as runtime
    participant O as 编排层

    C->>API: POST /v1/messages (human)
    Note over O: 出队后 _run_turn_and_maybe_execute_tools

    loop 网关流式分片（实时 SSE）
        R-->>O: assistant / reasoning（增量）
        O->>API: publish → SSE
        API-->>C: event: assistant / reasoning
    end
    opt LLM_STREAM_INCLUDE_USAGE
        R-->>O: usage
        O->>API: publish → SSE
        API-->>C: event: usage
    end
    R-->>O: done（finish_reason，仅内部）
    Note over O: 暂存 done，不立刻 publish
    R-->>O: final → 写入 messages，run_turn 结束
    O->>API: 合并后 publish 一条 done
    API-->>C: event: done {finish_reason: stop|…}
    Note over C: 回合边界；SSE 连接不断开
```

**时序要点**：

- 前端看到的 **`assistant`** 与网关 token **同步**，无需等 `done`。
- 网关发出 **`finish_reason`** 时，runtime 已 yield **`done`**，但 **SSE 上此时尚无 `done`**；待 **`final`** 处理完、`run_turn` 退出后，编排层才 **`publish` 一条 `done`**。
- 若产品要求「`finish_reason` 一到立刻解锁输入」，需改编排层对无 `tool_calls` 路径**提前下发 `done`**（当前实现未做）。

**`finish_reason` 常见值**：**`stop`**（正常结束）、网关返回的 **`length`** / **`content_filter`** 等；映射层缺省时可能为 **`unspecified`**。

#### 2.7.2 工具调用 — 全部自动执行（无需审批）

模型决定调用工具且策略允许自动执行时：先结束模型流，再下发 **`tool_call`** 与 **`done`**，随后在**同一条 HTTP 消费上下文**内执行工具并推送 **`tool_result`**，最后**入队** `tool_result` 触发下一轮模型（新一轮 SSE 事件另起一段）。

```mermaid
sequenceDiagram
    autonumber
    participant C as 前端
    participant API as FastAPI / 总线
    participant O as 编排层 + runtime
    participant T as 工具执行

    C->>API: POST /v1/messages (human)
    loop 模型流式
        O->>API: SSE assistant / reasoning
        API-->>C: assistant / reasoning
        opt 流式 tool_calls 片段
            O->>API: SSE tool_call_delta
            API-->>C: tool_call_delta
        end
    end
    O->>API: SSE tool_call {tool_calls, assistant_content}
    API-->>C: tool_call
    O->>API: SSE done {finish_reason: tool_calls|…}
    API-->>C: done
    Note over C: 第一轮「模型+决策」结束，可展示工具卡片

    par 每个自动批准的工具
        O->>T: invoke
        T-->>O: 结果文本
        O->>API: SSE tool_result {tool_call_id, content, …}
        API-->>C: tool_result
    end
    Note over O: messages 已写入 role=tool，入队 tool_result
    O->>O: 新一轮 run_turn (tool_message)
    loop 下一轮模型流式
        O->>API: SSE assistant …
        API-->>C: assistant …
    end
    O->>API: SSE done
    API-->>C: done
```

**前端要点**：

- **`tool_call` 与 `done` 之间**不要假设还有增量 **`assistant`**（正文已在流式阶段输出完毕）。  
- **`tool_result` 可能在 `done` 之后**才到达（执行耗时）；UI 宜按 **`tool_call_id`** 更新状态，而非依赖 `done` 触发工具区渲染。  
- 多工具时会有**多条** **`tool_result`**，顺序与并发完成顺序一致。

#### 2.7.3 工具调用 — 需要人工审批

需审批的工具不会在本轮立即全部执行；先 **`approval_required`** + **`done`**，等待用户 **`resume`**。

```mermaid
sequenceDiagram
    autonumber
    participant C as 前端
    participant API as FastAPI / 总线
    participant O as 编排层

    C->>API: POST /v1/messages (human)
    loop 模型流式
        O->>API: SSE assistant / tool_call_delta …
        API-->>C: …
    end
    O->>API: SSE tool_call
    API-->>C: tool_call
    O->>API: SSE approval_required {approval_id, approval_args, …}
    API-->>C: approval_required
    O->>API: SSE done {finish_reason: tool_calls|…}
    API-->>C: done
    Note over C: 展示审批；禁用普通发送或切换为 /yes /no / resume API

    opt 同轮另有「自动批准」工具
        O->>API: SSE tool_result（自动项）
        API-->>C: tool_result
    end

    C->>API: POST /v1/messages (resume, 批准/拒绝)
    alt 批准
        loop 每个批准项
            O->>API: SSE tool_result
            API-->>C: tool_result
        end
        O->>O: 入队 tool_result → 新一轮模型流
        O->>API: SSE assistant … → done
        API-->>C: …
    else 拒绝
        O->>API: SSE error
        API-->>C: error
        O->>API: SSE done {finish_reason: resume_rejected}
        API-->>C: done
    end
```

**前端要点**：收到 **`approval_required`** 后应进入「待审批」状态；**`done` 表示模型轮次结束，不等于工具已执行完**。用户提交 **`resume`** 后才会出现批准项的 **`tool_result`** 及可能的下一轮 **`assistant`**。

#### 2.7.4 异步工具完成回灌

长耗时工具在后台跑完后，由服务 **`async_tool_result`** 入队，SSE 上会先补发合成的 **`tool_call`** + **`tool_result`**，再视上下文进入 **`tool_message`** 新一轮模型流。

```mermaid
sequenceDiagram
    participant C as 前端
    participant API as FastAPI / 总线
    participant O as 编排层

    Note over C,O: 此前已建立 SSE；异步 job 在后台执行
    O->>API: SSE tool_call（合成，name 常为 tool_callback）
    API-->>C: tool_call
    O->>API: SSE tool_result {async 终态 content}
    API-->>C: tool_result
    opt 需要继续推理
        loop 新一轮模型
            O->>API: SSE assistant …
            API-->>C: assistant …
        end
        O->>API: SSE done
        API-->>C: done
    end
```

#### 2.7.5 用户在「待工具/待审批」阶段插入新消息

若 **`pending_tool_calls` 非空**时又来 **`human`** 消息，编排层会先为每个 pending 补发打断型 **`tool_result`**（**`interrupted_by_user_message: true`**），再进入新的 **`human_message`** 模型轮。

```mermaid
sequenceDiagram
    participant C as 前端
    participant O as 编排层

    C->>O: POST message（新 human）
    loop 每个 pending tool
        O->>C: SSE tool_result {interrupted_by_user_message: true}
    end
    O->>C: SSE assistant … → tool_call? → done
```

### 2.8 前端处理清单（建议）

| 关注点 | 做法 |
|--------|------|
| **回合边界** | 以 **`done`** 为一轮处理的结束信号（**`finish_reason`** 区分 **`stop` / `tool_calls` / `error` / `resume_*`**）；**不要**在 `done` 后关闭 EventSource（**Go Node**：`turn_complete` + `awaiting`，见 [agent-node-api.md §2.4.1](./architecture/agent-node-api.md)） |
| **流式正文** | **`assistant` / `reasoning`**：按事件 **追加** `content`；收到 **`tool_call` / `approval_required` / `error`** 时结束当前流式块 |
| **工具 UI** | **`tool_call`** 建卡 → **`tool_result`** 按 **`tool_call_id`** 填结果 → 可选在 **`done` 后**显示 loading（执行中） |
| **审批** | 仅 **`approval_required`** 打开审批；提交 **`resume`** 后再监听 **`tool_result`** 与下一轮 **`assistant`** |
| **增量 tool 参数** | **`tool_call_delta`** 可选展示；**执行与审批以 `tool_call` 为准** |
| **多会话** | 同一 **`connection_id`** 一条 SSE，用顶层 **`session_id`** 路由到对应会话面板 |
| **字段表** | 各 `type` 的扁平字段以 [api-reference.md](./api-reference.md) **§4.1** 为准 |

### 2.9 端到端时序图（入站 + 出站）

```mermaid
sequenceDiagram
    autonumber
    participant C as 客户端
    participant API as FastAPI
    participant Q as MessageQueue(session)
    participant S as AgentService
    participant BUS as InMemoryEventBus

    C->>API: GET /v1/streams?connection_id=cid
    API->>BUS: subscribe_all(connection_id=cid)

    C->>API: POST /v1/messages (session_id, connection_id=cid, ...)
    API->>S: submit_message / submit_resume → enqueue

    S->>Q: receive() 出队
    S->>S: _handle_message → 编排 / runtime
    S->>API: handle_stream_event(env, type, data)
    Note over S,API: env.connection_id 非空才继续
    API->>BUS: publish(connection_id, session_id, type, data)
    BUS-->>API: StreamEvent → SSE 帧
    API-->>C: event: assistant / done / ...
```

---

## 第三部分：与观测、其它文档的交叉引用

- **Prometheus**：队列积压等若暴露为指标，见 [prometheus-metrics.md](./prometheus-metrics.md)。  
- **子目录维护笔记**：**`app/harness/streaming/README.md`**（实现细节与早期设计笔记）；**`app/harness/queue/REFERENCE.md`**（符号索引）。

---

实现若有变更，以 **`app/harness/queue/message_queue.py`**、**`app/harness/service/agent_service.py`**、**`app/harness/api/app.py`**、**`app/harness/streaming/events.py`** 为准；本文随行为演进需同步修订。
