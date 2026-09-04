# Manage 与 Workgroup 架构

> **状态**：现行架构说明
>
> Manage 是目录、工作组和控制台服务；Node 是 Agent、LLM、工具和本地数据的运行时。旧 A2A Task/Inbox 与 Placement 仅在 [`docs/archive/`](../archive/README.md) 中保留历史，不是当前产品路径。

## 1. 责任边界

| 组件 | 负责 | 不负责 |
|---|---|---|
| Agent Node（Go） | Agent 实例、Session/Turn/Step、LLM、工具、policy、HITL、本地历史与 Web UI | 管理全网目录；直接访问其他 Node |
| Manage（Python） | Node/Agent Registry、Workgroup、ACL、成员引用、Timeline、Console、LLM/制品/Release 元数据 | 代替 Node 执行本地工具；主动请求 Node HTTP |
| Manage Console（Vue） | 管理 Node、Agent、工作组和审计视图 | 持有 Node 的本地工具权限 |

核心原则：

1. Node 是连接发起方。注册、心跳和 Workgroup 控制都由 Node 主动请求或主动建立 WS。
2. Manage 不反向调用 Node HTTP，也不要求 Node 暴露可被 Manage 回调的地址。
3. 跨机 Agent 协作统一走 Workgroup；本地 Agent 个人对话仍由 Node 独立处理。
4. Manage 保存协作状态和投影，Node 保存被引用 Agent 的真实运行状态。

## 2. 组件关系

```text
浏览器 ──HTTP/SSE──► Node Web UI / Node API
                         │
                         │ Node 主动 HTTPS 注册/心跳
                         │ Node 主动 WSS 建立 Workgroup Dialer
                         ▼
                    Manage（Registry + Workgroup + Console）
                         │
                         ├─ Workgroup Timeline / outbox
                         ├─ AgentRef / 成员 Session 状态
                         └─ tool.execute / turn 控制帧
```

Node 与 Manage 的控制链路只有出站连接，网络受限环境只需允许 Node 访问 Manage 的 HTTPS/WSS 地址。

## 3. Registry

Node 启动后登记 Node 身份以及本地 Agent 目录，周期性发送心跳和能力快照。Manage 的目录至少区分：

- `node_id`：Node 进程/主机身份；
- `agent_id`：Node 中真实存在的持久化 Agent；
- `display_name`、角色、描述、能力摘要和在线状态；
- `catalog_revision`：目录变化的版本，用于增量同步或重新对账。

`agent_id` 不再等同于 Node 身份。工作组添加成员时引用现有 `agent_id`，不能把 Node 注册行误当作 Agent，也不能隐式创建一个受限 Agent。

## 4. Workgroup

工作组由 Manage 持久化并负责公开协作视图：

```text
Workgroup
 ├─ supervisor（Manage 侧编排身份）
 ├─ members[] = AgentRef（agent_id + node_id）
 ├─ ACL / subscription
 ├─ Assign / HITL / approval 状态
 └─ Timeline + reliable outbox
```

每个 `workgroup_id + member_id` 拥有独立的 Workgroup Session。它使用同一个 Node 上 Agent 的能力和配置，但不与 Agent 的个人对话共享消息历史、HITL、工具结果或取消状态。

成员的有效工具权限按收紧规则计算：

```text
Agent 工具快照 ∩ Workgroup policy overlay ∩ Node 本地 policy
```

Manage 可以收紧范围，不能绕过 Node 本地 policy；不存在的工具必须在执行前明确失败。

## 5. Node ↔ Manage WS

Node 负责拨号、认证、重连和游标恢复。Manage 只在已建立的连接上发送控制帧。

### Node → Manage

`hello/resume`、Agent catalog、Session ready/state、turn event/result、Timeline ack、reconcile。

### Manage → Node

`welcome`、catalog resync、`agent.session.open/close`、`agent.turn.start/cancel`、HITL resolve 和工具执行请求。

可靠 envelope 应至少携带：

```text
envelope_id, delivery_seq, stream_id, stream_seq,
connection_generation, node_id, agent_id, session_id,
workgroup_id, member_id, assign_id, run_id, turn_id, step_id
```

- `delivery_seq`：Node 与 Manage 之间的投递恢复；
- `stream_seq`：一个 Agent/Workgroup Session 内的事件顺序；
- `connection_generation`：新连接使旧连接失效；
- `envelope_id` / `command_id`：幂等去重。

WS 不是事实存储。Manage 的 outbox、Node 的本地 journal 和游标共同承担断线恢复；只有确认已持久化的命令才能返回 accepted。

## 6. 成员 Session 与并发

同一个 Session 采用单写者模型：消息、HITL 恢复、工具回灌和取消在同一队列中按顺序处理。不同 Session 可以并行。

同一个 Agent 可以同时拥有：

- 个人 Session；
- 一个或多个 Workgroup member Session；
- 临时子 Agent Session（仅 Node 内部）。

隔离键必须覆盖 `agent_id + session_id`，必要时再带上 `workgroup_id + member_id`。Terminal、异步工具回调、HITL、cancel 和 SSE 不能只按 `agent_id` 路由，否则会把一个会话的状态投影到另一个会话。

## 7. 状态与故障处理

前端只消费权威持久化状态和事件，不用 HTTP 请求是否返回、轮询是否超时或本地 loading 推断业务完成。

```text
Node:      online / offline / reconnecting
Agent:     available / archived / degraded
Session:   opening / ready / running / awaiting_hitl / closed / error
Turn:      queued / accepted / running / completed / failed / canceled / indeterminate
Member:    binding / waiting_for_node / ready / archived / error
```

工具已经 accepted 但连接中断时不得自动重做非幂等副作用；应进入 `indeterminate`，等待查询或人工处理。旧连接的 late event 不能复活已归档成员。

## 8. 代码入口

| 能力 | 入口 |
|---|---|
| Manage HTTP/Console | `manage/manage_app.py`、`manage/*/routes.py` |
| Workgroup 状态与 turn | `manage/workgroup/turn_kernel.py`、`store.py`、`routes.py` |
| Manage WS | `manage/workgroup/ws_hub.py`、`ws_routes.py` |
| Node Registrar/Dialer | `node/internal/manage/`、`node/internal/workgroup/dialer.go` |
| Node Worker/成员 Session | `node/internal/workgroup/` |
| 现行用户路径 | [`docs/user/workgroups.md`](../user/workgroups.md) |
| 契约与字段 | [`workgroup-d05-contracts.md`](./workgroup-d05-contracts.md) |

## 9. 明确不做

- Manage 主动访问 Node HTTP 或 callback；
- Node-to-Node 直连派活；
- 通过工作组成员创建另一个“受限 Agent”；
- 把公开 Timeline 原样当作成员 Agent 的完整模型上下文；
- 以旧 A2A inbox、Placement 或 `agent_invoke` 作为新功能入口。
