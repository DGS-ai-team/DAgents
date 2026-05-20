from __future__ import annotations

import os
import sys
import unittest
from pathlib import Path
from unittest.mock import patch

from fastapi.testclient import TestClient

_REGISTER_CENTER_ROOT = Path(__file__).resolve().parents[1] / "register_center"
if str(_REGISTER_CENTER_ROOT) not in sys.path:
    sys.path.insert(0, str(_REGISTER_CENTER_ROOT))

import rc_app  # noqa: E402


class RegisterCenterSharedTokenTests(unittest.TestCase):
    def test_agent_routes_require_shared_token_when_configured(self) -> None:
        with patch.dict(os.environ, {"AGENT_PEER_SHARED_TOKEN": "secret"}):
            app = rc_app.create_app()
            with TestClient(app) as client:
                payload = {
                    "agent_id": "agent-a",
                    "base_url": "http://agent.local",
                    "discovery_group": ["g1"],
                }
                rejected = client.post("/v1/agents", json=payload)
                accepted = client.post("/v1/agents", headers={"x-dagents-a2a-token": "secret"}, json=payload)
                rejected_list = client.get("/v1/agents", params={"discovery_group": "g1"})
                accepted_list = client.get(
                    "/v1/agents",
                    headers={"x-dagents-a2a-token": "secret"},
                    params={"discovery_group": "g1"},
                )

        self.assertEqual(rejected.status_code, 401)
        self.assertEqual(accepted.status_code, 200)
        self.assertEqual(rejected_list.status_code, 401)
        self.assertEqual(accepted_list.status_code, 200)
        self.assertEqual(accepted_list.json()["agents"][0]["agent_id"], "agent-a")


if __name__ == "__main__":
    unittest.main()
