# 原始消息 JSONL（`node/internal/history`）

对齐 Python `app/harness/history/raw_message_journal.py`：在业务路径向会话 `messages` 追加/插入消息时，将**插入瞬间**快照按 `session_id` + 当日 `YYYYMMDD` 写入 JSONL。

| 文件 | 说明 |
|------|------|
| `journal.go` | `Journal`：按条追加 JSONL、`AppendMessage` / `InsertMessage` |
| `normalize.go` | `NormalizeMessageForContext`：assistant + tool_calls 的 reasoning 规范化 |
| `journal_test.go` | 开关、JSONL 行结构、insert/tool_callback 单测 |

JSONL 根目录固定为 **`<runtime>/history`**（配置见 `shared/config` 的 `RawMessageHistoryDir()`）；开关 **`AGENT_RAW_MESSAGE_HISTORY_ENABLED`** 或 YAML `raw_message_history.enabled`（默认 true）。摘要压缩等整段替换**不会**触发 JSONL 写入。
