# tests/test_manage_storage_schema.py
import tempfile, unittest
from pathlib import Path
from manage.storage.sqlite import SQLiteDatabase

class SchemaTest(unittest.TestCase):
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
                    "execution_grants",
                    "workgroup_assigns",
                    "actor_runs",
                    "workgroup_timeline",
                    "workgroup_outbox",
                    "workgroup_hitl",
                }
                - rows,
                set(),
            )
            # Blob 元数据在 sidecar JSON，不入 SQLite：blobs 表不应存在。
            self.assertNotIn("blobs", rows)
            with db.connect() as conn:
                ver = conn.execute(
                    "SELECT value FROM schema_meta WHERE key='schema_version'").fetchone()[0]
            self.assertEqual(ver, "9")
