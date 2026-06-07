> **待修订（2026-05）**：Backend Control Plane / `connection_id` 描述已过时。Agent+Client 阶段以 **本地 Go Node SSE** 为准，见 [agent-node-api.md](../architecture/agent-node-api.md)（**§2.4.1 `done` 语义**）、[agent-client-refactor-plan.md](../design/agent-client-refactor-plan.md)。

# Client 事件与人机交互（HITL）

本文定义 **Client Plane** 与 Backend 之间的 SSE 事件协议，以及 **审批**、**用户询问** 两类人机交互在 v2 中的归属与 resume 约定。v1 行为以 [agent-input-output.md](../agent-input-output.md) 为准；v2 在保留语义基础上增加 `connection_id`、`execution_id`、`body_id` 等字段。

## 1. 设计原则

- **Brain** 决定是否调用工具；**Control Plane** 决定策略与 execution 状态；**Client** 负责展示与收集人工输入。
- HITL 暂停时 Brain turn **未完成**：须通过 `resume` 回灌后继续 Session Queue 消费。
- SSE 推送范围由 **connection 授权的 session 集合** 决定，不能跨 connection 泄露。

## 2. SSE 帧格式（v2）

与 v1 兼容扩展：

```text
event: approval_required
data: {"connection_id":"conn-...","session_id":"sess-...","agent_id":"...","body_id":"...","type":"approval_required","seq":12,"ts":"...","data":{...}}
```

顶层字段：

| 字段 | 说明 |
|------|------|
| `connection_id` | **必填**；SSE 分桶键 |
| `session_id` | 有 |
| `agent_id` | 推荐 |
| `body_id` | 推荐（Body 相关 execution 时） |
| `type` | 有 |
| `seq` | 按 connection 维度递增 |
| `data` | 业务载荷 |

Gateway 须关闭 SSE buffering（`X-Accel-Buffering: no`）。

## 3. 事件目录

### 3.1 模型与推理流（继承 v1）

| type | 说明 | 典型 `data` 字段 |
|------|------|------------------|
| `assistant` | 助手正文增量/完整块 | `content`, `display_type` |
| `reasoning` | 推理链（若模型支持） | `content` |
| `usage` | Token 用量 | `prompt_tokens`, `completion_tokens`, ... |
| `tool_call_delta` | 流式 tool call 片段 | `tool_calls` |
| `tool_call` | 完整 tool call | `tool_calls`, `assistant_content` |
| `tool_result` | 工具结果 | `content`, `tool_call_id`, `tool_name`, `rejected` |
| `error` | 错误 | `message` |
| `done` | 编排暂停、轮到用户（语义 B，**非**段落换行） | `finish_reason`、`turn_complete`、`awaiting`（见 [agent-node-api.md §2.4.1](../architecture/agent-node-api.md)） |

### 3.2 人机交互（HITL）

| type | 说明 | 触发方 |
|------|------|--------|
| `approval_required` | 工具执行待审批 | Control Plane / 编排器 |
| `user_information_required` | 模型向用户询问信息 | Brain 编排器（`ask_user_information`） |

### 3.3 v2 新增（Body / Execution）

| type | Phase | 说明 |
|------|-------|------|
| `execution_started` | 1 | Body 或 backend 工具开始执行 |
| `execution_progress` | 2 | 长任务进度（可选） |
| `body_offline` | 1 | 绑定 Body 失联，后续 body 工具将失败 |
| `policy_denied` | 1 | 策略拒绝（也可仅通过 `tool_result` + `rejected` 表达） |

Phase 1 可将 execution 生命周期折叠进 `tool_call` / `tool_result`；独立事件便于 UI 展示进度条与取消按钮。

## 4. 工具审批（`approval_required`）

### 4.1 归属

```text
Brain 产生 ToolCall
  → Control Plane: decide_tool_approval / policy
  → require_approval → ExecutionRecord.status = waiting_approval
  → SSE: approval_required
  → 等待 Client resume
  → approved → 下发 execute（body 或 backend）
  → denied → tool_result(rejected=true)
```

### 4.2 SSE `data` 载荷（v2 扩展）

```json
{
  "approval_type": "execute_tool",
  "approval_id": "appr-...",
  "execution_id": "exec-...",
  "content": "待审批：执行 shell 命令",
  "description": "...",
  "approval_args": {
    "tool_name": "shell_exec",
    "tool_kind": "body",
    "arguments": { "command": "kubectl apply -f ..." },
    "risk_level": "high",
    "policy_source": "rule:ops-write"
  },
  "display_type": "normal_text"
}
```

