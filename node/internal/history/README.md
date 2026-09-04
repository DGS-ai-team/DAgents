# `node/internal/history`

原始 OpenAI 消息 **JSONL 审计侧车**（对齐原 Python `raw_message_journal`）。

**落盘路径**：`<workspace_root>/.dagents/<agent_id>/history/YYYYMMDD/<session_id>.jsonl`（按自然日分子目录；文件名经 sanitize；每个 Agent 在共享 workspace 中独立命名空间）。

Node 的 `memory/sessions.db` 仍是会话快照与恢复的控制面存储；本目录只保存原始消息审计侧车，不替代 SQLite。

| 文件 | 说明 |
|------|------|
| `journal.go` | `Journal`：按条追加 JSONL、`AppendMessage` / `InsertMessage`（调用方须先经 `llm.Client.NormalizeAssistant`） |
| `normalize.go` | JSONL `message` 快照（委托 `llm.MessageToJournalPayload`） |
| `journal_test.go` | 开关、JSONL 行结构 |

**reasoning_content 存储规范化**已迁至 **`node/internal/llm/`**（按 `llm.provider` 的 `MessageAdapter`）。
