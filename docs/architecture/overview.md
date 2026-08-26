# 架构总览

> 当前跨组件总览见 [`docs/architecture.md`](../architecture.md)。本页保留在旧路径，方便从历史链接进入。

## 组件

```text
浏览器 ──HTTP/SSE──► Agent Node（Go + 内嵌 Web UI）
                           │
                           │ Node 主动 HTTPS/WSS
                           ▼
                    Manage（Python + Console，可选）
```

- Node 负责 Agent、Session/Turn/Step、LLM、工具、policy、HITL、本地历史与 UI。
- Manage 负责 Registry、Workgroup、Console、制品/Release 元数据和协作 Timeline。
- Node 是到 Manage 的连接发起方；Manage 不反向访问 Node。

## 现行边界

1. 一个 Node 可以承载多个真实 Agent，每个 Agent 有独立配置快照和主对话。
2. 一个 Agent 可以同时拥有个人 Session 和多个 Workgroup Session；Session 是消息、工具、HITL、终端和 SSE 的隔离单位。
3. Workgroup 成员是 `AgentRef(node_id + agent_id)`，不是影子 Agent；工具仍在成员 Agent 所在 Node 执行。
4. 工具定义通过模型 API 的 `tools` 字段发送；skill 正文通过独立 ContextInjection 在 Step 边界生效。

## 入口

| 主题 | 文档 |
|---|---|
| 跨组件设计 | [`../architecture.md`](../architecture.md) |
| Node 内部 | [`go-node-internals.md`](./go-node-internals.md) |
| Node HTTP/SSE | [`agent-node-api.md`](./agent-node-api.md) |
| Manage/Workgroup | [`../design/manage-architecture.md`](../design/manage-architecture.md)、[`../design/workgroup-and-node-gateway.md`](../design/workgroup-and-node-gateway.md) |
| 用户使用 | [`../user/README.md`](../user/README.md) |
| 开发和测试 | [`../development.md`](../development.md) |
