# 参考资料

这里放可以按字段、路径或事件查询的资料；产品用法见 [user](../user/README.md)，跨组件行为见 [architecture](../architecture.md)。

| 主题 | 权威入口 |
|---|---|
| Node HTTP/SSE API | [agent-node-api.md](../architecture/agent-node-api.md) · [openapi-node.yaml](../architecture/openapi-node.yaml) |
| 配置项 | [配置项参考](../handbook/附录/配置项参考.md) · [`config.example.yaml`](../../packaging/agent-client/config.example.yaml) |
| 内置工具/参数/审批 | [内置工具参考](../handbook/附录/内置工具参考.md) · `node/internal/tools/REFERENCE.md` |
| SSE 事件 | [SSE 事件速查](../handbook/附录/SSE事件速查.md) · `node/internal/stream/README.md` |
| Workgroup 协议 | [D0.5 契约](../design/workgroup-d05-contracts.md) · [fixtures](../design/fixtures/workgroup-d05/) |
| Workgroup 使用 | [工作组指南](../user/workgroups.md) |
| 观测 | [Prometheus](../handbook/附录/Prometheus观测.md) · `node/internal/logx/` |
| 回归验证 | [开发与验证](../development.md) · [UI 回归清单](../design/ui-e2e-regression-checklist.md) |

字段发生变化时，优先修改 Schema/OpenAPI 和代码同目录参考，再更新总览；不要只修改一篇叙述文档。
