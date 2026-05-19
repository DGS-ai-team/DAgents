# `scripts/`

| 路径 | 说明 |
|------|------|
| **`migrate_runtime_layout.py`** | 将仓库根 **`history/*.jsonl`**、**`skills/`** 迁入 **`.runtime/history`**、**`.runtime/skills`**（幂等） |
| **`query_session_sqlite.py`** | 运维：按 **`session_id`** 读取 **`SqliteMessageStore`** sqlite（**`list`** / **`show`**）；依赖仓库根 **`PYTHONPATH=.`** |
| **`ci/`** | 仅 CI（PyInstaller 等）使用的构建脚本，详见 `ci/README.md` |
| **`windows/`** | Windows 托盘启动器（`pystray`），详见 `windows/README.md` |
