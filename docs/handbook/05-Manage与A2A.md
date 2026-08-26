# 05 · Manage 与协作面

本章保留旧文件名；跨机协作当前不再使用旧的 A2A inbox/placement 路径，现行产品入口是 [工作组指南](../user/workgroups.md)，跨组件行为见 [架构](../architecture.md)。

## Manage 的职责

Manage 是 Python 控制面，负责：

- Node 注册、心跳、Agent 目录和能力发现；
- Workgroup、ACL、成员引用、assign、HITL、Timeline 和 RunHistory；
- Console、LLM/skills/plugin/release/case 等管理能力；
- 持久化 outbox 和 Node 重连后的游标恢复。

Manage 不负责本地 Agent 的 turn loop、工具执行或终端，也不主动向 Node 发 HTTP。Node 通过出站 WebSocket 建立控制链路，所有 Manage→Node 控制帧都复用这条连接。

## Agent 与 Workgroup

工作组成员引用 Node 上已注册的 `agent_id`。成员的模型、工具、skills 和本地执行环境以该 Agent/Node 的实际能力为基础，Manage 不再创建一个与本地 Agent 脱节的受限副本。Supervisor 由 Manage 的工作组运行时负责编排；成员工具仍在 Home Node 执行。

```text
Node Registrar / Dialer ──出站 WS──► Manage
Node Web UI ──HTTP/SSE──► Node ──Workgroup API/WS──► Manage
Manage Console ──HTTP──► Manage
```

## 继续阅读

- [工作组用户指南](../user/workgroups.md)
- [Workgroup 契约](../design/workgroup-d05-contracts.md)
- [Node↔Manage 设计](../design/workgroup-and-node-gateway.md)
- [Node manage 包](../../node/internal/manage/README.md)
- [Manage README](../../manage/README.md)
