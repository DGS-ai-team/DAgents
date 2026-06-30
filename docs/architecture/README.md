# 架构文档

> **项目手册** → [../handbook/README.md](../handbook/README.md)

本目录保留 **Node 内部结构、HTTP/SSE 契约、打包兼容** 等专题正文；与手册章节对应关系：

| 文件 | 手册章节 |
|------|----------|
| `overview.md` | [01-愿景与架构](../handbook/01-愿景与架构.md) |
| `go-node-internals.md` | [02-Agent-Node-核心](../handbook/02-Agent-Node-核心.md) |
| `local-assistant.md` | [03-API与Client](../handbook/03-API与Client.md) |
| `agent-node-api.md` | [03](../handbook/03-API与Client.md) + [SSE事件速查](../handbook/附录/SSE事件速查.md) |
| `child-agent-tools.md` | [04](../handbook/04-能力与策略.md) §5 |
| `client-packaging.md` | [06](../handbook/06-运维与案例.md) §2 |
| `go-node-compatibility.md` | [06](../handbook/06-运维与案例.md) §3 |

新正文优先写入 **handbook**；本目录仅补充需与源码路径并列维护的专题。
