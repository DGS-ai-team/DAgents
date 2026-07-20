# 架构文档

> **项目手册** → [../handbook/README.md](../handbook/README.md)  
> **v0.8 重构设计** → [../design/agent-instance-model.md](../design/agent-instance-model.md)

本目录保留 **Node 内部结构、HTTP/SSE 契约** 等专题；与手册章节对应关系：

| 文件 | 手册章节 | 备注 |
|------|----------|------|
| `overview.md` | [01-愿景与架构](../handbook/01-愿景与架构.md) | |
| `go-node-internals.md` | [02-Agent-Node-核心](../handbook/02-Agent-Node-核心.md) | Phase 2 起按 AgentRuntime 修订 |
| `agent-node-api.md` | [03](../handbook/03-API与Client.md) | Phase 1 起按 `/v1/agents` 重写 |
| `child-agent-tools.md` | [04](../handbook/04-能力与策略.md) §5 | |
| `go-node-compatibility.md` | [06](../handbook/06-运维与案例.md) §3 | TUI 兜底叙述将废弃 |

## 已移除

- `local-assistant.md` — 多 Client（TUI）选型
- `client-packaging.md` — Client 同包发布

见 [agent-instance-model.md](../design/agent-instance-model.md)。
