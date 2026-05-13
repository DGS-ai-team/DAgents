# `app/harness/`

| 子项 | 说明 |
|------|------|
| **`tools/`** | 工具注册（`tool.py` → `get_tools`） |
| **`queue/`** | 进程内消息队列（MVP，串行消费） |
| **`service/`** | 独立 Agent 服务（常驻消费消息队列并输出结果） |
| **`api/`** | FastAPI 接入层（统一客户端入口） |
| **`streaming/`** | 流式事件总线抽象（当前内存实现，预留 Redis 替换位） |
| **`memory/`** | 记忆层（已落地 `store.py`，提供 `SqliteMessageStore`） |
| **`cli/`** | 命令行入口（`main.py`） |
| **`skills/`** | skills 能力模块：扫描标准 `skills/*/SKILL.md`（frontmatter + 正文），并渲染 prompt 片段 |
| **`history/`** | 原始 OpenAI 消息 JSONL 按条记录（按会话、按日滚动）；详见子目录 README |

Agent 组装在 **`app/core/main_agent/agent.py`**（**`init_agent`**）。
