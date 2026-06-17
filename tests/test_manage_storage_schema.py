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
            self.assertEqual({"llm_configs", "skill_packages", "blobs"} - rows, set())
            with db.connect() as conn:
                ver = conn.execute(
                    "SELECT value FROM schema_meta WHERE key='schema_version'").fetchone()[0]
            self.assertEqual(ver, "3")
