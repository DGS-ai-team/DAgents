# Architecture v2 设计文档

本目录存放 **DAgents 双运行时架构重构** 的设计文档。重构目标：将 DAgents 从一个"只能在 Python 能跑的 OS 上运行"的单运行时系统，演进为"Python 做 Agent 大脑，Go 做跨平台执行代理"的双运行时系统。

## 文档索引

| 文件 | 说明 |
|------|------|
| [background-and-motivation.md](./background-and-motivation.md) | **重构背景与动机**：OS 兼容性瓶颈、Python 在老系统上的局限、为什么必须引入第二运行时 |
| [agent-dual-runtime.md](./agent-dual-runtime.md) | **Agent 双运行时架构设计**：Agent = 大脑(Python) + 身体(本地或Go Proxy)；终端/非终端 Agent 分类、Go Proxy 设计、跨 OS 协作流程 |
| [runtime-split.md](./runtime-split.md) | **Python-Go 功能划分**：大脑层与身体层的职责边界、各自优劣势、适用场景决策树 |
| [identity-and-session.md](./identity-and-session.md) | **身份与会话模型**：agent_id / session_id / client_id / connection_id 四层身份体系、生命周期管理、A2A 会话 |
| [deployment-and-ops.md](./deployment-and-ops.md) | **部署与运维指南**：部署拓扑、配置分层、健康检查、启动顺序、监控指标 |

## 重构范围

```
Python 后端：~200 行新增 + ~50 行调整，核心代码 90% 不变
Go 新增：  go-proxy (~400 行) + go-tui (~800 行)，全新独立仓库
Register Center：agent_type / schedulable / host_info 三个可选字段，向后兼容
```

## 与现有文档的关系

本目录是现有 [`doc/`](../) 的补充。架构细节以当前实现的 [architecture-and-flows.md](../architecture-and-flows.md) 为准；A2A 协议以 [a2a-and-register-center.md](../a2a-and-register-center.md) 为准；OS 兼容性以 [os-compatibility.md](../os-compatibility.md) 为准。本目录文档描述的是**计划中的 v2 目标形态**，实现时以实际代码和 CHANGELOG 为准。
