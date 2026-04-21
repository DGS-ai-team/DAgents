# `app/harness/memory/` REFERENCE

## `store.py`

- **`SqliteMessageStore`**：表 `session_history`（`session_id` + `content` BLOB）；`content` 为 `{"history","openai_messages","pending_tool_calls","run_turn_phase","messages_total_tokens","tool_loop_count"}` 的 UTF-8 JSON；**`append_message`** / **`save_conversation_content`** / **`load_conversation_content`**；无旧版表/列自动迁移

