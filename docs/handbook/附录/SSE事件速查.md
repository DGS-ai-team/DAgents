# SSE 事件速查

实现：`node/internal/stream/`、`node/internal/turn/orchestrator.go`。Client 订阅：`GET /v1/streams?agent_id=`（以实现为准）。

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
| `user_message_deferred` | 旁路 external Produce（桥接态） | 展示 deferred user；不结束 turn |
| `side_effect_turn_start` | 被动 side-effect 续跑 LLM 前 | Client `beginImplicitTurn` |
| `hitl_required` | 本地 turn 统一 HITL（含 ask + 审批，见下） | Client 分步 resume |
| `approval_required` | 需审批（A2A 中继 / 子 Agent） | Client resume |
| `user_information_required` | 需用户输入（A2A 中继） | Client resume |
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
| `finish_reason` | `stop` \| `awaiting_hitl` \| `error` \| `cancelled` |
| `turn_complete` | `true`：本条 user message 链已结束；`false`：HITL 暂停 |
| `awaiting` | HITL 时：`hitl`；否则 `null` |

| 场景 | 发 `done`？ | `turn_complete` |
|------|-------------|-----------------|
| 无 tool_calls 正常结束 | ✅ | `true` |
| 审批 / ask_user  pending | ✅ | `false` |
| 自动工具后 `tool_result` 续跑 | ❌ | — |
| resume 后继续链 | ❌ | — |

实现：`publishDone`（`sse_publish.go`）。

---

## HITL 事件分工

### `hitl_required`（本地 turn）

单事件携带整批 pending；Client 按 `items[].hitl_type` 展示并分别 resume。

| 字段 | 说明 |
|------|------|
| `hitl_id` | 批次 id（展开为 approval 时写入 `approval_id`） |
| `message` | 摘要文案 |
| `items[]` | 待交互项列表 |

| `items[].hitl_type` | 含义 | Client resume |
|---------------------|------|---------------|
| `user_information` | `ask_user_information` | `type=user_information` + `tool_call_id` |
| `execute_tool` | 需审批工具 | `type=approval` / `selection` |

同批可含 **ask_user + 多个 execute_tool**；Node `PendingHITL.Items` 与 SSE 一一对应，支持**分步 resume**。

### 兼容事件

- `approval_required` / `user_information_required`：A2A caller 中继、子 Agent 等仍使用。  
- `tool_call`：转录工具行；**不**替代 HITL 块。  
- 子 Agent 内部 `done`：**不**转发到父 SSE（`relay_hub.go`）。

**Client 展开**：Go `client/internal/hitl/hitl_batch.go`；Python `app/cli/hitl_batch.py`；Web `node/webui/frontend/src/stores/hitl.js` → `expandHitlRequired`。

---

## 旁路 side-effect（Produce / 被动续跑）

| 事件 | 含义 | Client |
|------|------|--------|
| `user_message_deferred` | trigger/a2a 桥接 Produce | deferred 样式 user 行 |
| `side_effect_turn_start` | `side_effect_continue` 跑 LLM 前 | `BeginImplicitTurn` / `beginImplicitTurn` |
| `side_effect_applied` | Apply 写入 history | 标 deferred/callback 为 **已入库** |
| `side_effects_cleared` | ClearContext/Delete 丢弃缓冲 | 标未入库条目为 **已失效** |
| Produce 的 `tool_call`/`tool_result` | async 回灌预览 | 正常渲染；含 `side_effect_seq`；idle 时不 `finishTurn` |

**Cancel 序列**：`done(cancelled)` → `finishTurn` → `side_effect_turn_start` → 被动 turn → `assistant`… → `done(stop)`。

详述：[agent-node-api.md §2.4.3](../../architecture/agent-node-api.md)、[turn-side-effects-refactor.md](../../design/turn-side-effects-refactor.md)。

---

## 源码索引

| 文件 | 职责 |
|------|------|
| `turn/sse_publish.go` | assistant / done / usage / side-effect deferred |
| `turn/tool_router.go` | tool_call / tool_result |
| `turn/hitl_payload.go` | `hitl_required` 载荷 |
| `turn/approval_payload.go` | execute_tool item 展示字段 |
| `stream/hub.go` | 序号与订阅 |
| `childagent/relay_hub.go` | 子 Agent 过滤 |

Python Client：`app/cli/session_controller.py` — `wait_user_turn` 等。  
Go Client：`client/internal/tui/full/stream_events.go`。
