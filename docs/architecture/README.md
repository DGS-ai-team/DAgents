# `docs/architecture/`

当前已落地的运行时架构说明（非远期规划）。

| 文件 | 说明 |
|------|------|
| [overview.md](./overview.md) | 选型总览（Go Node + Register Center） |
| [go-node-internals.md](./go-node-internals.md) | **Go Node 内部结构**：Manager、runtime、queue、Orchestrator |
| [local-assistant.md](./local-assistant.md) | Go Node + 双 Client 联调 |
| [agent-node-api.md](./agent-node-api.md) | Agent Node HTTP/SSE API（含 §2.4.1 `done` 语义） |
| [child-agent-tools.md](./child-agent-tools.md) | 临时子 Agent 工具 / HTTP / SSE |
| [client-packaging.md](./client-packaging.md) | 同包配置与安装 |
| [go-node-compatibility.md](./go-node-compatibility.md) | 静态构建与 glibc 矩阵 |
| [rhel6-acceptance-checklist.md](./rhel6-acceptance-checklist.md) | RHEL 6.9 验收清单 |

已移除的 Python Agent 运行时说明见 [../archive/python-agent-runtime/](../archive/python-agent-runtime/)；`python-runtime.md` 为跳转桩。
