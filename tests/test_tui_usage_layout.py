from __future__ import annotations

import io
import unittest

from rich.console import Console
from rich.table import Table
from rich.text import Text

from unittest.mock import MagicMock

from app.cli.tui.app import DAgentsTuiApp


class AssistantUsageLayoutTests(unittest.TestCase):
    def test_usage_row_does_not_fold_think_count(self) -> None:
        app = DAgentsTuiApp.__new__(DAgentsTuiApp)
        app._transcript_content_width = lambda: 80  # type: ignore[method-assign]

        suffix = " · ↑7,291 ↓55 · think 30"
        usage = app._right_aligned_usage_row(suffix)
        content = "现在是 **2026 年 6 月 10 日 (周三) 上午 9:43**。"
        body = app._assistant_body_with_usage(content, suffix)

        buf = io.StringIO()
        console = Console(file=buf, width=80, force_terminal=True, legacy_windows=False)
        grid = Table.grid(expand=True, padding=(0, 1))
        grid.add_column(width=1, no_wrap=True)
        grid.add_column(ratio=1, overflow="fold")
        grid.add_row(Text("●", style="green"), body)
        console.print(grid)
        rendered = buf.getvalue()

        self.assertIn("think 30", rendered)
        self.assertNotIn("\n   30\n", rendered)
        self.assertNotIn("\n30\n", rendered.replace("think 30", ""))


class ApplyRoundUsageTests(unittest.TestCase):
    def test_apply_round_usage_pending_while_streaming(self) -> None:
        app = DAgentsTuiApp.__new__(DAgentsTuiApp)
        app._assistant_buffer = "partial"
        app._pending_round_usage_suffix = None
        app._last_assistant_done_block = None
        app._apply_round_usage(" · ↑1 ↓2")
        self.assertEqual(app._pending_round_usage_suffix, " · ↑1 ↓2")

    def test_apply_round_usage_rewrites_last_done_block(self) -> None:
        app = DAgentsTuiApp.__new__(DAgentsTuiApp)
        app._assistant_buffer = ""
        app._pending_round_usage_suffix = None
        app._last_assistant_done_block = {
            "start": 5,
            "end": 8,
            "text": "hello",
            "usage_suffix": None,
        }
        replaced: list[tuple[int, int, object]] = []

        def fake_replace(start: int, end: int, content: object) -> tuple[int, int]:
            replaced.append((start, end, content))
            return start, end + 1

        app._replace_log_block = fake_replace  # type: ignore[method-assign]
        app._assistant_block = lambda text, *, complete, usage_suffix=None: (  # type: ignore[method-assign]
            f"block:{text}:{usage_suffix}"
        )
        app._apply_round_usage(" · ↑1 ↓2")
        self.assertEqual(len(replaced), 1)
        self.assertEqual(replaced[0][0], 5)
        self.assertEqual(replaced[0][1], 8)
        self.assertEqual(app._last_assistant_done_block["usage_suffix"], " · ↑1 ↓2")
        self.assertEqual(app._last_assistant_done_block["end"], 9)

    def test_apply_round_usage_skips_duplicate_suffix(self) -> None:
        app = DAgentsTuiApp.__new__(DAgentsTuiApp)
        app._assistant_buffer = ""
        app._last_assistant_done_block = {
            "start": 1,
            "end": 2,
            "text": "hi",
            "usage_suffix": " · ↑1 ↓2",
        }
        app._replace_log_block = MagicMock()  # type: ignore[method-assign]
        app._apply_round_usage(" · ↑1 ↓2")
        app._replace_log_block.assert_not_called()


if __name__ == "__main__":
    unittest.main()
