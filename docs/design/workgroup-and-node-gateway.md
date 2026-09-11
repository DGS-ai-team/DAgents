# Workgroup 与 Node Gateway

> **状态**：现行 Workgroup 设计。实现状态以代码、CHANGELOG 和 [`docs/user/workgroups.md`](../user/workgroups.md) 为准；字段级约束见 [`workgroup-d05-contracts.md`](./workgroup-d05-contracts.md)。当前 AgentRef 协议不再接收旧的 synthetic member/provision/`turn_id` 委派字段。

## 1. 目标与边界

Workgroup 让 Manage 组织多个 Node 上已经存在的 Agent。Manage 负责成员引用、任务编排、公开时间线和人机审批；Node 负责运行被引用 Agent 的真实 Session、LLM、工具和本地 policy。

网络硬约束：

- Node 是唯一连接发起方；
- Node 主动建立到 Manage 的 HTTPS/WSS 连接并重连；
- Manage 不主动请求 Node HTTP，不依赖 callback URL；
- Node 离线时 Manage 只能显示等待/不可用，不伪造执行完成。

## 2. 核心对象

```text
AgentRef = { node_id, agent_id }
Member   = { workgroup_id, member_id, AgentRef, display_name, role }
Session  = { workgroup_id, member_id, session_id }
Assign   = { assign_id, member_id, leader_run_id, parent_turn_id, child_turn_id, attempt_id, status }
```

一个 Agent 可以同时拥有个人 Session 和多个 Workgroup Session。`member_id` 只是在一个工作组内的引用身份，不能替代 `agent_id`。
一次 `Assign` 只有一个执行尝试；`assign_id`、`parent_turn_id`、`child_turn_id` 和 `attempt_id` 各自承担委派、父 Turn、成员 Turn 和尝试身份，不能互相替代。Leader 工具委派的 `leader_tool_call_id` 可以存在，`@成员` 直达委派则为空。

## 3. 成员加入流程

```text
1. Node 注册 node_id 与本地 Agent catalog
2. 用户在 Manage/Node UI 选择已有 AgentRef
3. Manage 持久化 Member，状态 binding
4. Manage 通过现有 WS 发送 agent.session.open
5. Node 校验 agent_id，创建/恢复隔离 Session，返回 session.ready
6. Manage 将成员置为 ready，允许 assign/@
```

Node 离线时状态为 `waiting_for_node`；Agent 不存在、已归档或能力不满足时进入明确的 `error`，不回退为创建受限 Agent。

## 4. WS 消息与可靠性

Node → Manage：

```text
session.hello / resume.offer
agent.session.ready / error / closed
agent.turn.accepted / event / result / cancelled / resumed
agent.tool.cancelled
delivery.ack
```

Manage → Node：

```text
welcome
agent.session.open / close
agent.turn.start / cancel / resume
agent.tool.cancel
timeline.event
```

控制消息都携带适用的 `node_id`、`agent_id`、`session_id`、`workgroup_id`、`member_id`、`assign_id`、`parent_turn_id`、`child_turn_id` 和 `attempt_id`。成员执行事件另外携带 `event_id`、`event_seq` 和 `stream_epoch`；`event_seq` 只在同一 `stream_epoch` 内递增。

可靠性规则：

1. Manage outbox 在发送前持久化；Node 回 ACK 后才推进 delivery cursor。
2. Node 以 `command_id/payload_hash` 去重，重复 start/cancel 返回既有状态。
3. 新连接提升 `connection_generation`，旧连接帧不可覆盖新状态。
4. 断线后按 cursor resume；出现 gap 时重新请求 snapshot/reconcile。
5. accepted 只表示命令 journal 已落盘，不表示副作用已成功。

成员事件的恢复边界由 `assign_id + child_turn_id + attempt_id` 固定，Manage 按 `stream_epoch/event_seq` 单调推进 Assign 游标；重复事件只确认、不重复写 Timeline。Node 重启后若成员 Session 仍在执行，Node 重新订阅并回放同一个 child Turn，不重新入队用户消息。

## 5. Turn、工具和审批

Workgroup 的 supervisor 只使用 Manage 编排工具；成员 turn 通过 WS 在成员 Agent 的 Node Session 中运行。所有执行权限按以下交集计算：

```text
Agent snapshot ∩ Workgroup overlay ∩ Node local policy
```

工具状态必须绑定 `assign_id + tool_call_id + session_id`。工具审批（`kind=tool_approval`）由拥有执行权限的 Node 产生，Manage 只做持久化投影和用户路由；用户问题（`kind=user_question`）是成员 Turn 的输入恢复，不是工具授权。两者都以 HITL id/CAS/幂等方式只解析一次，但恢复负载必须带回同一个 `assign_id + child_turn_id + attempt_id`。

已 accepted 的非幂等工具在连接中断后不能自动重做；结果未知时标记 `indeterminate`，通过查询或人工确认收敛。

## 6. 消息与事件隔离

### 私有 RunHistory

每个成员 Session 保留模型所需的完整合法消息序列，包括 assistant tool call 与对应 tool result。它不广播给其他成员。

### 公共 Timeline

Timeline 只保存工作组需要看到的最终产出、Assign 状态、审批摘要和必要的脱敏事件。不能把完整 raw tool args/results 当作所有成员的模型上下文。

### UI 订阅

Node UI 通过 `/v1/workgroups/{workgroup_id}/events` 接收工作组事件，并与 hydrate/Timeline 对账；个人 Agent 页面只显示个人 Session。前端状态必须由 `session.*`、`turn.*`、`timeline.*` 等权威事件驱动。

## 7. 状态机

```text
Member:  binding → waiting_for_node → ready → archived | error
Session: opening → ready → running → awaiting_hitl → closed | error
Assign:  queued → running → awaiting_hitl → succeeded | failed | canceled | indeterminate
Attempt: created → running → awaiting_hitl → terminal（同一 Assign 内最多一个当前 Attempt）
Turn:    queued → accepted → running → completed | failed | canceled | indeterminate
```

状态单调性和 fencing 优先于“看起来完成”。迟到的 ready/result/close 不能复活已经归档或 generation 已变更的对象。

## 8. 验证矩阵

| 场景 | 必须验证 |
|---|---|
| 选择已有 Agent | 不创建 shadow Agent；AgentRef、Node、成员名称显示正确 |
| Node 重连 | outbox/cursor 恢复；重复控制帧不重复打开 Session |
| 多个 Workgroup | Session、消息、HITL、terminal 和 cancel 互不串线 |
| 工具审批 | 单个/批量审批语义与 Node 一致；同一 `hitl_id` 合并成一个审批卡片，不重复展示或解析 |
| 成员进度 | 无实时订阅时仍可从 Timeline 恢复；同一 `event_seq` 不重复写入；新 epoch 可继续接收 |
| 工具失败/断线 | accepted 与结果状态分离；副作用未知进入 indeterminate |
| 归档成员 | 旧 WS 事件被 fencing；历史只读且不能继续 @ |

## 9. 实现入口

| 层 | 入口 |
|---|---|
| Manage Workgroup | `manage/workgroup/routes.py`、`turn_kernel.py`、`assignment_service.py`、`vertical.py`、`ws_hub.py` |
| Node Dialer/Worker | `node/internal/workgroup/dialer.go`、`worker.go` |
| Node Session | `node/internal/session/` |
| UI | `node/webui/frontend/src/views/WorkgroupView.vue`、`manage/console/frontend/` |
| 用户操作 | [`docs/user/workgroups.md`](../user/workgroups.md) |
