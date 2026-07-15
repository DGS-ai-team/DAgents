# Go Agent Node 内部结构

本文说明 **`node/`** 内会话运行时核心组件的职责与协作关系：**Manager**、**runtime**、**MessageQueue**、**Orchestrator**，以及它们与 HTTP/SSE、工具、持久化的边界。

实现细节见代码目录 README：

- [`node/internal/session/README.md`](../../node/internal/session/README.md)
- [`node/internal/turn/README.md`](../../node/internal/turn/README.md)
- [`node/internal/queue/`](../../node/internal/queue/)（`queue.go`、`envelope.go`）

对外 HTTP/SSE 契约见 [agent-node-api.md](./agent-node-api.md)。

---

## 1. 组件层级关系

下图表示 **拥有 / 包含**（实线框嵌套）与 **调用 / 委托**（箭头）。自上而下：进程入口 → 会话表 → 单 session 运行时 → 队列与编排器 → 外部依赖。

```mermaid
flowchart TB
    subgraph Process["Go Agent Node 进程"]
        API["internal/api<br/>HTTP · SSE 路由"]

        subgraph ManagerLayer["L1 · session.Manager"]
            MGR["Manager<br/>sessions map[id]→runtime<br/>共享 LLM · Registry · policy · TurnOptions"]
        end

        subgraph RuntimeLayer["L2 · session.runtime（每 session 一个）"]
            direction TB
            RT["runtime<br/>session 元数据 · agentID"]

            subgraph OwnedByRuntime["runtime 直接拥有"]
                direction LR
                Q["L3 · queue.MessageQueue<br/>优先级入队 / 出队"]
                LOOP["consumeLoop<br/>（goroutine）"]
                STATE["会话状态<br/>messages · pending HITL<br/>toolLoopCount · loadedSkills"]
                ORCH["L3 · turn.Orchestrator<br/>字段 orch"]
            end

            subgraph RuntimeServices["runtime 外围（同层协作）"]
                direction LR
                STORE["store.SQLiteStore"]
                COMP["compression.Coordinator"]
                CAT["skills.Catalog"]
            end

            RT --> Q
            RT --> LOOP
            RT --> STATE
            RT --> ORCH
            RT --> STORE
            RT --> COMP
            RT --> CAT
        end

        HUB["stream.Hub<br/>（Manager 级 SSE 总线）"]
    end

    subgraph External["L4 · 注入依赖（非 session 独有）"]
        LLM["llm.Client"]
        TOOLS["tools.Executor<br/>Registry / RestrictedRegistry"]
        POL["policy.Engine"]
    end

    API -->|"EnqueueMessage 等"| MGR
    MGR -->|"1 : N"| RT
    LOOP -->|"Dequeue → handle*"| ORCH
    ORCH -->|"SetToolResultEnqueuer"| Q

    ORCH --> LLM
    ORCH --> TOOLS
    ORCH --> POL
    ORCH -->|"Publish SSE"| HUB
    RT -->|"MaybeHandle 压缩"| COMP
    RT -->|"persist"| STORE
```

**读图要点**：

| 关系 | 说明 |
|------|------|
| **Manager → runtime** | 一对多；`Create` / `SpawnChild` 时插入 `sessions` 表 |
| **runtime → MessageQueue** | 每个 runtime **独占** 一个队列；Manager 不直接操作队列 |
| **runtime → Orchestrator** | `orch` 是 runtime 的**成员字段**；Orchestrator **不包含** runtime |
| **consumeLoop → Orchestrator** | 队列消费者 **调用** orchestrator 的单步 API，而非 orchestrator 消费队列 |
| **Orchestrator → Queue** | 仅通过 `enqueueToolResult` **回调入队** `tool_result`，不拥有队列 |
| **Hub / LLM / Tools** | Manager 构造时注入，经 runtime 传给 Orchestrator 或步前压缩使用 |

父 session 与临时子 session **层级相同**（都是 `Manager` 下的一条 `runtime`）；子 runtime 的差异是 `RelayHub`、`RestrictedRegistry`、`store=nil`，图中未单独展开。

### 1.1 单 session 内调用方向（时序）

与上图「谁拥有谁」互补：一条 user 消息从入队到模型/工具的大致顺序。

```mermaid
sequenceDiagram
    participant API as internal/api
    participant M as Manager
    participant RT as runtime
    participant Q as MessageQueue
    participant L as consumeLoop
    participant O as Orchestrator

    API->>M: EnqueueMessage
    M->>RT: enqueue(human)
    RT->>Q: Enqueue
    L->>Q: Dequeue
    Q-->>L: Envelope
    L->>RT: handleHumanMessage
    RT->>O: RunHumanMessageTurn
    O-->>RT: ScheduleToolResult
    RT->>Q: Enqueue(tool_result)
    L->>Q: Dequeue
    L->>RT: handleToolResult
    RT->>O: RunToolMessageTurn
```

---

## 2. 组件总览（文字）

