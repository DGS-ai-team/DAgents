from __future__ import annotations

import sys
import unittest
from pathlib import Path

from fastapi.testclient import TestClient

_REGISTER_CENTER_ROOT = Path(__file__).resolve().parents[1] / "register_center"
if str(_REGISTER_CENTER_ROOT) not in sys.path:
    sys.path.insert(0, str(_REGISTER_CENTER_ROOT))

import rc_app  # noqa: E402


class RegisterCenterDirectoryUITests(unittest.TestCase):
    def test_ui_index_served(self) -> None:
        app = rc_app.create_app()
        with TestClient(app) as client:
            redirect = client.get("/ui", follow_redirects=False)
            page = client.get("/ui/")
            assets = client.get("/ui/app.js")

        self.assertEqual(redirect.status_code, 307)
        self.assertEqual(redirect.headers["location"], "/ui/")
        self.assertEqual(page.status_code, 200)
        self.assertIn("Agent Directory", page.text)
        self.assertEqual(assets.status_code, 200)
        self.assertIn("loadAgents", assets.text)


if __name__ == "__main__":
    unittest.main()
