# `app/harness/`

> **【已弃用】** 本目录为 Python Agent 运行时主体（API → AgentService → turn loop）。替代：**[`node/`](../../node/README.md)**。

| 子项 | 说明 |
|------|------|
| **`tools/`** | 工具注册（`tool.py` → `get_tools`） |
| **`queue/`** | 进程内消息队列（MVP，串行消费） |
| **`service/`** | 独立 Agent 服务（常驻消费消息队列并输出结果） |
| **`api/`** | FastAPI 接入层（统一对外入口；响应带 `Deprecation` 头） |
| **`streaming/`** | 流式事件总线抽象（当前内存实现，预留 Redis 替换位） |
| **`memory/`** | 记忆层（已落地 `store.py`，提供 `SqliteMessageStore`） |
| **`skills/`** | skills 能力模块 |
| **`history/`** | 原始 OpenAI 消息 JSONL |
| **`triggers/`** | 触发器 MVP（interval / fire_at）；完整能力见 Go [`node/internal/triggers/`](../../node/internal/triggers/README.md) |

Agent 组装在 **`app/core/main_agent/agent.py`**（**`init_agent`**）。