```text
HTTP / CLI（internal/api）
        │ EnqueueMessage / EnqueueResume / …
        ▼
  session.Manager  ──sessions[id]──►  runtime × N
                                        ├─ MessageQueue + consumeLoop
                                        ├─ messages / pending / skills
                                        ├─ store · compression · catalog
                                        └─ orch *Orchestrator ──► LLM · Tools · Hub
```

**包含关系**：`runtime` **拥有** `Orchestrator`（字段 `orch`），**不是** orchestrator 包含 runtime。  
`Orchestrator` 不持有 session 表或队列；每次 turn 由 runtime 传入 `history` 指针与 `sessionID`。

---

## 3. 四层职责

| 层级 | 包 / 类型 | 职责 |
|------|-----------|------|
| **会话表** | `session.Manager` | 创建/恢复/删除 session；对外入队 API；skills 管理；子 Agent `SpawnChild`（`manager_child.go`） |
| **会话运行时** | `session.runtime` | 每 session 独立队列与 consumer；维护 messages、HITL、tool 循环计数；步前压缩；SQLite 持久化 |
| **消息队列** | `queue.MessageQueue` | 进程内优先级队列；**无内嵌 consumer**（与 Python v1 相同约定） |
| **Turn 编排** | `turn.Orchestrator` | 单步：system prompt → LLM 流式 → 工具分流/执行 → SSE；`tool_result` 续跑由 runtime 再次调用 |

---

## 4. MessageQueue

**路径**：`node/internal/queue/`

每个 `runtime` 在构造时创建 **一个** `MessageQueue`。`Manager.EnqueueMessage` 等 API 最终调用 `runtime.enqueue`。

### 4.1 入队类型（`Envelope.RequestType`）

| 类型 | 常量 | 典型来源 | consume 处理 |
|------|------|----------|--------------|
| 用户消息 | `message` | `POST /v1/messages` | `handleHumanMessage`（步首 Apply 缓冲） |
| HITL 恢复 | `resume` | 审批 / `ask_user_information` 提交 | `handleResume` |
| 工具续跑 | `tool_result` | orchestrator 经 `SetToolResultEnqueuer` 回调 | `handleToolResult` |
| 旁路续跑 | `side_effect_continue` | Produce 后 / Cancel 恢复 | `handleSideEffectContinue` |
| 异步工具完成 | `async_tool_result` | 后台 job 完成 | `handleSideEffectProduceAsync`（Produce） |
| Trigger | `trigger_message` | trigger fire | `handleSideEffectProduceExternal` |
| A2A inbox | `a2a_inbox_message` | Manage inbox | `handleSideEffectProduceExternal` |

### 4.2 优先级（`Priority`）

高优先级项先出队（同优先级 FIFO 按 `seq`）。实现见 `queue.go` → `priorityValue`。

```text
side_effect_continue(-1) = tool_result(-1) > human(0) > resume(1) > async_completion(2) > other(10)
```

| 档位 | 典型 `request_type` | 说明 |
|------|----------------------|------|
| `tool_result` | `tool_result` / `side_effect_continue` | 同步工具闭合续跑；旁路 Apply 后续跑 LLM |
| `human` | `message` | 新 user 输入；**高于**排队的 `resume` |
| `resume` | `resume` | HITL 提交 |
| `async_completion` | `async_tool_result` | 后台 job **Produce**（缓冲 + SSE） |
| `other` | `trigger_message` / `a2a_inbox_message` | trigger / A2A inbox **Produce** |

**注意**：`async_tool_result` 等在 pending HITL 期间仍会被 **Produce**，但 **不** inline 改 history（`sideEffectStore`）。open batch 下 Apply 在步首或 TaskComplete/Cancel 时 continue。见 [turn-side-effects-refactor.md](../design/turn-side-effects-refactor.md)。

### 4.3 与 turn 的关系

