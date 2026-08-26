# Workgroup Agent Membership v1

## 状态

本文件定义 v0.10.0 的目标架构。它取代旧的“工作组成员由 Manage 创建 MemberSpec，再由 Node provision WorkerBinding”的默认产品路径；旧路径在迁移期仅作为 legacy 兼容路径保留。

## 网络硬约束

- Node 是唯一的连接发起方。
- Node 主动建立到 Manage 的 `wss://` 长连接。
- Manage 不访问 Node 的 HTTP、WS 或 callback endpoint。
- Manage 向 Node 的所有控制都通过 Node 已建立的 WS 发送。
- Node → Manage 的旧 HTTP 接口可以兼容保留，但不是新运行时控制链路。

## 身份

| 标识 | 含义 |
| --- | --- |
| `node_id` | Node 进程/主机身份 |
| `agent_id` | Node 中真实持久化的 Agent |
| `member_id` | Agent 在一个 Workgroup 中的成员身份 |
| `session_id` | Agent 的独立消息、上下文和运行队列 |
| `stream_id` | WS 事件流，通常为 `agent_id:session_id` |
| `run_id` / `turn_id` / `step_id` | 运行、LLM 往返和单步执行身份 |

`member_id` 不能替代 `agent_id`。同一个 Agent 可以拥有个人 Session，并同时加入多个 Workgroup，每个 Workgroup 使用不同的 Session。

## 权威边界

- Node 权威保存 Agent 的 prompt、tools、skills、memory、LLM 配置和实际 Session 历史。
- Manage 权威保存 Agent 目录、Workgroup 成员引用、ACL、策略覆盖、Timeline 和运行投影。
- Workgroup 策略只能收紧 Agent 权限：
  `Agent allowlist ∩ Workgroup policy ∩ Node policy`。
- Workgroup Timeline 不自动等同于 Agent Session 上下文。

## 核心 WS 消息

Node → Manage：

```text
session.hello
agent.catalog.snapshot
agent.catalog.delta
agent.session.ready
agent.session.state
agent.turn.accepted
agent.turn.event
agent.turn.result
agent.session.reconcile
delivery.ack
```

Manage → Node：

```text
session.welcome
agent.catalog.resync
agent.session.open
agent.session.close
agent.turn.start
agent.turn.cancel
agent.hitl.resolve
```

每个可靠业务 envelope 必须带上：

```text
envelope_id, delivery_seq, stream_id, stream_seq,
connection_generation, node_id, agent_id, session_id,
workgroup_id, member_id, assign_id, run_id, turn_id, step_id
```

`delivery_seq` 负责 Node↔Manage 传输恢复；`stream_seq` 负责单个 Session 内的事件顺序；Workgroup Timeline 继续使用自己的公共 `seq`。

## 成员绑定

新的成员请求只引用已有 Agent：

```json
{
  "agent_id": "agt_xxx",
  "display_name": "代码审查员",
  "role": "reviewer",
  "policy_overlay": {"deny_tools": ["network"]},
  "conversation_mode": "persistent"
}
```

Manage 创建成员后状态为 `binding`，通过 WS 发送 `agent.session.open`。Node 验证本地 Agent、创建/恢复 Session，返回 `agent.session.ready`；Manage 再将成员投影为 `ready`。Node 离线时保持 `waiting_for_node`，不得伪造 ready。

## 并发和隔离

- 同一 `session_id` 内单写者、FIFO。
- 不同 Session 可以并行。
- Agent 有全局并发、LLM 额度和副作用资源限制。
- personal、Workgroup member、isolated Assign 使用不同 Session。
- messages、tools、memory、terminal、async callback、HITL、cancel 和 stream 全部以 `agent_id + session_id` 为隔离键。
- 同一个 Agent 的共享工作区默认对副作用操作串行；只读操作是否并行由策略决定。

## 状态权威

前端只能根据权威事件或持久化状态机展示：

```text
Node: online / offline / reconnecting
Agent: available / archived / degraded
Session: opening / ready / running / awaiting_hitl / closed / error
Turn: queued / accepted / running / completed / failed / canceled / indeterminate
```

不得通过 HTTP 请求是否返回、前端是否仍在等待或缺少事件来推断 Agent 已 ready 或 turn 已完成。

## 迁移原则

- 新成员默认使用 `AgentRef` 路径。
- 旧 `MemberSpec` / `WorkerBinding` 标记为 `legacy_provisioned`。
- 不自动猜测旧成员对应哪个已有 Agent。
- 用户可以显式“替换为已有 Agent”或“根据旧配置创建本地 Agent”。
- 新旧路径可在同一 Workgroup 并存，直到旧路径完成迁移。

## v0.10.0 已落地范围

本版本已落地以下最小闭环：

- Node 在注册心跳之外发布本地 Agent 目录；Manage 以 `agent_id` 为主键保存 Agent Registry，`node_id` 仅表示承载 Node。
- AgentRef 成员创建通过已有 Agent 绑定，并为每个 `workgroup_id + member_id` 分配稳定的独立 `session_id`；不再为新成员创建受限 Agent。
- Manage 通过 Node 已建立的出站 WS 下发 `agent.session.open`、`agent.turn.start`、`agent.turn.cancel`、`agent.session.close`；Node 回传 ready、事件和结果。
- Node 运行时按 Agent 快照构建 Agent，显式使用该 Agent 的 prompt、skills、tools、memory 和 LLM 配置；Workgroup Session 使用独立历史和记忆命名空间。
- Manage 对 AgentRef turn 做 outbox、waiter、assign 状态和 late-result fencing；重复 start 在 Node 侧幂等，完成结果可重放。
- AgentRef 成员归档通过 `agent.session.close` 关闭绑定；open/start/cancel/close 的控制帧均在响应写出后推进 delivery ACK，归档后的迟到 close/ready/error 不会复活成员。
- Manage 对同一 Node 的 resume 重放和实时 outbox 投递使用连接级发送串行化；Node 对已被更高游标覆盖的迟到低序 ACK 做幂等忽略，避免无意义断线。
- 旧 `member.provision` / `tool.command` 保留为迁移期兼容协议，不作为新的 AgentRef 产品路径。

以下能力仍属于后续增强，而不是 v0.10.0 已完成承诺：WSS/TLS 端到端部署验证、目录 delta/reconcile、断线恢复的真实网络演练、HITL/终端在 AgentRef Session 下的真实联调、Workgroup 工具策略 overlay 的完整执行，以及真实 LLM 成本和限额策略。
