# `app/harness/memory/` REFERENCE

## `store.py`

- **`SqliteMessageStore`**：表 `session_history`（`session_id` + `content` BLOB）；`content` 为 `{"history","openai_messages","pending_tool_calls","run_turn_phase","summary_compression_phase","summary_compression_range_start","summary_compression_range_end","summary_compressed_message","tool_loop_count"}` 的 UTF-8 JSON；**`append_message`** / **`save_conversation_content`** / **`load_conversation_content`**；无旧版表/列自动迁移

