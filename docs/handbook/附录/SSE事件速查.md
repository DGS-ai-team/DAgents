# SSE 事件速查

实现：`node/internal/stream/`、`node/internal/turn/orchestrator.go`。Client 订阅：`GET /v1/stream`（或 `/v1/streams?session_id=`，以实现为准）。

---

## 连接

```http
GET /v1/stream?after_seq=0
Accept: text/event-stream
```

- `after_seq`：只接收该序号**之后**的事件（Hub `CurrentSeq()`）。  
- 事件 JSON 通常含 `session_id`、`seq`（或等价字段）。

---

## 事件一览

| 事件 | 含义 | 典型后续 |
|------|------|----------|
| `assistant_delta` | 流式 assistant 正文 | 继续流式 |
| `reasoning_delta` | 推理链片段 | DeepSeek 等 |
| `tool_call` | 模型发起工具调用 | 执行或 HITL |
| `tool_result` | 工具返回 | 入队 `tool_result` 续跑 |
| `approval_required` | 需审批 | Client resume |
| `user_information_required` | 需用户输入 | Client resume |
| `usage` | token 统计 | 展示独占一行 |
| `temporary_agent_created` | 子 Agent 创建 | 父 session UI |
| `temporary_agent_completed` | 子 Agent 完成 | |
| `temporary_agent_cancelled` | 子 Agent 取消 | |
| `error` | 错误 | turn 可能结束 |
| `done` | **轮到用户**（见下） | Client 解锁输入 |

A2A caller relay 可能含 synthetic 工具块事件；见 [05-Manage与A2A](../05-Manage与A2A.md) §5。

---

## `done` 语义（语义 B）

`done` **仅**表示编排器暂停、**轮到用户**——不是 assistant 段落结束。

| 字段 | 说明 |
|------|------|
| `finish_reason` | `stop` \| `awaiting_user_information` \| `awaiting_tool_approval` \| `error` \| `cancelled` |
| `turn_complete` | `true`：本条 user message 链已结束；`false`：HITL 暂停 |
| `awaiting` | HITL 时：`user_information` \| `tool_approval`；否则 `null` |

| 场景 | 发 `done`？ | `turn_complete` |
|------|-------------|-----------------|
| 无 tool_calls 正常结束 | ✅ | `true` |
| 审批 / ask_user  pending | ✅ | `false` |
| 自动工具后 `tool_result` 续跑 | ❌ | — |
| resume 后继续链 | ❌ | — |

实现：`publishDone`（`sse_publish.go`）。

---

## HITL 事件分工

- `approval_required` / `user_information_required`：弹 HITL UI（含 `tool_call_id` 等）。  
- `tool_call`：转录工具行；**不**替代 HITL 块。  
- 子 Agent 内部 `done`：**不**转发到父 SSE（`relay_hub.go`）。

---

## 源码索引

| 文件 | 职责 |
|------|------|
| `turn/orchestrator.go` | 发布 assistant / done / usage |
| `turn/tool_router.go` | tool_call / tool_result |
| `turn/approval_payload.go` | approval_required 载荷 |
| `stream/hub.go` | 序号与订阅 |
| `childagent/relay_hub.go` | 子 Agent 过滤 |

Python Client：`app/cli/session_controller.py` — `wait_user_turn` 等。  
Go Client：`client/internal/tui/full/stream_events.go`。
