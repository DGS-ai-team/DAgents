"""FastAPI：`POST /v1/sessions/{session_id}/cancel` 与无在途 turn 行为。"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

from fastapi.testclient import TestClient

_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_ROOT))

from app.harness.api.app import create_app


class ApiCancelTurnTestCase(unittest.TestCase):
    def test_cancel_turn_no_active_returns_false(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            sid = client.post("/v1/sessions", json={}).json()["session_id"]
            resp = client.post(f"/v1/sessions/{sid}/cancel")
            self.assertEqual(resp.status_code, 200)
            body = resp.json()
            self.assertEqual(body["session_id"], sid)
            self.assertFalse(body["cancelled"])


if __name__ == "__main__":
    unittest.main()
