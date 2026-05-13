# `app/context/`

| 文件 | 说明 |
|------|------|
| **`models.py`** | 会话上下文核心模型：`MessageRecord`、`ConversationContext`、`PendingToolCall`、`OpenAIConversationContext`，以及 runtime 对齐常量与阶段枚举（含 `RunTurnPhase` 与 `SummaryCompressionPhase`） |
| **`REFERENCE.md`** | 本目录 Python 符号索引 |

说明：

- 本目录统一承载会话上下文相关模型与转换逻辑。
- 上下文持久化、压缩与 **`ctx`** 字段演变的专题叙述见仓库 **`doc/context-compression-and-state.md`**。