v1 字段 `approval_args`、`approval_id` 保留；**新增 `execution_id`、`tool_kind`** 便于 Client 与审计关联。

### 4.3 Resume

```http
POST /v1/messages

{
  "connection_id": "conn-...",
  "session_id": "sess-...",
  "request_type": "resume",
  "resume_value": {
    "type": "approval",
    "execution_id": "exec-...",
    "decision": "approved"
  }
}
```

`decision`：`approved` | `denied`。

v1 兼容：无 `execution_id` 时按 `approval_id` 或 pending tool call 匹配（Phase 1 过渡期）。

### 4.4 审批主体

见 [security-and-policy.md](./security-and-policy.md) §5。Client 仅提交 decision；Backend 校验 connection 对应用户是否有权批准该 Agent + Body 的执行。

A2A 触发的 body 写操作默认 `require_approval`，审批人为 **目标 Agent owner 或目标 session 用户**，不能由 caller Agent 单方面批准。

## 5. 用户询问（`user_information_required`）

### 5.1 归属

`ask_user_information` 是 **`tool.kind=backend` 的编排器特殊工具**：

- **不**经 Execution Dispatcher 下发 Proxy。
- 不依赖 Body online。
- 暂停 Brain turn，等待用户输入后作为 **tool result** 继续推理。

### 5.2 SSE `data` 载荷

```json
{
  "content": "请选择部署环境",
  "description": "...",
  "user_information_args": {
    "mode": "choice",
    "question": "目标环境？",
    "choices": ["dev", "staging", "prod"],
    "allow_free_text": false
  },
  "tool_call_id": "call_...",
  "display_type": "normal_text"
}
```

`mode`：`free_text` | `choice` | `mixed`（与 v1 schema 一致）。

### 5.3 Resume

```json
{
  "request_type": "resume",
  "resume_value": {
    "type": "user_information",
    "tool_call_id": "call_...",
    "answer": "staging"
  }
}
```

编排器 `_handle_user_information_resume` 将 answer 写入 messages 作为 tool result，并继续 `_run_turn_and_maybe_execute_tools`。

### 5.4 与审批的区别

| 维度 | approval | user_information |
|------|----------|------------------|
| 触发 | 策略层 / 风险工具 | 模型主动询问 |
| 绑定 ID | `execution_id` | `tool_call_id` |
| 是否创建 ExecutionRecord | 是 | 否（Phase 1） |
| 是否依赖 Body | body 工具审批依赖 | 否 |

## 6. Body 离线通知

当 Body presence 从 online → offline：

- 向该 Agent 相关 active connection 推送 `body_offline`（或 session 级 error）。
- 新的 `tool.kind=body` 执行在 policy 前即失败，返回可读错误。
- 已在 `waiting_approval` 的 body execution：标记 `failed` 或保持等待直至 TTL（产品可选；默认 failed + 通知用户）。

`ask_user_information` 与 backend 工具 **不受影响**。

## 7. 异步工具与 HITL 顺序

同一 session 内优先级（与 v1 队列一致）：

```text
tool_result / async_tool_result  >  human message  >  resume  >  other
```

含义：

- 异步 body 任务完成时，`async_tool_result` 优先于新的 human message 被消费。
- 用户可在 `approval_required` 等待期间发送 human message（可能打断当前 turn，见 v1 `interrupted_by_user_message` 语义）。

## 8. A2A Client 的 HITL

A2A callee session 也可能产生 `approval_required`：

- 审批事件只推送到 **callee Backend 为该 A2A session 创建的 connection**。
- Caller 若需代批，须通过 `agent_peer_approve_tools`（backend 工具）向 callee 提交 resume，不能绕过 callee policy。

## 9. Go TUI / Web UI 实现清单

Client 须实现：

1. `POST /v1/connections` → 持有 `connection_id`
2. `GET /v1/streams?connection_id=...` 长连接
3. 处理 `approval_required` → UI 确认 → `resume(type=approval)`
4. 处理 `user_information_required` → 表单/选项 → `resume(type=user_information)`
5. （可选）`execution_started` / cancel API

## 10. Phase 1 最小集

- 保留 v1 全部 SSE 事件类型。
- 增加可选字段：`connection_id`、`execution_id`、`body_id`、`agent_id`。
- 审批 resume 同时支持 `execution_id` 与 v1 `approval_id`。
- `user_information_required` 行为与 v1 完全一致。
