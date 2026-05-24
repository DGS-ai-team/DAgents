# `app/harness/memory/` REFERENCE

## `store.py`

- **`SqliteMessageStore`**：表 `session_history`（`session_id` + `content` BLOB + `first_request_message`）；`content` 为 JSON；**`list_session_summaries`** / **`delete_session_if_exists`**；列 `first_request_message` 在初始化时按需 `ALTER TABLE` 追加

