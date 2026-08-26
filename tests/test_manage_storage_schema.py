# tests/test_manage_storage_schema.py
import tempfile
import unittest
from pathlib import Path

from manage.storage.sqlite import SCHEMA_VERSION, SQLiteDatabase


class SchemaTest(unittest.TestCase):
    def test_connection_context_closes_connection(self):
        with tempfile.TemporaryDirectory(ignore_cleanup_errors=True) as d:
            db = SQLiteDatabase(Path(d) / "m.db")
            with db.connect() as conn:
                conn.execute("SELECT 1")
            with self.assertRaisesRegex(Exception, "closed"):
                conn.execute("SELECT 1")

    def test_new_tables_exist(self):
        with tempfile.TemporaryDirectory(ignore_cleanup_errors=True) as d:
            db = SQLiteDatabase(Path(d) / "m.db")
            with db.connect() as conn:
                rows = {r[0] for r in conn.execute(
                    "SELECT name FROM sqlite_master WHERE type='table'")}
            self.assertEqual(
                {
                    "llm_configs",
                    "skill_packages",
                    "release_packages",
                    "case_examples",
                    "externaltool_packages",
                    "plugin_packages",
                    "workgroups",
                    "workgroup_acls",
                    "workgroup_members",
                    "member_specs",
                    "workgroup_assigns",
                    "actor_runs",
                    "actor_run_histories",
                    "actor_context_snapshots",
                    "workgroup_timeline",
                    "workgroup_outbox",
                    "workgroup_hitl",
                    "workgroup_human_queue",
                    "workgroup_turn_checkpoints",
                    "workgroup_subscriptions",
                }
                - rows,
                set(),
            )
            # Blob 元数据在 sidecar JSON，不入 SQLite：blobs 表不应存在。
            self.assertNotIn("blobs", rows)
            self.assertEqual(db.schema_version, SCHEMA_VERSION)

    def test_schema_initialization_is_idempotent_and_rejects_newer_databases(self):
        with tempfile.TemporaryDirectory(ignore_cleanup_errors=True) as d:
            path = Path(d) / "m.db"
            db = SQLiteDatabase(path)
            with db.connect() as conn:
                conn.execute(
                    "UPDATE schema_meta SET value=? WHERE key='schema_version'",
                    (str(SCHEMA_VERSION - 1),),
                )
                conn.commit()

            self.assertEqual(SQLiteDatabase(path).schema_version, SCHEMA_VERSION)

            with SQLiteDatabase(path).connect() as conn:
                conn.execute(
                    "UPDATE schema_meta SET value=? WHERE key='schema_version'",
                    (str(SCHEMA_VERSION + 1),),
                )
                conn.commit()
            with self.assertRaisesRegex(RuntimeError, "newer than supported"):
                SQLiteDatabase(path)
