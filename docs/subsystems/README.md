# 子系统参考

借鉴 DeepSeek Harness 的做法，每个子系统只拥有一份职责和术语说明；总架构不重复包内 API。优先阅读下表中的代码同目录 README/REFERENCE。

## Node

| 子系统 | 代码 | 说明 |
|---|---|---|
| API / Server | `node/internal/api/` | HTTP、SSE、Web UI 和工作组反代 |
| Session | `node/internal/session/` | Manager、runtime、队列消费、持久化边界 |
| Turn | `node/internal/turn/` | 单 Step 模型循环、工具路由、HITL |
| Queue | `node/internal/queue/` | 多来源消息、优先级和 FIFO |
| Tools | `node/internal/tools/` | Registry、Schema、执行、结果契约 |
| Policy / Hooks | `node/internal/policy/`、`hooks/` | 审批策略和执行前后扩展 |
| Skills / Prompt | `node/internal/skills/`、`promptcontext/` | 技能发现、加载和 ContextInjection |
| Compression | `node/internal/compression/` | 上下文压缩和缓存相关计量 |
| Terminal / MCP / Browser | `terminal/`、`mcp/`、`browser/` | 外部执行和连接能力 |
| Workgroup / Manage client | `workgroup/`、`manage/` | Node 主动连接 Manage 的跨机协作 |

## Manage

| 子系统 | 代码 | 说明 |
|---|---|---|
| Platform | `manage/platform/` | 鉴权、Blob、指标等控制面基础设施 |
| Registry | `manage/registry/` | Node 注册、心跳、Agent 目录 |
| Workgroup | `manage/workgroup/` | Leader、assign、HITL、Timeline、WS outbox |
| Storage | `manage/storage/` | SQLite 与持久化边界 |
| Console | `manage/console/frontend/` | Manage 管理界面 |

## 契约和前端

- API/OpenAPI：[`../architecture/agent-node-api.md`](../architecture/agent-node-api.md)、[`../architecture/openapi-node.yaml`](../architecture/openapi-node.yaml)
- Workgroup schemas/fixtures：[`../design/fixtures/workgroup-d05/`](../design/fixtures/workgroup-d05/)
- Node UI：`node/webui/frontend/src/`
- Manage Console：`manage/console/frontend/src/`
- 公共配置：`shared/config/`

代码包中的 README 是实现导航，`REFERENCE.md` 是符号参考；行为变更同时更新 [reference](../reference/README.md) 中的契约入口。
