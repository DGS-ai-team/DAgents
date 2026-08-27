# SSE 事件速查

实现：`node/internal/stream/`、`node/internal/turn/sse_publish.go`。Web UI 订阅：`GET /v1/streams?agent_id=`。

---

## 连接与恢复

```http
GET /v1/streams?agent_id=agt-xxx&live=1
Accept: text/event-stream
```

首连使用 `live=1`，订阅点由 Node 在注册订阅者时原子确定，不会漏掉紧邻到达的事件。Agent 过滤流重连使用：

```http
GET /v1/streams?agent_id=agt-xxx&after_agent_seq=42
```

`after_agent_seq` 只计算当前 Agent 的可重放事件；其他 Agent 和 `delivery=ephemeral` 的高频事件不会制造假洞。无 Agent 过滤的 Node 级订阅才使用 `after_seq`。

每条事件 envelope 都包含：

| 字段 | 含义 |
|------|------|
| `event_version` | 事件协议版本，当前为 `1` |
| `stream_epoch` | Node 进程事件纪元；进程重启后变化 |
| `seq` | Hub 进程级传输序号，主要用于诊断和 SSE `id` |
| `agent_seq` | 当前 Agent 的可重放连续序号；ephemeral 事件不设置 |
| `delivery` | `replayable` 或 `ephemeral` |
| `agent_id` / `type` / `ts` / `data` | 事件身份、名称、时间和业务数据 |

当请求游标早于 Hub 保留历史时，流先发送 `resync_required`，其 `data.requires_hydrate=true`。客户端必须调用 Agent hydrate，以权威快照修复 transcript、HITL、工具和 turn 状态，然后从新的 `stream_epoch` / `agent_seq_hint` 继续接收事件。

---

## 事件分工

| 事件 | 含义 | 客户端职责 |
|------|------|------------|
| `assistant` | assistant 正文 delta | 追加流式正文 |
| `reasoning` | 推理 delta | 更新思考输出 |
| `tool_call` | 模型发起工具调用 | 展示工具行 |
| `tool_result` | 工具返回结果 | 更新工具行/继续 turn |
| `turn_state` | Turn Coordinator 权威生命周期快照 | 更新 phase、终态、取消和工具执行态 |
| `hitl_required` | 当前 turn 需要用户交互的事实 | 展开审批/询问队列，等待 resume |
| `turn_finished` | 当前 turn 真正进入终态 | 收束流式输出；不负责判断 HITL |
| `error` | 错误事实 | 展示错误；终态由 `turn_state` 收敛 |
| `usage` | token/cache 统计 | 更新状态栏和诊断 |
| `resync_required` | 恢复游标不可覆盖当前历史 | 立即 hydrate |
| `side_effect_turn_start` | 旁路结果触发被动 LLM turn | 开启 implicit turn |
| `side_effect_applied` / `side_effects_cleared` | 旁路结果入库/失效 | 更新 callback 工具行 |
| `temporary_agent_created` / `temporary_agent_completed` / `temporary_agent_cancelled` | 子 Agent 生命周期 | 更新子 Agent 面板 |

核心原则：`hitl_required` 和 `turn_state.phase=tool_waiting|waiting_user` 表示暂停；暂停不发送 `turn_finished`。`turn_finished` 的 `turn_complete` 固定为 `true`，不再携带 `awaiting` 这种会改变语义的字段。

---

## HITL

`hitl_required` 一次携带整批 `PendingHITL.Items[]`：

| `items[].hitl_type` | 含义 | resume |
|---------------------|------|--------|
| `user_information` | `ask_user_information` | `type=user_information` + `tool_call_id` |
| `execute_tool` | 需要审批的工具 | `type=approval` / `selection` |

同批可以混合询问和审批，并支持分步 resume。Node 保存的 pending 状态是唯一事实源；如果实时事件丢失，`turn_state` 进入等待态后客户端 hydrate 对账，不把普通工具行当成审批卡片。

子 Agent 的内部 `turn_finished` 不中继为父 Agent 的终态；父 Agent 仍由自己的 `turn_state` / `turn_finished` 收束。子 Agent 的 HITL 仍可通过带 `child_agent_id` 的 `hitl_required` 展示。

---

## 旁路 side-effect

Produce 阶段的 `tool_call` / `tool_result` 只展示回调预览并携带 `side_effect_seq`；Apply 写入 history 后发送 `side_effect_applied`，ClearContext/Delete 丢弃时发送 `side_effects_cleared`。旁路被动续跑前发送 `side_effect_turn_start`，后续是普通 `turn_state`、assistant/tool 事件和最终 `turn_finished`。

取消仍由显式 cancel API 驱动：取消请求不会被普通 human message 抢占；Node 以取消生命周期事件和权威 `turn_state` 收敛状态。取消过程中产生的 side-effect 缓冲按既有 cancel recovery 流程处理。

---

## 源码索引

| 文件 | 职责 |
|------|------|
| `node/internal/stream/hub.go` | 事件版本、epoch、全局/Agent 游标、历史与 resync 判定 |
| `node/internal/api/server_messages.go` | SSE 连接、live/after_agent_seq、resync_required |
| `node/internal/api/server_agent_api.go` | hydrate 的 stream_epoch/stream_seq_hint/agent_seq_hint |
| `node/internal/turn/sse_publish.go` | assistant / HITL / turn_finished / usage / side-effect 事件 |
| `node/internal/session/turn_state_view.go` | Turn Coordinator 权威状态快照 |
| `node/internal/childagent/relay_hub.go` | 子 Agent 终态隔离 |
| `node/webui/frontend/src/sse/stream.js` | SSE 事件解析和 Agent 游标重连 |
| `node/webui/frontend/src/views/ChatView.vue` | 事件路由、HITL 和 turn 投影 |
