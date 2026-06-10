from __future__ import annotations

import unittest
from unittest.mock import MagicMock, PropertyMock, patch

from app.cli.tui.transcript_log import TranscriptLog


class TranscriptLogTests(unittest.TestCase):
    def test_watch_scroll_y_notifies_app_on_position_change(self) -> None:
        log = TranscriptLog()
        app = MagicMock()
        app._update_transcript_follow_tail = MagicMock()

        with (
            patch.object(TranscriptLog, "app", new_callable=PropertyMock) as mock_app,
            patch.object(TranscriptLog.__bases__[0], "watch_scroll_y"),
        ):
            mock_app.return_value = app
            log.watch_scroll_y(0.0, 0.0)
        app._update_transcript_follow_tail.assert_not_called()

        with (
            patch.object(TranscriptLog, "app", new_callable=PropertyMock) as mock_app,
            patch.object(TranscriptLog.__bases__[0], "watch_scroll_y"),
        ):
            mock_app.return_value = app
            log.watch_scroll_y(0.0, 3.0)
        app._update_transcript_follow_tail.assert_called_once()

    def test_watch_scroll_y_skips_when_app_has_no_updater(self) -> None:
        log = TranscriptLog()
        app = MagicMock(spec=[])

        with (
            patch.object(TranscriptLog, "app", new_callable=PropertyMock) as mock_app,
            patch.object(TranscriptLog.__bases__[0], "watch_scroll_y"),
        ):
            mock_app.return_value = app
            log.watch_scroll_y(1.0, 4.0)


if __name__ == "__main__":
    unittest.main()
