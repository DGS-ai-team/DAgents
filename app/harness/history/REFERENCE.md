# `app/harness/history/` REFERENCE

## `raw_message_journal.py`

- **`_sanitize_session_id_for_filename`**：会话 ID → 安全文件名片段（长度上限、非法字符替换）
- **`_journal_file_path`**：解析当日 JSONL 路径 **`{safe_sid}_{YYYYMMDD}.jsonl`**（基础目录 **`resolve_repo_relative_path(agent_raw_message_history_dir)`**）
- **`record_raw_openai_message_append`**：向 JSONL 追加一行 **`{"recorded_at", "message"}`**（失败仅打日志）
- **`append_openai_message_with_journal`**：**`deepcopy`** → **`ctx.messages.append`** → JSONL 记录
- **`insert_openai_message_with_journal`**：**`deepcopy`** → **`ctx.messages.insert`** → JSONL 记录

## `__init__.py`

- 重新导出 **`raw_message_journal`** 中的公开符号（见 **`__all__`**）