队列负责 **「何时跑下一步」**；orchestrator 负责 **「这一步跑什么」**。  
生产路径下，一次 `handleHumanMessage` 通常只执行 **一步** `RunHumanMessageTurn`；若产生工具调用且需续跑，orchestrator 返回 `ScheduleToolResult=true`，runtime 入队 `tool_result`，consumer 稍后调用 `RunToolMessageTurn`。时序见图 **[§1.1](#11-单-session-内调用方向时序)**。

---

## 5. runtime

**路径**：`node/internal/session/runtime.go`（主体）、`runtime_child.go`（子 session）

### 5.1 构造

- **父 session**：`Manager.Create` → `newRuntime` → `newRuntimeWithPublisher`
- **子 session**（临时 Agent）：`SpawnChild` → `newChildRuntime` → 同一 `newRuntimeWithPublisher`，但换用 `RelayHub`、`RestrictedRegistry`、`store=nil`

`newRuntimeWithPublisher` 内：

1. 创建 `skills.Catalog`、`compression.Coordinator`、`history.Journal`
2. `rt.orch = turn.NewOrchestrator(...)`
3. `rt.orch.SetToolResultEnqueuer(rt.enqueueToolResult)`

### 5.2 内存状态（runtime 权威）

| 字段 | 说明 |
|------|------|
| `messages` | OpenAI 格式对话历史 |
| `pending` | 等待 HITL 的 `PendingHITL` |
| `toolLoopCount` | 当前 human message 链上的工具步计数 |
| `state` | `idle` / `model_streaming` / `awaiting_tool` |
| `loadedSkills` | 已加载 skill 元数据（持久化到 SQLite，子 session 不持久化） |

Orchestrator 通过 `SkillAccess{Get, Set}` 回调读写 `loadedSkills`。

### 5.3 consumeLoop

单 goroutine 串行处理同一 session 上所有出队项；新 `message` 到达时若存在 pending HITL，先 `InterruptPending` 再开新 turn。

**父 session 专有**：`handleHumanMessage` / `handleToolResult` 步前可调用 `compression.MaybeHandle`。  
**子 session**：跳过压缩；空闲且末条为 assistant 时 `tryCompleteChildIfIdle` → `childagent.OnChildSettled`。

---

## 6. Orchestrator

**路径**：`node/internal/turn/orchestrator.go`

### 6.1 依赖注入（`NewOrchestrator`）

| 参数 | 作用 |
|------|------|
| `hub` | SSE 发布（父用 `*stream.Hub`，子用 `*childagent.RelayHub` 转发到父 `session_id`） |
| `client` | `llm.Client` 流式补全 |
| `toolExec` | `tools.Executor`（父：完整 Registry；子：白名单 `RestrictedRegistry`） |
| `policy` | 工具 auto / approval / deny |
| `skillAccess` | skills 目录与 loaded 读写 |
| `promptCtx` | `.runtime/prompt_context/` 侧车（子 session 当前与父共用构造路径，见改进项） |
| `maxToolLoops` | 单条 user 消息内工具循环上限 |

事后设置：

- `SetToolResultEnqueuer` — 工具步结束后入队 `tool_result`
- `SetChildAgentManager(mgr)` — 父 session 注入管理器，可 `create_temporary_agent`
- `SetChildSession(true)` — 子 session 禁止管理类工具

### 6.2 单步主路径（`runOneStep`）

1. `buildSystemPrompt(sessionID)` → [`BuildSystemPrompt`](../../node/internal/turn/prompt.go)
2. `llm.StreamChat` → 推送 `assistant` / `reasoning` / `usage`
3. 若有 `tool_calls` → `processToolCalls`（policy、临时 Agent 工具、skills 工具、auto 批执行）
4. 无待处理 HITL 且需续跑 → `ScheduleToolResult=true`
5. 回合结束 → `done` SSE（语义见 [agent-node-api.md](./agent-node-api.md) §2.4.1）

### 6.3 与 runtime 的边界

| 事项 | 归属 |
|------|------|
| 队列、取消 turn、持久化 | runtime |
| LLM 请求、工具执行、SSE 事件类型 | orchestrator |
| 压缩触发时机 | runtime 在调用 orchestrator **之前** |
| SQLite 写入 | runtime `persist`（orchestrator 可选写 raw journal） |

---

## 7. 父 session 与 子 session

| 项 | 父 | 子（临时 Agent） |
|----|-----|------------------|
| 创建 | `Manager.Create` | `childagent` → `SpawnChild` |
| Orchestrator | `NewOrchestrator` + `SetChildAgentManager` | 同上 + `SetChildSession(true)` |
| SSE | 直接 `Hub.Publish(sessionID)` | `RelayHub` → 父 `session_id` |
| 持久化 | SQLite | 无 |
| 压缩 | 可选 | 关闭 |

详见 [child-agent-tools.md](./child-agent-tools.md)。

---

## 8. 从 HTTP 到 SSE 的端到端路径

```text
POST /v1/sessions/{id}/messages
  → Manager.EnqueueMessage
  → runtime.enqueue(PriorityHuman)
  → consumeLoop → handleHumanMessage
  → orch.RunHumanMessageTurn
  → [可选] enqueue tool_result → handleToolResult → RunToolMessageTurn
  → persist
  → Hub 事件 → GET /v1/streams（Client 订阅）
```

Resume、异步工具、trigger 入队路径见 `runtime.consumeLoop` 的 `switch` 分支。

---

## 9. 相关文档

| 文档 | 内容 |
|------|------|
| [agent-node-api.md](./agent-node-api.md) | HTTP 路径、SSE `type`、`done` 语义 |
| [child-agent-tools.md](./child-agent-tools.md) | 临时子 Agent 工具与生命周期 |
| [local-assistant.md](./local-assistant.md) | Node + Client 联调 |
| [context-compression-cache-analysis.md](../design/context-compression-cache-analysis.md) | 压缩与 prompt 侧车（Go：`compression` 包） |
| [design/agent-hooks.md](../design/agent-hooks.md) | **Hook 扩展点**（已落地）：Registry、phase 锚点、与 runtime/Orchestrator 映射 |
