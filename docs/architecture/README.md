# 深入架构与协议

跨组件架构的入口已统一到 [../architecture.md](../architecture.md)。本目录只保留需要按接口或源码深入阅读的材料，不再承担产品介绍、路线图或历史版本验收。

| 文档 | 内容 | 状态 |
|---|---|---|
| [agent-node-api.md](agent-node-api.md) | Node HTTP/SSE API 详细契约 | 当前参考 |
| [openapi-node.yaml](openapi-node.yaml) | Node OpenAPI 文档 | 机器可验证 |
| [go-node-internals.md](go-node-internals.md) | Go Node 内部调用与 runtime 结构 | 当前参考 |
| [child-agent-tools.md](child-agent-tools.md) | 临时子 Agent 工具与生命周期 | 当前参考 |
| [overview.md](overview.md) | 旧版架构总览兼容页 | 逐步收敛到 `docs/architecture.md` |

构建矩阵、RHEL 旧环境和历史 Client/TUI 验收已不再是当前架构正文，见 [archive/README.md](../archive/README.md)。
