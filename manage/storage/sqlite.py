"""Manage SQLite 连接与 schema 初始化。"""

from __future__ import annotations

import sqlite3
import threading
from pathlib import Path


class SQLiteDatabase:
    def __init__(self, path: Path | None) -> None:
        self._path = path
        self._lock = threading.RLock()
        if self._path is not None:
            self._path.parent.mkdir(parents=True, exist_ok=True)
            self._init_schema()

    @property
    def enabled(self) -> bool:
        return self._path is not None

    def connect(self) -> sqlite3.Connection:
        if self._path is None:
            raise RuntimeError("MANAGE_DB_PATH 未配置")
        conn = sqlite3.connect(self._path, check_same_thread=False)
        conn.row_factory = sqlite3.Row
        return conn

    def _init_schema(self) -> None:
        with self._lock, self.connect() as conn:
            conn.executescript(
                """
                CREATE TABLE IF NOT EXISTS schema_meta (
                    key TEXT PRIMARY KEY,
                    value TEXT NOT NULL
                );
                INSERT OR IGNORE INTO schema_meta(key, value) VALUES ('schema_version', '2');

                CREATE TABLE IF NOT EXISTS registry_agents (
                    agent_id TEXT PRIMARY KEY,
                    payload_json TEXT NOT NULL
                );

                CREATE TABLE IF NOT EXISTS a2a_tasks (
                    task_id TEXT PRIMARY KEY,
                    payload_json TEXT NOT NULL
                );

                CREATE TABLE IF NOT EXISTS llm_configs (
                    id TEXT PRIMARY KEY,
                    payload_json TEXT NOT NULL
                );
                CREATE TABLE IF NOT EXISTS skill_packages (
                    skill_id TEXT NOT NULL,
                    version TEXT NOT NULL,
                    payload_json TEXT NOT NULL,
                    PRIMARY KEY (skill_id, version)
                );
                CREATE TABLE IF NOT EXISTS case_examples (
                    case_id TEXT PRIMARY KEY,
                    payload_json TEXT NOT NULL
                );
                -- Blob 元数据随内容寻址文件落在 MANAGE_BLOB_DIR/{sha256}.json sidecar，
                -- 不入 SQLite；故此处不建 blobs 表。
                """
            )
            conn.execute(
                "INSERT INTO schema_meta(key,value) VALUES('schema_version','4') "
                "ON CONFLICT(key) DO UPDATE SET value='4'"
            )
            conn.commit()
