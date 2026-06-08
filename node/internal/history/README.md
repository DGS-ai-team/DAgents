# `node/internal/history`

原始 OpenAI 消息 **JSONL 审计侧车**（对齐原 Python `raw_message_journal`）。

| 文件 | 说明 |
|------|------|
| `journal.go` | `Journal`：按条追加 JSONL、`AppendMessage` / `InsertMessage`（调用方须先经 `llm.Client.NormalizeAssistant`） |
| `normalize.go` | JSONL 行 `message` 快照序列化（assistant+tool_calls 保留 `reasoning_content` 键） |
| `journal_test.go` | 开关、JSONL 行结构 |

**reasoning_content 存储规范化**已迁至 **`node/internal/llm/`**（按 `llm.provider` 的 `MessageAdapter`）。
