# `app/harness/history/`

| 子项 | 说明 |
|------|------|
| **`raw_message_journal.py`** | 在业务路径向 **`OpenAIConversationContext.messages`** 追加/插入消息时，将**插入瞬间**的原始 dict 快照按 **`session_id` + 当日 `YYYYMMDD`** 写入 JSONL（摘要压缩等整表替换**不会**触发） |
| **`__init__.py`** | 导出 **`append_openai_message_with_journal`** / **`insert_openai_message_with_journal`** / **`record_raw_openai_message_append`** |

JSONL 根目录固定为 **`<运行根>/.runtime/history`**（**`runtime_layout.raw_message_history_dir()`**）；开关 **`AGENT_RAW_MESSAGE_HISTORY_ENABLED`**。
