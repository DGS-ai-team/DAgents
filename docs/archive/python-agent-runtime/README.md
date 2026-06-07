# Python Agent 运行时文档（已归档）

本目录存放 **已移除的 Python FastAPI Agent API**（`run_agent_api.py`、`app/harness/`、`app/core/main_agent/`）相关技术说明，仅供历史对照与 v1 语义迁移参考。

**当前本地 Agent 运行时**为 **Go Agent Node**（`node/`）：

| 主题 | 现行文档 |
|------|----------|
| Node 内部结构（runtime / orchestrator / queue） | [architecture/go-node-internals.md](../../architecture/go-node-internals.md) |
| HTTP / SSE 契约 | [architecture/agent-node-api.md](../../architecture/agent-node-api.md) |
| 临时子 Agent | [architecture/child-agent-tools.md](../../architecture/child-agent-tools.md) |
| 代码级说明 | [node/internal/session/README.md](../../../node/internal/session/README.md)、[node/internal/turn/README.md](../../../node/internal/turn/README.md) |

## 归档文件

| 文件 | 原路径 | 说明 |
|------|--------|------|
| [python-runtime.md](./python-runtime.md) | `docs/architecture/python-runtime.md` | Python 分层与主流程 |
| [architecture-and-flows.md](./architecture-and-flows.md) | `docs/architecture-and-flows.md` | 旧总览（已指向 python-runtime） |
| [api-reference.md](./api-reference.md) | `docs/api-reference.md` | Python HTTP/SSE 字段表 |
| [agent-input-output.md](./agent-input-output.md) | `docs/agent-input-output.md` | 入队、SSE、`connection_id` |
| [agent-turn-loop.md](./agent-turn-loop.md) | `docs/agent-turn-loop.md` | `MainAgentTurnOrchestrator` / `run_turn` |
