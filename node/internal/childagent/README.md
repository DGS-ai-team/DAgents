# childagent

Go Node 临时子 Agent 生命周期、工具与 SSE。契约见 [`docs/architecture/child-agent-tools.md`](../../../docs/architecture/child-agent-tools.md)。

| 文件 | 说明 |
|------|------|
| `manager.go` | 创建 / 取消 / 等待 / 交付 |
| `policy.go` | 子 Agent 工具权限与首条 task 格式化 |
| `registry.go` | 子 Agent 受限工具表 |
| `relay_hub.go` | SSE 转发至父 session |
| `tools_handler.go` | 四个父 Agent 工具实现 |
| `parse.go` | 参数解析 |
