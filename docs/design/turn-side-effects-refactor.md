# Turn 旁路侧效应重构：场景规格（历史记录）

> **文档性质**：历史设计与验收记录，不是当前运行时契约。本文保留旧场景的推导过程；当前 InputBox、Turn cancel、同步工具和异步浏览器任务语义以架构文档及代码为准。
>
> **状态**：**缓冲门控已实现**（`sideEffectStore` + `side_effect_continue` + Client SSE）  
> **关联**：[Issue #32](https://github.com/DGS-ai-team/DAgents/issues/32)（async + open batch）  
> **代码基准**：`dev` 合并 PR #33 之后

> **Turn/Step 重构说明（2026-08）**：下文场景表中的 `pending`、`state`、`toolLoopCount` 是规格编写时的状态记号；当前代码以 `turn.Coordinator` 的生命周期事件和投影为准，`StepOutcome.LoopCount` 已替换为 `StepOutcome.StepIndex`。旁路 Produce/Apply/Continue 的行为不变。

> **后续语义修订（2026-08-27）**：本文的旁路场景和第 1.3、6–10 节保留为历史设计/验收背景，不再完整代表当前运行时。现行实现中，user/trigger/A2A 输入进入 session `InputBox` FIFO；活动 Turn（包括审批等待）期间只排队，普通输入不会调用 `InterruptPending`，只有显式 `turn cancel` 才会取消当前 Turn。`ask_user_information` 仍通过 typed `resume` 回答；`bash_run` 始终同步执行，超时直接失败，不创建后台 job。当前规范以 [`docs/architecture/go-node-internals.md`](../architecture/go-node-internals.md) 和 session/tools README 为准。

## 实现摘要（2026-06）

| 阶段 | 行为 |
|------|------|
| **Produce** | `async_tool_result` / `trigger_message` / `a2a_inbox_message` → 缓冲 + 立即 SSE；不改 `runtime.messages` |
| **Apply** | `runTurnStepWithSideEffects` 步首 `ApplyReady`；≥2 条合并为 `get_callback` batch |
| **Lifecycle fact** | Apply 同步向 `TurnCoordinator` 写入 `external.fact.recorded`；callback history 只服务模型上下文，不进入当前 ToolBatch，也不触发 ToolExecution |
| **Continue** | `side_effect_continue`（priority -1）→ `PublishSideEffectTurnStart` → LLM |
| **Cancel** | 无 pending + 有缓冲 → schedule continue；ClearContext/Delete 丢弃缓冲 |
| **Client** | `user_message_deferred`、`side_effect_turn_start` → passive `beginImplicitTurn`；`side_effect_applied` / `side_effects_cleared` → 标记 Produce 行已入库/已失效 |

### 验收测试（2026-06）

| 场景 | 测试文件 |
|------|----------|
| TaskComplete 判定 | `turn/task_complete_test.go` |
| InsertSite 五分支 | `turn/side_effect_insert_site_test.go` |
| 合并 `get_callback` batch | `turn/merged_callback_batch_test.go`、`turn/side_effect_messages_test.go` |
| Produce 不破坏 open batch / pending | `session/runtime_async_open_batch_test.go`、`session/runtime_async_hitl_test.go` |
| Cancel 三分法 / Clear 丢弃 / human 抢占 | `session/side_effects_cancel_test.go`（含 `side_effect_turn_start` / `side_effect_applied` / `side_effects_cleared` SSE） |
| FIFO / continue 去重 / A2A 空 history | `session/side_effects_test.go` |
| trigger 入队与 delivery | `session/triggers_test.go` |
| Client 被动 turn + applied/cleared UX | `client/.../turn_gate_test.go`、`client/.../stream_events_turn_test.go`、`client/.../side_effect_format_test.go`、`tests/test_cli_session_controller.py` |

---

## 1. 背景与目标

同步 tool loop（场景一）在单 consumer、单步 `runTurnStep` 下大体正确。引入 **async 工具回灌**、**trigger 投递**、**HITL 拆批** 后，旧实现曾在 logical turn 未结束时 inline 改 `history`，且 async 存在 **inline 续跑** 与 **`tool_result` 队列续跑** 双轨（Issue #32）。

**当前**（见文首实现摘要）：旁路事件仅 **Produce**（缓冲 + SSE），**Apply** 在步首/Continue 前写入，续跑经 **`side_effect_continue`**。下文场景规格仍作验收基准；§12 对照旧目标与现实现。

### 1.1 目标不变量

| ID | 不变量 | 含义 |
|----|--------|------|
| **I** | Turn 临界区内 history 单写者 | 单次 `runTurnStep` 内，history 仅由当前 orchestrator 路径（及步前压缩）修改；步末 `applyStepOutcome` 一次性 commit 到 `r.messages`。 |
| **II** | 旁路不丢 | async / trigger 等入队事件最终写入 history 或带 epoch 明确丢弃；不得静默消失。 |

### 1.2 符号

| 符号 | 含义 |
|------|------|
| `H*` | `runtime.messages`（handler 之间的持久视图） |
| `h*` | `runTurnStep` 内局部 `history`（步内单写者，步末 commit） |
| `Q` | `MessageQueue` 深度；条目写 `request_type` |
| `U` | 本轮 user 消息 |
| `A(tc)` | 带 `tool_calls` 的 assistant |
| `A(text)` | 纯文本 assistant（无 tool_calls） |
| `T` | tool 结果消息 |

### 1.3 队列优先级（入队顺序相同时按 seq）

| Priority | 值 | request_type |
|----------|-----|--------------|
| `side_effect_continue` / `tool_result` | -1 | `side_effect_continue` / `tool_result` |
| human | 0 | `message`（含 CLI/API user） |
| `resume` | 1 | `resume` |
| async | 2 | `async_tool_result` |
| other | 10 | `trigger_message` / `a2a_inbox_message` |

### 1.4 关键代码入口

| 路径 | 文件 |
|------|------|
| 消费循环 | `node/internal/session/runtime.go` → `consumeLoop` |
| 步脚手架 | `node/internal/session/runtime_turn.go` → `runTurnStep` |
| history commit | `runtime.go` → `applyStepOutcome` |
| 单步 LLM + 工具 | `node/internal/turn/orchestrator.go` → `runOneStep` |
| 工具批 + enqueue | `orchestrator.go` / `tool_router.go` → `processToolCalls` |
| async 旁路 | `session/side_effects.go` → Produce / ApplyReady；`runtime_side_effects.go` |

---

## 2. 场景索引

| # | 场景 | 相对场景一的主要差异 | 文档状态 |
|---|------|----------------------|----------|
| **1** | human → 单 tool(auto) → tool_result → assistant 终稿 | 基准串行路径 | ✅ §3 |
| **2** | human → 直接 assistant 终稿（无 tool） | 仅一次 handler；Q 无 `tool_result` | ✅ §4 |
| **3** | 多 tool auto batch | 一步内多个 `T`；仍一次 `tool_result` enqueue | ✅ §5 |
| **4** | auto + approval 拆批 + pending | open batch；Q 不因 pending 暂停 | ✅ §6 |
| **5** | resume 后继续 | `resume` → 写 tool → `ScheduleToolResult` 续跑 | ✅ §7 |
| **6** | bash background + async 回灌 | Produce → Apply → `side_effect_continue` | ✅ §8 |
| **7** | trigger message | `InputBox FIFO`；可 `InterruptPending` | ✅ §9 |
| **8** | 新 human 打断 pending | 步前 `InterruptPending` 改 H | ✅ §10 |

场景 **4、6** 为 Issue #32 与 defer 重构的主要靶点（**已实现**，见文首测试矩阵）。

---

## 3. 场景一：Human → 单 tool(auto) → tool_result → assistant 终稿

### 3.1 前提

- 单 session，`consumeLoop` 空闲。
- 用户 `POST /v1/messages`，`request_type=message`。
- 模型**第一步**返回 **1 个** policy-auto 工具（无 HITL、非 background）。
- 模型**第二步**无 `tool_calls`，`finish_reason=stop`。
- 上下文压缩未改变 message 条数（或本场景忽略压缩）。

### 3.2 总览时序

```mermaid
sequenceDiagram
    participant API as POST /v1/messages
    participant Q as MessageQueue
    participant Loop as consumeLoop
    participant RTS as runTurnStep
    participant Orch as Orchestrator

    API->>Q: Enqueue(message, human)
    Loop->>Q: Dequeue message
    Loop->>RTS: handleHumanMessage
    RTS->>Orch: RunHumanMessageTurn → runOneStep
    Note over Orch: h: +user +assistant(tc) +tool
    Orch->>Q: Enqueue(tool_result) 注：步内入队，handler 未结束
    RTS->>Loop: applyStepOutcome → H commit
    Loop->>Q: Dequeue tool_result
    Loop->>RTS: handleToolResult
    RTS->>Orch: RunToolMessageTurn → runOneStep
    Note over Orch: h: +assistant(纯文本)
    RTS->>Loop: applyStepOutcome → H commit
```

### 3.3 阶段 A：入队（API → 队列）

| 时刻 | 事件 | Q | H | runtime |
|------|------|---|---|---------|
| A0 | 空闲 | `[]` | `H₀` | `state=idle`, `pending=nil`, `toolLoopCount=0` |
| A1 | `EnqueueMessage(message)` | `[message]` | `H₀` | 不变 |

### 3.4 阶段 B：第一次 handler — `handleHumanMessage`

| 子阶段 | 代码路径 | h（步内） | H（commit 前） | Q | SSE（Client） |
|--------|----------|-----------|----------------|---|---------------|
| B1 | Dequeue → `handleHumanMessage` | — | `H₀` | `[]` | — |
| B2 | 步前 `toolLoopCount=0`；`runTurnStep` 开始，`history := r.messages` | `h = H₀` | `H₀` | `[]` | — |
| B3 | `RunHumanMessageTurn` → `appendHistory(user)` | `H₀ + U` | `H₀` | `[]` | — |
| B4 | `runOneStep` → `StreamChat` → 有 `tool_calls` | `… + A₁(tc)` | `H₀` | `[]` | `assistant` 流式 → `tool_call` |
| B5 | `processToolCalls` → `executeTool` → `appendHistory(tool)` | `… + T₁` | `H₀` | `[]` | `tool_result` |
| B6 | batch 闭合、无 pending → **`enqueueToolResult()`**（在 `runOneStep` 内） | 同上 | `H₀` | **`[tool_result]`** | — |
| B7 | `runOneStep` 返回 `StepOutcome{LoopCount:1}`（无 `ScheduleToolResult`） | 同上 | `H₀` | `[tool_result]` | — |
| B8 | `applyStepOutcome` **commit** | — | **`H₁ = H₀+U+A₁+T₁`** | `[tool_result]` | — |
| B9 | handler 结束；`ScheduleToolResult==false` → `persist` | — | `H₁` | `[tool_result]` | 第一步结束后 Client 已见工具 SSE；`turn_finished` 在 B5–B6 附近由 orchestrator 推送（本路径无 pending） |

**要点**

1. **History 在 B8 才写入 `r.messages`**；B4–B7 只改局部 `h`。
2. **`tool_result` 在 B6 入队**，但 `consumeLoop` 仍阻塞在 B1–B9，**不会**在 B6 立刻消费。
3. 生产路径由 orchestrator 内 **`SetToolResultEnqueuer` → `enqueueToolResult`** 入队；`StepOutcome.ScheduleToolResult` 主要用于 `ContinueAfterResume` 等路径，本场景 handler **不会**再调 `scheduleToolResult`。
4. `applyStepOutcome` 将 `toolLoopCount` 置 **0**；下一步 `handleToolResult` 快照为 0，由 `runOneStep` 内 `++` 变为 1。

**B8 后 history 形态**

```text
H₁ = [ … , user(U), assistant(tool_calls=[tc₁]), tool(tc₁ → result) ]
```

### 3.5 阶段 C：第二次 handler — `handleToolResult`

| 子阶段 | 代码路径 | h | H | Q |
|--------|----------|---|---|---|
| C1 | Dequeue `tool_result` | — | `H₁` | `[]` |
| C2 | `toolLoopCountSnapshot()` → 0；`runTurnStep`，`history := H₁` | `h = H₁` | `H₁` | `[]` |
| C3 | `RunToolMessageTurn` → `runOneStep`（**不**追加 user） | `h = H₁` | `H₁` | `[]` |
| C4 | `StreamChat` → 无 `tool_calls` | `… + A₂(text)` | `H₁` | `[]` |
| C5 | `publishTurnFinished(stop)` | 同上 | `H₁` | `[]` |
| C6 | `StepOutcome{LoopCount:1}` | 同上 | `H₁` | `[]` |
| C7 | `applyStepOutcome` commit | — | **`H₂ = H₁ + A₂`** | `[]` |
| C8 | 无后续 enqueue → `persist`，idle | — | `H₂` | `[]` |

**C7 后 history 形态**

```text
H₂ = [ … , user(U), assistant(tc₁), tool(T₁), assistant(A₂ 终稿) ]
```

### 3.6 消息队列轨迹（汇总）

```text
A1:  [message]
B1:  []                    ← dequeue message
B6:  [tool_result]         ← runOneStep 内 enqueue（human handler 未返回）
B9:  [tool_result]
C1:  []                    ← dequeue tool_result
C8:  []                    ← 结束
```

### 3.7 History 变更轨迹（汇总）

```text
H₀
  │  handleHumanMessage / runTurnStep #1（步内 h，B8 commit）
  ▼
H₀ + user + assistant(tc) + tool(result)
  │  handleToolResult / runTurnStep #2（C7 commit）
  ▼
H₀ + user + assistant(tc) + tool(result) + assistant(终稿)
```

### 3.8 Runtime 状态轨迹

| 阶段 | `state` | `pending` | `toolLoopCount` |
|------|---------|-----------|-----------------|
| A0 | idle | nil | 0 |
| B2–B7 | model_streaming → awaiting_tool（步内短暂） | nil | 0（步内 runOneStep 计数为 1） |
| B8 | idle | nil | 0 |
| C2–C6 | model_streaming | nil | 0 → 步内 1 |
| C8 | idle | nil | 0 |

### 3.9 本场景成立的不变量

- 每个 handler 内：仅 `runTurnStep` 改 history，步末一次 commit。
- **tool 写 history** 与 **续跑 LLM** 之间：先 commit `H₁`，再 dequeue `tool_result`。
- **batch 闭包**：第二次 LLM 前，`assistant(tc)` 的全部 `tool_calls` 已有对应 `tool` 消息。
- 续跑**仅**经 `tool_result` 队列，无 inline `RunToolMessageTurn`。

---

## 4. 场景二：Human → 直接 assistant 终稿（无 tool）

### 4.1 前提

- `consumeLoop` 空闲。
- 用户 POST `message`。
- 模型**第一步** `StreamChat` 返回纯文本，**无** `tool_calls`。
- 无 compression 改写（或忽略）。

### 4.2 总览时序

```mermaid
sequenceDiagram
    participant API as POST /v1/messages
    participant Q as MessageQueue
    participant Loop as consumeLoop
    participant RTS as runTurnStep
    participant Orch as Orchestrator

    API->>Q: Enqueue(message)
    Loop->>Q: Dequeue message
    Loop->>RTS: handleHumanMessage
    RTS->>Orch: RunHumanMessageTurn → runOneStep
    Note over Orch: h: +user +assistant(text)
    Orch->>Loop: publishTurnFinished(stop)
    RTS->>Loop: applyStepOutcome → H commit
    Note over Q: 无 tool_result 入队
```

### 4.3 阶段轨迹

| 子阶段 | 事件 | h / H | Q | runtime（步末） |
|--------|------|-------|---|-----------------|
| A1 | 入队 `message` | `H₀` | `[message]` | idle |
| B1 | Dequeue → `handleHumanMessage` | `H₀` | `[]` | — |
| B2 | `appendHistory(user)` + `runOneStep` | `H₀+U+A(text)` | `[]` | 步内 streaming |
| B3 | 无 tool_calls → `publishTurnFinished(stop)` | 同上 | `[]` | — |
| B4 | `StepOutcome{LoopCount:1}`；**不** enqueue | 同上 | `[]` | — |
| B5 | `applyStepOutcome` commit | **`H₁=H₀+U+A(text)`** | `[]` | idle, pending=nil, toolLoopCount=0 |
| B6 | `persist` | `H₁` | `[]` | — |

### 4.4 队列 / History 汇总

```text
Q:  [message] → [] → （全程 []）

H:  H₀ → H₀ + user + assistant(终稿)
```

### 4.5 相对场景一

- **仅 1 次** `handleHumanMessage`，无 `handleToolResult`。
- `runOneStep` 在 `len(result.ToolCalls)==0` 分支直接结束，**从不**调用 `enqueueToolResult`。
- Client 仅见一轮 `assistant` + `turn_finished(turn_complete=true)`。

### 4.6 不变量

- 成立：步内单写者 + 单次 commit。
- 无 open batch、无旁路插队。

---

## 5. 场景三：Human → 多 tool auto batch → tool_result → assistant 终稿

### 5.1 前提

- 模型第一步返回 **N 个**均为 policy-auto 的 `tool_calls`（如 `read_file` ×2）。
- `processToolCalls` 将它们全部放入 `autoCalls`，**无** approval / ask_user。
- `executeAutoBatch`：goroutine 并行 `invokeTool`，`wg.Wait` 后**按原始顺序**依次 `appendHistory(tool)`。
- 第二步 LLM 无 tool_calls。

### 5.2 总览时序

与场景一相同，差别仅在 B5：`processToolCalls` 一步写入 `T₁…Tₙ`，仍只 **enqueue 一次** `tool_result`。

### 5.3 第一次 handler 关键子阶段

| 子阶段 | 事件 | h（步内累积） | Q |
|--------|------|---------------|---|
| B4 | `StreamChat` → `A(tc=[tc₁…tcₙ])` | `H₀+U+A` | `[]` |
| B5 | `executeAutoBatch` 并行执行 | 同上（执行中） | `[]` |
| B5′ | `wg.Wait` 后顺序 append | `…+T₁+…+Tₙ` | `[]` |
| B6 | batch 闭合 → `enqueueToolResult()` | 同上 | `[tool_result]` |
| B8 | commit | **`H₁=H₀+U+A+T₁…Tₙ`** | `[tool_result]` |

**B8 后 history**

```text
H₁ = [ … , user, assistant(tc₁…tcₙ), tool(tc₁), …, tool(tcₙ) ]
```

### 5.4 第二次 handler

与场景一 §3.5 相同：`handleToolResult` → `+A(终稿)` → `H₂`。

### 5.5 队列 / History 汇总

```text
Q:  [message] → [] → [tool_result] → [] → []

H:  H₀ → H₀+U+A+T₁…Tₙ → …+assistant(终稿)
```

### 5.6 要点

- 并行仅发生在 **tool 执行**；history append 在 `wg.Wait` 之后 **同步、有序**，不会出现「队列已消费 async 而 batch 写一半」。
- 仍只 **一条** `tool_result` 触发下一步 LLM。

### 5.7 不变量

- batch 在第一次 `runOneStep` 内 **完全闭合** 后才 enqueue。
- 与场景一相同的 commit / dequeue 顺序。

---

## 6. 场景四：Auto + approval 拆批 → pending（open batch）

### 6.1 前提

- 模型第一步返回 **2 个** tool_calls：`tc_auto`（policy auto）、`tc_appr`（require approval）。
- `processToolCalls` 顺序：先 `executeAutoBatch([tc_auto])`，再因 `approvalCalls` 非空返回 `PendingHITL`。
- **不** enqueue `tool_result`；Client 见 `hitl_required` + `turn_state(tool_waiting)`。

### 6.2 总览时序

```mermaid
sequenceDiagram
    participant Loop as consumeLoop
    participant Orch as Orchestrator
    participant Q as MessageQueue

    Loop->>Orch: handleHumanMessage → runOneStep
    Note over Orch: +A(tc_auto,tc_appr) +T(auto)
    Orch->>Loop: pending, 无 enqueue
    Note over Loop: pending≠nil, open batch(tc_appr)
    Note over Q: 队列仍可消费 async/resume/message
```

### 6.3 第一次 handler

| 子阶段 | 事件 | h / H | Q | runtime |
|--------|------|-------|---|---------|
| B5 | `executeAutoBatch` → `T(auto)` | `H₀+U+A+T(auto)` | `[]` | — |
| B6 | `approvalCalls` → 返回 `pending` | 同上 | `[]` | — |
| B7 | `StepOutcome{Pending, LoopCount:1}` | 同上 | `[]` | — |
| B8 | commit | **`H₁`**（见下） | `[]` | **pending=approval**, toolLoopCount=**1** |

```text
H₁ = [ … , user, assistant(tc_auto, tc_appr), tool(tc_auto) ]
     ▲ open batch：tc_appr 尚无 tool 回应
```

### 6.4 队列 / History / 状态（pending 等待期）

| 项 | 值 |
|----|-----|
| Q | **`[]`**（第一步未 enqueue） |
| H | 保持 `H₁` 直至 resume / 打断 / async handler |
| `pending` | `PendingHITL{Items:[{ToolCall: tc_appr}]}` |
| `toolLoopCount` | `1` |
| `state` | idle（handler 已返回） |

**重要**：`consumeLoop` **不因 pending 暂停**；此期间入队的 `async_tool_result`、`message`、`resume` 仍会被消费。

### 6.5 子场景 4b：pending 期间 async 插队（Issue #32，**已由 Produce 缓冲修复**）

**附加前提**：`tc_auto` 为 background bash，已写 `T(ACK)`；后台 job 在 **仍等 tc_appr 审批** 时完成。

| 时刻 | 事件 | H 变化 | Q | 行为（当前） |
|------|------|--------|---|--------------|
| P0 | 处于 §6.4 状态 | `H₁` open batch | `[]` | — |
| P1 | `EnqueueAsyncToolResult` | `H₁` 不变 | `[async]` | — |
| P2 | `handleSideEffectProduceAsync` | **不改 H** | `[]` | SSE + `sideEffectStore` 缓冲 |
| P3 | 步首 / Cancel / TaskComplete 时 Apply | 步首写入 callback | `[]` | 不 inline 破坏 open batch |
| P4 | `side_effect_continue` 被动续跑 | +可能 `A*` | `[]` | 经队列，非 inline `RunToolMessageTurn` |

**旧缺陷（已移除 `HandleAsyncToolResult` inline 路径）**

- open batch 未闭合即写 callback / 续跑 LLM。
- async 续跑与同步主线双轨。

### 6.6 相对场景一

| 对比项 | 场景一 | 场景四 |
|--------|--------|--------|
| 第一步后 Q | `[tool_result]` | `[]` |
| `pending` | nil | approval |
| batch | 闭合 | **open** |
| 续跑触发 | enqueue（步内） | 等场景五 resume |

---

## 7. 场景五：Resume（审批通过）→ tool → tool_result → 续跑

### 7.1 前提

- 起始于场景四 §6.4：`H₁` open batch，`pending=approval(tc_appr)`，`toolLoopCount=1`。
- 用户 POST `resume`（approval approve `tc_appr`）。
- Resume 后第二步 LLM 无 tool_calls。

### 7.2 总览时序

```mermaid
sequenceDiagram
    participant API as POST resume
    participant Q as MessageQueue
    participant Loop as consumeLoop
    participant Orch as Orchestrator

    API->>Q: Enqueue(resume, prio=1)
    Loop->>Q: Dequeue resume
    Loop->>Orch: handleResume → ContinueAfterResume
    Note over Orch: append T(appr)
    Orch->>Loop: ScheduleToolResult=true
    Loop->>Q: scheduleToolResult → tool_result
    Loop->>Q: Dequeue tool_result
    Loop->>Orch: handleToolResult → runOneStep
    Note over Orch: +assistant(终稿)
```

### 7.3 `handleResume` 轨迹

| 子阶段 | 事件 | h / H | Q | runtime |
|--------|------|-------|---|---------|
| R1 | Dequeue `resume` | `H₁` | `[]` | pending 仍在 |
| R2 | `runTurnStep` → `ContinueAfterResume` | | | |
| R3 | `executeAutoBatch([tc_appr])` → `T(appr)` | `H₁+T(appr)` | `[]` | 步内 |
| R4 | `StepOutcome{LoopCount:1, ScheduleToolResult:true}` | 同上 | `[]` | — |
| R5 | commit | **`H₂=H₁+T(appr)`** batch **闭合** | `[]` | pending=**nil**, toolLoopCount=0 |
| R6 | `afterToolStep` → **`scheduleToolResult()`** | `H₂` | **`[tool_result]`** | — |

**与场景一差异**：续跑入队由 handler 的 `ScheduleToolResult` + `scheduleToolResult()` 完成，**不是** orchestrator 内 `enqueueToolResult`（`ContinueAfterResume` 不直接 enqueue）。

### 7.4 第二次 LLM（`handleToolResult`）

与场景一 §3.5：`H₂ → H₃ = H₂ + assistant(终稿)`，Q 回到 `[]`。

### 7.5 队列 / History 汇总

```text
Q:  [] → [resume] → [] → [tool_result] → [] → []

H:  H₁(open)
      │ ContinueAfterResume
      ▼
    H₁ + tool(tc_appr)        ← batch 闭合
      │ handleToolResult
      ▼
    … + assistant(终稿)
```

### 7.6 `ask_user_information` 变体

- `pending` 为 `PendingHITL` 中含 `ask_user_information` 的 item；可与 `execute_tool` **同批**。SSE **`hitl_required`**；分步 resume。
- `ContinueAfterResume` 写 **单个** ask_user 的 `tool`，同样返回 `ScheduleToolResult: true`。
- 后续队列 / commit 轨迹与审批路径一致。

### 7.7 不变量

- batch 闭合发生在 **resume handler 的 runTurnStep 内**，commit 后才 `scheduleToolResult`。
- 成立：闭合 → enqueue → dequeue → 下一步 LLM（与场景一对齐，仅 enqueue 触发点不同）。

---

## 8. 场景六：Bash background + async 回灌（设计意图路径）

### 8.1 前提

- 单 auto `bash_run`（`run_in_background` 或同步超时降级 `StartBackground`）。
- 第一步 tool 消息为 **ACK**（含 `job_id`），非最终结果。
- Job 完成后 `SetBackgroundJobNotifier` → `EnqueueAsyncToolResult`。
- 不含场景四的 approval pending（batch 在第一步已闭合）。

### 8.2 三阶段 LLM（当前实现：Produce / Apply / Continue）

```text
阶段 A  handleHumanMessage        → U + A(tc) + T(ACK)     → enqueue tool_result
阶段 B  handleToolResult           → A₂（如「任务运行中」）   → 可能已 done，job 仍跑
阶段 C  Produce → Apply → Continue → 写 callback + 被动 LLM → A₃（终稿）
```

### 8.3 阶段 A–B（同步段，同场景一）

| 阶段 | H 累积 | Q |
|------|--------|---|
| A commit | `H₁ = H₀+U+A(tc)+T(ACK)` | `[tool_result]` |
| B commit | `H₂ = H₁+A₂` | `[]` |

若 `A₂` 为纯文本，此时 Client 已见 `turn_finished(stop)`，但 job 未完成。

### 8.4 阶段 C — Produce / Apply / `side_effect_continue`

Job 完成 → Q: `[async]` → `handleSideEffectProduceAsync`（**Produce**：SSE + 缓冲，不改 H）。

**Apply**（`runTurnStep` 步首、`Cancel` 恢复、或 `TaskComplete` 后 `ReconcileAfterStep`）按 `PlanSingleSideEffectApply` 写入 history，并推送 `side_effect_applied`。

**Continue**：`scheduleSideEffectContinue` → `side_effect_turn_start` → `handleSideEffectContinue` → 被动 `RunToolMessageTurn`。

**尾部分类**（`classifyToolResultTail` / `selectSideEffectSegments`）：

| 进入 Apply 时末条 | 写入段 | 是否 Continue |
|-------------------|--------|---------------|
| `tool`（末条为 ACK） | append `A_cb + T(result)` | **是**（`tailTool`） |
| `assistant` 无 tc（末条为 A₂ 文本） | append `U_async + A_cb + T(result)` | **是**（桥接态） |
| `assistant` 带未闭合 tc | insert 在末条 assistant 前 | **否**（等 batch 闭合） |

| 子阶段 | 事件 | H | Q |
|--------|------|---|---|
| C1 | Dequeue async → Produce | `H₂` 不变 | `[]` |
| C2 | Apply（步首或 continue 前） | **`H₃'`**（+callback） | `[]` |
| C3 | `side_effect_continue` → passive LLM | **`H₃`**（+终稿） | `[]` |
| C4 | commit；无 inline async 续跑 | `H₃` | `[]` |

### 8.5 队列 / History 汇总

```text
Q:  [message]→[]→[tool_result]→[] → (job 完成) → [async]→[]

H:  H₀ → H₁(ACK) → H₂(+A₂) → H₃(+callback+终稿)
```

### 8.6 相对场景一

| 对比项 | 场景一 | 场景六 |
|--------|--------|--------|
| tool 第一步内容 | 最终结果 | ACK |
| 续跑次数 | 1× `tool_result` | 1× `tool_result` + **1× `side_effect_continue`** |
| 旁路入队 | 无 | `async_tool_result` → Produce；Continue 经内部队列 |
| SSE | 一次 turn_finished 链 | 可能 **两次** turn_finished（B 与 C 各一次）+ `side_effect_applied` |

### 8.7 不变量（当前实现）

- 步内单写者仍成立。
- Produce **不**改 history；Apply 在步首/Continue 前统一写入。
- 被动续跑经 **`side_effect_continue`**，非 consume 内 inline `RunToolMessageTurn`。

---

## 9. 场景七：Trigger message 投递

### 9.1 前提

- Scheduler / 工具 `trigger_fire` → `EnqueueTriggerMessage`（`request_type=trigger_message`）。
- 写入 `InputBox FIFO`，`UserName=trigger`，`TriggerID` 非空；trigger 与普通用户输入共用数据面顺序，不占用控制队列优先级。
- 分 **7a 空闲投递** 与 **7b pending 时投递**。

### 9.2 子场景 7a：无 pending（同场景二 + trigger 元数据）

| 时刻 | 事件 | Q | H |
|------|------|---|---|
| T0 | `SubmitTriggerMessage` | `[trigger_message@other]` | `H₀` |
| T1 | Dequeue → Produce（**Apply 前**不清 delivery） | `[]` | `H₀` |
| T2 | TaskComplete / human 步首 Apply | | |
| T3 | 桥接态：deferred user + callback；否则 callback SSE | **`H₀+…`** | `[]` |

- Trigger store：`HasPendingDelivery` 在 fire 前置位；**Apply 成功**后 `ClearPendingDelivery`。

### 9.3 子场景 7b：存在 approval pending 时 fire

| 时刻 | 事件 | H | Q | runtime |
|------|------|---|---|---------|
| T0 | 处于场景四 pending | `H₁` open | `[]` | pending≠nil |
| T1 | Trigger 入队 | `H₁` | `[trigger_message@other]` | 不变 |
| T2 | Dequeue → Produce（缓冲，不改 H） | `H₁` | `[]` | pending 仍在 |
| T3 | human 步首 Apply + `InterruptPending` 后新 turn | 见场景八 | | |

```text
H₁' = H₁ + tool(tc_appr → ToolUserInterruptedMessage)   ← batch 强制闭合
```

之后轨迹同场景二或场景一（视 LLM 是否出 tool）。

### 9.4 与 async 优先级

若 Q 中同时有 `[async(2), trigger(10)]`（先入 async），则 **先** Produce async，**后** Produce trigger。Trigger 不会跳过 async。

### 9.5 队列 / History 汇总（7b）

```text
Q:  [] → [trigger_message@other] → []

H:  H₁(open,pending)
      │ InterruptPending + 新 human turn
      ▼
    H₁ + tool(interrupt) + user(trigger) + …
```

### 9.6 不变量

- Trigger **不丢**：Apply 成功后 `ClearPendingDelivery`；若 `HasPendingDelivery` 则 scheduler **跳过** fire。
- 7b **主动抢占** pending：与场景八同类，属产品语义而非 bug。

---

## 10. 场景八：新 Human message 打断 pending

### 10.1 前提

- 起始于场景四 §6.4：`H₁` open batch，`pending=approval(tc_appr)`。
- 用户 POST 新 `message`（`PriorityHuman=0`），**非** resume。

### 10.2 总览时序

```mermaid
sequenceDiagram
    participant API as POST message
    participant Loop as consumeLoop
    participant Orch as Orchestrator

    API->>Loop: Enqueue(message, human)
    Loop->>Orch: handleHumanMessage 步前
    Orch->>Orch: InterruptPending → tool(interrupt)
    Orch->>Orch: RunHumanMessageTurn(新 U)
```

### 10.3 轨迹

| 子阶段 | 事件 | H（commit 前） | Q | runtime |
|--------|------|----------------|---|---------|
| S1 | 入队 human | `H₁` | `[message]` | pending 仍在 |
| S2 | Dequeue | `H₁` | `[]` | — |
| S3 | 步前：取 `pending` → **`InterruptPending`** | **`r.messages` 已写 interrupt tool** | `[]` | pending=**nil**（步前清空） |
| S4 | — | 当前无通用 orphan repair；取消/恢复只依据生命周期事实显式闭合 | `[]` | — |
| S5 | `toolLoopCount=0`；`runTurnStep` 新 turn | 步内 `+U_new+…` | `[]` | — |
| S6 | commit | **`H₂`** | 视 LLM | idle 或新 pending |

```text
H₂ = H₁ + tool(tc_appr → interrupted) + user(U_new) + …
```

### 10.4 与场景七 7b 对比

| 项 | 场景八（human） | 场景七 7b（trigger） |
|----|-----------------|----------------------|
| 队列优先级 | 0 | 10 |
| 步前逻辑 | 相同 `InterruptPending` | 相同 |
| user `Name` | human（默认） | trigger |
| TriggerID / delivery | 无 | Apply 成功后清 pending delivery |

### 10.5 队列 / History 汇总

```text
Q:  [] → [message] → [] → (若新 turn 有 tool) → [tool_result] → …

H:  H₁(open)
      │ InterruptPending
      ▼
    H₁ + tool(interrupt)     ← 旧 approval 作废
      │ 新 RunHumanMessageTurn
      ▼
    … + user(new) + …
```

### 10.6 不变量

- 旧 pending **不会**通过 resume 继续；interrupt tool 闭合旧 batch。
- 新 turn 的 enqueue / 续跑规则回到场景一或场景四。
- **不**恢复旧 `pending`（Produce 缓冲不改 pending；无旧 `savedPending` 回滚路径）。

---

## 11. 场景对照总表

| # | Handler 次数（典型） | 第一步后 Q | pending | 续跑机制 | 主要风险 |
|---|---------------------|------------|---------|----------|----------|
| 1 | 2 | `tool_result` | nil | enqueue ×1 | — |
| 2 | 1 | `[]` | nil | 无 | — |
| 3 | 2 | `tool_result` | nil | enqueue ×1 | — |
| 4 | 1 | `[]` | approval | 等 resume | open batch + 旁路插队 |
| 5 | 2+ | `tool_result` | nil（resume 后） | ScheduleToolResult | — |
| 6 | 2+async | `tool_result` + async | nil | `side_effect_continue` | open batch 下须 Produce 不 Apply |
| 7 | 1 | `[]` | 7b 打断 | Apply + continue / 同 2 | 抢占 pending |
| 8 | 1+ | 视新 turn | 打断后 nil | 同 1/4 | 抢占 pending |

---

## 12. 重构目标 ↔ 现实现

| # | 原目标 | 现实现 |
|---|--------|--------|
| 1 | OpenBatch 门闩：pending/open batch 时 async 只 defer | ✅ `sideEffectStore.Produce`；`runtime_async_open_batch_test.go` |
| 2 | Flush 于 batch 闭合 / Interrupt / TaskComplete | ✅ `ApplyReady` 步首；`ReconcileAfterStep` 步末 |
| 3 | 统一续跑，禁止 consume 内 inline async | ✅ `side_effect_continue`（priority -1） |
| 4 | Turn epoch 丢弃过期 deferred | ⏳ 未做（ClearContext 发 `side_effects_cleared`；orphan Produce 行由 Client 标失效） |
| 5 | 去掉 `savedPending` 回滚 | ✅ 已移除 `HandleAsyncToolResult` inline 路径 |

---

## 13. 变更记录

| 日期 | 说明 |
|------|------|
| 2026-06-21 | 初稿：场景索引 + 场景一完整规格 |
| 2026-06-21 | 补充场景二～八完整规格 + 对照总表 |
| 2026-06-20 | 实现闭环：Produce/Apply/Continue、`side_effect_applied`/`cleared`、移除 inline `HandleAsyncToolResult` |
