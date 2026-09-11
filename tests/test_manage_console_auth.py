from __future__ import annotations
import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import patch
from fastapi.testclient import TestClient

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))
from manage.config import ManageSettings
from manage.manage_app import create_app
from manage.platform.sessions import SESSION_COOKIE


class ManageConsoleAuthTests(unittest.TestCase):
    def test_admin_password_login_and_me(self):
        with (
            TemporaryDirectory() as tmp,
            patch.dict("os.environ", {"MANAGE_ADMIN_USERNAME": "admin", "MANAGE_ADMIN_PASSWORD": "secret"}),
        ):
            with TestClient(create_app(ManageSettings.for_test(db_path=Path(tmp) / "manage.db"))) as client:
                self.assertFalse(client.get("/v1/auth/me").json()["authenticated"])
                self.assertEqual(
                    client.post("/v1/auth/login", json={"username": "admin", "password": "wrong"}).status_code, 401
                )
                ok = client.post("/v1/auth/login", json={"username": "admin", "password": "secret"})
                self.assertEqual(ok.status_code, 200)
                self.assertEqual(ok.json()["role"], "admin")
                self.assertIn(SESSION_COOKIE, client.cookies)
                self.assertTrue(client.get("/v1/auth/me").json()["authenticated"])

    def test_node_login_requires_bound_token(self):
        env = {
            "MANAGE_ADMIN_USERNAME": "admin",
            "MANAGE_ADMIN_PASSWORD": "secret",
            "MANAGE_TOKENS": '[{"id":"node-token","role":"node","agent_id":"node-x","token":"node-secret"}]',
        }
        with TemporaryDirectory() as tmp, patch.dict("os.environ", env):
            with TestClient(create_app(ManageSettings.for_test(db_path=Path(tmp) / "manage.db"))) as client:
                self.assertEqual(client.post("/v1/auth/login/node", json={"node_id": "node-x"}).status_code, 401)
                client.post("/v1/auth/login", json={"username": "admin", "password": "secret"})
                client.post(
                    "/v1/registry/agents",
                    json={"agent_id": "node-x", "base_url": "http://node-x.local"},
                    headers={"x-dagents-agent-id": "node-x"},
                )
                client.patch("/v1/registry/agents/node-x/groups", json={"discovery_group": ["ops"]})
                client.post("/v1/auth/logout")
                self.assertEqual(
                    client.post("/v1/auth/login/node", json={"node_id": "node-x", "token": "wrong"}).status_code, 401
                )
                ok = client.post("/v1/auth/login/node", json={"node_id": "node-x", "token": "node-secret"})
                self.assertEqual(ok.status_code, 200)
                self.assertEqual(ok.json()["agent_id"], "node-x")

    def test_node_token_cannot_reassign_existing_agent(self):
        env = {
            "MANAGE_TOKENS": '[{"role":"node","agent_id":"node-a","token":"secret-a"},{"role":"node","agent_id":"node-b","token":"secret-b"}]'
        }
        with TemporaryDirectory() as tmp, patch.dict("os.environ", env):
            with TestClient(create_app(ManageSettings.for_test(db_path=Path(tmp) / "manage.db"))) as client:
                headers = {"x-dagents-a2a-token": "secret-a", "x-dagents-agent-id": "node-a"}
                first = client.post(
                    "/v1/registry/agents",
                    json={"agent_id": "agent-1", "node_id": "node-a", "base_url": "http://a.local"},
                    headers=headers,
                )
                self.assertEqual(first.status_code, 200)
                takeover = client.post(
                    "/v1/registry/agents",
                    json={"agent_id": "agent-1", "node_id": "node-b", "base_url": "http://b.local"},
                    headers={"x-dagents-a2a-token": "secret-b", "x-dagents-agent-id": "node-b"},
                )
                self.assertEqual(takeover.status_code, 403)


if __name__ == "__main__":
    unittest.main()
