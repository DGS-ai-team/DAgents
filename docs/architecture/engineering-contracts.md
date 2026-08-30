# 工程边界契约

本文是 DAgents 在跨组件改动时使用的稳定边界摘要。字段的完整定义以代码类型、OpenAPI 和 `docs/design/workgroup-d05-contracts.md` 为准；这里不复制完整 Schema。

## 身份与作用域

| 标识 | 作用域 | 规则 |
|---|---|---|
| `agent_id` | Node 上的 Agent 实例 | 不等同于 Node 身份，也不代替会话身份 |
| `session_id` | Agent 对话运行时 | 消息、工具结果、终端和取消必须绑定到具体 session |
| `turn_id` / `step_id` | 一次执行及其步骤 | 事件恢复和幂等判断使用稳定 ID |
| `workgroup_id` | Manage 工作组 | ACL、Timeline、outbox 和快照的顶层隔离键 |
| `member_id` | 工作组成员 | 与 `home_node_id`、AgentRef 和成员代际一起校验 |
| `assign_id` / `run_id` | 工作组分派和运行 | 不能从展示文本或最近时间窗推断 |
| `delivery_seq` | WS 可靠投递 | 按工作组单调确认；与 Timeline `seq` 分离 |

## 工具执行边界

Node 的 `node/internal/tools` 已提供 provider-neutral 的 `ExecutionTarget`、`ExecRequest`、`ProcessEvent` 和 `ShellProvider`。调用方负责 policy、HITL、历史和结果展示；provider 只负责目标连接与进程生命周期。

新增执行目标时：

1. 复用请求上下文的身份字段，不把模型参数直接当作 provider 配置；
2. 返回结构化目标状态和稳定错误码；
3. 明确取消、超时、重试和副作用未知时的 `indeterminate` 行为；
4. 为本地、远端和失败恢复分别补测试，不以字符串错误作为协议。

## Workgroup WS 边界

- Node 主动建立连接，`session.hello` 后收到新的 `connection_generation`。
- `session.hello` 使用 `protocol_version=1` 和 `schema_version=0.5.0`，并携带节点工具清单 revision、能力列表和 `client_time`；认证仍由 WebSocket transport header 承担。
- `session.welcome` 返回 Manage 能力和 `server_time`；不识别的协议/Schema 版本必须以 `schema_mismatch` 终止握手。
- 旧连接的迟到帧必须被 fencing；新连接不能被旧连接的 disconnect 清理影响。
- outbox 使用 `delivery_seq` gap-fill；游标过旧时先 snapshot/full-resync，不自动重做可能有副作用的命令。
- `tool.ack` 表示可靠投递状态，`tool.result` 表示业务结果；两者不能混用。
- Assign 终态必须为 `succeeded`、`failed`、`canceled` 或 `indeterminate` 之一，并带稳定 `error_code`（失败时）。

公开 Timeline 可通过 `GET /v1/workgroups/{workgroup_id}/timeline/export.jsonl` 导出为 NDJSON。导出使用已持久化事实事件，不包含原始工具参数或结果正文；`limit` 最大为 5000。

## 状态与迁移

事实事件先落盘，`running`、`thinking`、`waiting_tool` 等是投影。新增持久化字段必须：

- 有显式 schema 版本或 migration entry；
- 迁移可重复执行，并有旧库测试；
- 失败时不伪造成功状态；
- 在发布说明中注明兼容窗口和移除版本。

## 前端约束

UI 只投影 SSE/WS/hydrate 的权威事实。组件可以合并展示状态，但不得以 watch 顺序、网络到达顺序或本地时间窗改变终态；切换 Agent、session 或工作组时必须清理旧订阅并校验身份。
