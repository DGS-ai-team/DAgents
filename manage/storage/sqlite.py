"""Manage SQLite 连接与 schema 初始化。"""

from __future__ import annotations

import sqlite3
import threading
from pathlib import Path


class _ClosingConnection(sqlite3.Connection):
    """Close SQLite connections when their transaction context exits."""

    def __exit__(self, exc_type, exc_value, traceback):
        try:
            return super().__exit__(exc_type, exc_value, traceback)
        finally:
            self.close()


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
        conn = sqlite3.connect(
            self._path,
            check_same_thread=False,
            factory=_ClosingConnection,
        )
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
                INSERT OR IGNORE INTO schema_meta(key, value) VALUES ('schema_version', '15');

                CREATE TABLE IF NOT EXISTS registry_agents (
                    agent_id TEXT PRIMARY KEY,
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
                CREATE TABLE IF NOT EXISTS release_packages (
                    artifact TEXT NOT NULL,
                    channel TEXT NOT NULL,
                    platform TEXT NOT NULL,
                    version TEXT NOT NULL,
                    payload_json TEXT NOT NULL,
                    PRIMARY KEY (artifact, channel, platform, version)
                );
                CREATE TABLE IF NOT EXISTS case_examples (
                    case_id TEXT PRIMARY KEY,
                    payload_json TEXT NOT NULL
                );
                CREATE TABLE IF NOT EXISTS externaltool_packages (
                    tool_id TEXT NOT NULL,
                    version TEXT NOT NULL,
                    payload_json TEXT NOT NULL,
                    PRIMARY KEY (tool_id, version)
                );
                CREATE TABLE IF NOT EXISTS plugin_packages (
                    plugin_id TEXT NOT NULL,
                    version TEXT NOT NULL,
                    payload_json TEXT NOT NULL,
                    PRIMARY KEY (plugin_id, version)
                );
                -- Workgroup D1：组 / ACL / 成员 / Spec / Assign / Run
                CREATE TABLE IF NOT EXISTS workgroups (
                    id TEXT PRIMARY KEY,
                    payload_json TEXT NOT NULL
                );
                CREATE TABLE IF NOT EXISTS workgroup_acls (
                    id TEXT PRIMARY KEY,
                    workgroup_id TEXT NOT NULL,
                    payload_json TEXT NOT NULL
                );
                CREATE TABLE IF NOT EXISTS workgroup_members (
                    id TEXT PRIMARY KEY,
                    workgroup_id TEXT NOT NULL,
                    payload_json TEXT NOT NULL
                );
                CREATE TABLE IF NOT EXISTS member_specs (
                    id TEXT PRIMARY KEY,
                    workgroup_id TEXT NOT NULL,
                    payload_json TEXT NOT NULL
                );
                CREATE TABLE IF NOT EXISTS workgroup_assigns (
                    id TEXT PRIMARY KEY,
                    workgroup_id TEXT NOT NULL,
                    payload_json TEXT NOT NULL
                );
                CREATE TABLE IF NOT EXISTS actor_runs (
                    id TEXT PRIMARY KEY,
                    workgroup_id TEXT NOT NULL,
                    payload_json TEXT NOT NULL
                );
                CREATE TABLE IF NOT EXISTS actor_run_histories (
                    id TEXT PRIMARY KEY,
                    workgroup_id TEXT NOT NULL,
                    payload_json TEXT NOT NULL
                );
                CREATE TABLE IF NOT EXISTS actor_context_snapshots (
                    id TEXT PRIMARY KEY,
                    workgroup_id TEXT NOT NULL,
                    payload_json TEXT NOT NULL
                );
                -- Workgroup D3：Timeline / Outbox / HITL
                CREATE TABLE IF NOT EXISTS workgroup_timeline (
                    id TEXT PRIMARY KEY,
                    workgroup_id TEXT NOT NULL,
                    payload_json TEXT NOT NULL
                );
                CREATE TABLE IF NOT EXISTS workgroup_outbox (
                    id TEXT PRIMARY KEY,
                    workgroup_id TEXT NOT NULL,
                    payload_json TEXT NOT NULL
                );
                CREATE TABLE IF NOT EXISTS workgroup_hitl (
                    id TEXT PRIMARY KEY,
                    workgroup_id TEXT NOT NULL,
                    payload_json TEXT NOT NULL
                );
                CREATE TABLE IF NOT EXISTS workgroup_human_queue (
                    id TEXT PRIMARY KEY,
                    workgroup_id TEXT NOT NULL,
                    payload_json TEXT NOT NULL
                );
                CREATE TABLE IF NOT EXISTS workgroup_turn_checkpoints (
                    id TEXT PRIMARY KEY,
                    workgroup_id TEXT NOT NULL,
                    payload_json TEXT NOT NULL
                );
                CREATE TABLE IF NOT EXISTS workgroup_subscriptions (
                    id TEXT PRIMARY KEY,
                    workgroup_id TEXT NOT NULL,
                    payload_json TEXT NOT NULL
                );
                -- discovery_group 目录（可空组；节点关联仍写在 registry_agents.payload_json）
                CREATE TABLE IF NOT EXISTS discovery_group_catalog (
                    name TEXT PRIMARY KEY,
                    created_at_unix INTEGER NOT NULL
                );
                -- Blob 元数据随内容寻址文件落在 MANAGE_BLOB_DIR/{sha256}.json sidecar，
                -- 不入 SQLite；故此处不建 blobs 表。
                """
            )
            conn.execute(
                "INSERT INTO schema_meta(key,value) VALUES('schema_version','15') "
                "ON CONFLICT(key) DO UPDATE SET value='15'"
            )
            conn.commit()
