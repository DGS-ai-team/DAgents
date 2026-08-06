"""Workgroup builtin hooks：日期注入 + 工具结果压缩。"""

from __future__ import annotations

import sys
import unittest
from datetime import datetime
from pathlib import Path
from tempfile import TemporaryDirectory

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.workgroup.builtin_hooks import (  # noqa: E402
    TODAY_DATE_MESSAGE_NAME,
    ensure_today_date_in_messages,
    estimate_tokens,
    format_today_date_message,
    package_tool_result,
)


class TodayDateHookTests(unittest.TestCase):
    def test_inserts_before_trailing_user(self) -> None:
        msgs = [
            {"role": "system", "content": "sys"},
            {"role": "user", "content": "你好"},
        ]
        fixed = datetime(2026, 8, 6, 12, 0, 0)
        out, inserted = ensure_today_date_in_messages(msgs, now=lambda: fixed)
        self.assertIsNotNone(inserted)
        assert inserted is not None
        self.assertEqual(inserted["name"], TODAY_DATE_MESSAGE_NAME)
        self.assertEqual(inserted["content"], format_today_date_message("20260806"))
        self.assertEqual(out[1]["content"], format_today_date_message("20260806"))
        self.assertEqual(out[2]["content"], "你好")

    def test_idempotent_same_day(self) -> None:
        fixed = datetime(2026, 8, 6, 12, 0, 0)
        day = format_today_date_message("20260806")
        msgs = [
            {"role": "user", "name": TODAY_DATE_MESSAGE_NAME, "content": day},
            {"role": "user", "content": "继续"},
        ]
        out, inserted = ensure_today_date_in_messages(msgs, now=lambda: fixed)
        self.assertIsNone(inserted)
        self.assertEqual(out, msgs)

    def test_appends_when_last_not_user(self) -> None:
        fixed = datetime(2026, 8, 6, 12, 0, 0)
        msgs = [
            {"role": "system", "content": "sys"},
            {"role": "assistant", "content": "ok"},
        ]
        out, inserted = ensure_today_date_in_messages(msgs, now=lambda: fixed)
        self.assertIsNotNone(inserted)
        self.assertEqual(out[-1]["content"], format_today_date_message("20260806"))


class ToolResultPackageTests(unittest.TestCase):
    def test_short_result_passthrough(self) -> None:
        packed = package_tool_result(
            "hello world",
            tool_name="read_file",
            run_id="run_1",
            tool_call_id="call_1",
            spill_threshold_tokens=12000,
        )
        self.assertEqual(packed.for_history, "hello world")
        self.assertFalse(packed.spilled)

    def test_long_result_spills_and_summarizes(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            root = Path(tmp) / "workgroup_tool_outputs"
            # 约 > 200 tokens（ASCII 0.3 → 需要 ~700+ chars）；用低阈值触发
            body = ("HEAD-" + ("a" * 400) + "-MID-" + ("b" * 400) + "-TAIL")
            self.assertGreater(estimate_tokens(body), 50)
            packed = package_tool_result(
                body,
                tool_name="read_file",
                run_id="run_abc",
                tool_call_id="call_xyz",
                spill_threshold_tokens=50,
                spill_root=root,
            )
            self.assertTrue(packed.spilled)
            self.assertIsNotNone(packed.spill_path)
            assert packed.spill_path is not None
            spill = Path(packed.spill_path)
            self.assertTrue(spill.is_file())
            self.assertEqual(spill.read_text(encoding="utf-8"), body)
            self.assertIn("已省略约", packed.for_history)
            self.assertIn("workgroup_tool_outputs/", packed.for_history)
            self.assertLess(estimate_tokens(packed.for_history), estimate_tokens(body))
            self.assertTrue(packed.for_history.startswith("HEAD-"))
            self.assertTrue(packed.for_history.endswith("-TAIL") or "TAIL" in packed.for_history[-20:])

    def test_unknown_tool_not_packaged(self) -> None:
        body = "x" * 5000
        packed = package_tool_result(
            body,
            tool_name="custom_unknown_tool",
            spill_threshold_tokens=10,
            spill_root=Path("/tmp/unused"),
        )
        self.assertEqual(packed.for_history, body)
        self.assertFalse(packed.spilled)


if __name__ == "__main__":
    unittest.main()
