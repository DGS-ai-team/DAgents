"""workgroup-d05 fixture INDEX 完整性 harness。

不执行全部业务 golden（多数仍由专项 unittest / Go 覆盖），
但保证 INDEX 可解析、文件存在、FixtureMeta 必填字段齐全、test 名一致。
"""

from __future__ import annotations

import json
import sys
import unittest
from pathlib import Path

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

_FIX_ROOT = _ROOT / "docs" / "design" / "fixtures" / "workgroup-d05"
_INDEX = _FIX_ROOT / "INDEX.json"
_REQUIRED = ("fixture_schema", "test", "given", "when", "then")


class WorkgroupD05IndexHarnessTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.index = json.loads(_INDEX.read_text(encoding="utf-8"))
        cls.entries = list(cls.index.get("tests") or [])

    def test_index_schema_version(self) -> None:
        self.assertEqual(self.index.get("schema_version"), "0.5.0")
        self.assertEqual(self.index.get("fixture_schema"), "workgroup-d05-fixture/v1")
        self.assertGreaterEqual(len(self.entries), 1)

    def test_every_index_entry_exists_and_matches_meta(self) -> None:
        missing: list[str] = []
        bad_meta: list[str] = []
        name_mismatch: list[str] = []
        for entry in self.entries:
            rel = str(entry.get("file") or "").strip()
            expected_test = str(entry.get("test") or "").strip()
            path = _FIX_ROOT / rel
            if not rel or not path.is_file():
                missing.append(rel or "<empty>")
                continue
            try:
                doc = json.loads(path.read_text(encoding="utf-8"))
            except json.JSONDecodeError as exc:
                bad_meta.append(f"{rel}: invalid json ({exc})")
                continue
            for key in _REQUIRED:
                if key not in doc:
                    bad_meta.append(f"{rel}: missing {key}")
                    break
            else:
                if doc.get("fixture_schema") != "workgroup-d05-fixture/v1":
                    bad_meta.append(f"{rel}: bad fixture_schema")
                if str(doc.get("test") or "") != expected_test:
                    name_mismatch.append(
                        f"{rel}: INDEX test={expected_test!r} fixture={doc.get('test')!r}"
                    )
                when = doc.get("when")
                if isinstance(when, dict):
                    if "op" not in when:
                        bad_meta.append(f"{rel}: when.op missing")
                elif isinstance(when, list):
                    if not when or any(not isinstance(x, dict) or "op" not in x for x in when):
                        bad_meta.append(f"{rel}: when[] invalid")
                else:
                    bad_meta.append(f"{rel}: when must be object|array")

        self.assertFalse(missing, f"INDEX files missing: {missing}")
        self.assertFalse(bad_meta, f"FixtureMeta violations: {bad_meta}")
        self.assertFalse(name_mismatch, f"INDEX/test name mismatches: {name_mismatch}")

    def test_projection_fixtures_covered_by_unittest(self) -> None:
        """投影类 golden 已有专项测试；INDEX 中 projection/* 应都能被发现。"""
        proj = [
            e["file"]
            for e in self.entries
            if str(e.get("file") or "").startswith("projection/")
        ]
        self.assertGreaterEqual(len(proj), 5)
        for rel in proj:
            self.assertTrue((_FIX_ROOT / rel).is_file(), rel)

    def test_store_golden_fixtures_wired(self) -> None:
        """Manage 纯 Python 可闭合的 fixture 应由 store_golden 加载。"""
        from tests.test_workgroup_d05_store_golden import STORE_GOLDEN_FILES

        index_files = {str(e.get("file") or "") for e in self.entries}
        missing_in_index = [f for f in STORE_GOLDEN_FILES if f not in index_files]
        self.assertFalse(missing_in_index, missing_in_index)
        for rel in STORE_GOLDEN_FILES:
            self.assertTrue((_FIX_ROOT / rel).is_file(), rel)


if __name__ == "__main__":
    unittest.main()
