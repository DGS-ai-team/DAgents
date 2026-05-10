from __future__ import annotations

import sys
import unittest
from pathlib import Path

_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_ROOT))

import app.config.host_snapshot as hs  # noqa: E402


class HostSnapshotTestCase(unittest.TestCase):
    def tearDown(self) -> None:
        hs._snapshot = None

    def test_capture_then_get_returns_same_instance(self) -> None:
        """显式 capture 后 get_host_snapshot 返回同一对象引用。"""
        hs._snapshot = None
        captured = hs.capture_host_snapshot_at_startup()
        self.assertIs(hs.get_host_snapshot(), captured)

    def test_get_without_prior_capture_builds_lazy_singleton(self) -> None:
        """未先 capture 时首次 get 惰性构建，后续 get 复用缓存。"""
        hs._snapshot = None
        s1 = hs.get_host_snapshot()
        s2 = hs.get_host_snapshot()
        self.assertIs(s1, s2)
        self.assertIn(s1.os_kind, {"windows", "linux", "darwin", "other"})
        self.assertIsInstance(s1.login_name, str)


if __name__ == "__main__":
    unittest.main()
