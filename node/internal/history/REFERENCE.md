# REFERENCE — `node/internal/history`

## `journal.go`

| 符号 | 类型 | 说明 |
|------|------|------|
| `Journal` | `struct` | JSONL 侧车；`enabled=false` 时仍 append history 但不写盘 |
| `NewJournal` | `func(enabled bool, baseDir string, logger *slog.Logger) *Journal` | 构造 Journal |
| `(j *Journal) Enabled` | `method` | 是否写入 JSONL |
| `(j *Journal) RecordAppend` | `method` | 低层追加一行 JSONL；失败仅 warning |
| `(j *Journal) AppendMessage` | `method` | append 已规范化 history → JSONL |
| `(j *Journal) InsertMessage` | `method` | insert 已规范化 history → JSONL |
| `journalFilePath` | `func(baseDir, sessionID string) string` | `<baseDir>/YYYYMMDD/<session>.jsonl` |
| `RuntimeJournalRelativePath` | `func(sessionID string, at time.Time) string` | 未绑定 Agent 的兼容路径 `history/YYYYMMDD/<session>.jsonl`；正常 Agent 使用 workspace 下 `.dagents/<agent_id>/history/` 前缀 |

## 相关

`reasoning_content` 写入前规范化见 **`node/internal/llm/provider*.go`** 与 **`Client.NormalizeAssistant`**；JSONL message 对象由 **`llm.MessageToJournalPayload`** 生成。
