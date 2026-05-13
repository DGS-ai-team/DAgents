# `app/harness/memory/`

| 文件 | 说明 |
|------|------|
| **`store.py`** | 记忆模块核心：`SqliteMessageStore`（每会话一行 `content` BLOB：`history` 与 runtime/summary 常驻字段的 UTF-8 JSON）；运维按 **`session_id`** 查询见仓库 **`scripts/query_session_sqlite.py`** |
| **`REFERENCE.md`** | 本目录 Python 符号索引 |
