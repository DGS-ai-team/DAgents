from __future__ import annotations

from textual.widgets import RichLog


class TranscriptLog(RichLog):
    """Transcript RichLog：根据 scroll_y 同步 App 的 follow-tail 状态。"""

    def watch_scroll_y(self, old_value: float, new_value: float) -> None:
        super().watch_scroll_y(old_value, new_value)
        if round(old_value) == round(new_value):
            return
        update = getattr(self.app, "_update_transcript_follow_tail", None)
        if callable(update):
            update()
