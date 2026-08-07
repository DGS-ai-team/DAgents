"""Manage 侧工作组共享工作区物化。"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.workgroup.workspace import materialize_workgroup_workspace  # noqa: E402


class WorkgroupWorkspaceMaterializeTests(unittest.TestCase):
    def test_creates_data_and_readme(self) -> None:
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            path = materialize_workgroup_workspace(root, "wg_01habcdefghijklmnopqrstuvw")
            self.assertEqual(path, (root / "wg_01habcdefghijklmnopqrstuvw").resolve())
            self.assertTrue((path / "data").is_dir())
            self.assertIn("Reserved", (path / "README.md").read_text(encoding="utf-8"))
            # idempotent
            again = materialize_workgroup_workspace(root, "wg_01habcdefghijklmnopqrstuvw")
            self.assertEqual(again, path)


if __name__ == "__main__":
    unittest.main()
