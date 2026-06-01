# REFERENCE — `node/internal/history`

## `journal.go`

| 符号 | 类型 | 说明 |
|------|------|------|
| `Journal` | `struct` | JSONL 侧车；`enabled=false` 时仍规范化但不写盘 |
| `NewJournal` | `func(enabled bool, baseDir string, logger *slog.Logger) *Journal` | 构造 Journal |
| `(j *Journal) Enabled` | `method` | 是否写入 JSONL |
| `(j *Journal) RecordAppend` | `method` | 低层追加一行 JSONL；失败仅 warning |
| `(j *Journal) AppendMessage` | `method` | 规范化 → append history → JSONL |
| `(j *Journal) InsertMessage` | `method` | 规范化 → insert history → JSONL |

## `normalize.go`

| 符号 | 类型 | 说明 |
|------|------|------|
| `NormalizeMessageForContext` | `func(existing []llm.Message, message llm.Message, logger *slog.Logger) llm.Message` | 写入前协议规范化；tool_callback 继承最近 reasoning |
