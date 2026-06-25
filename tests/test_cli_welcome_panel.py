from __future__ import annotations

import unittest

from app.cli.tui.welcome_panel import build_welcome_panel


class WelcomePanelTests(unittest.TestCase):
    def test_title_uses_node_version(self) -> None:
        panel = build_welcome_panel(
            api_base="http://127.0.0.1:18765",
            session_id="s1",
            version="0.5.1",
        )
        self.assertIn("0.5.1", str(panel.title))

    def test_body_shows_version(self) -> None:
        from io import StringIO

        from rich.console import Console

        panel = build_welcome_panel(
            api_base="http://127.0.0.1:18765",
            session_id="s1",
            version="0.5.1",
        )
        buf = StringIO()
        Console(file=buf, width=120, force_terminal=True).print(panel)
        text = buf.getvalue()
        self.assertIn("version · 0.5.1", text)


if __name__ == "__main__":
    unittest.main()
