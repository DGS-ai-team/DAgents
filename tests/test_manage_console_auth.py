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

from manage.config import ManageSettings  # noqa: E402
from manage.manage_app import create_app  # noqa: E402
from manage.platform.sessions import SESSION_COOKIE  # noqa: E402


class ManageConsoleAuthTests(unittest.TestCase):
    def test_admin_password_login_and_me(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            with TestClient(app) as client:
                me0 = client.get("/v1/auth/me")
                self.assertEqual(me0.status_code, 200)
                self.assertFalse(me0.json()["authenticated"])

                bad = client.post("/v1/auth/login", json={"username": "admin", "password": "wrong"})
                self.assertEqual(bad.status_code, 401)

                with patch.dict("os.environ", {"MANAGE_ADMIN_USERNAME": "admin", "MANAGE_ADMIN_PASSWORD": "secret"}):
                    ok = client.post("/v1/auth/login", json={"username": "admin", "password": "secret"})
                self.assertEqual(ok.status_code, 200)
                body = ok.json()
                self.assertTrue(body["authenticated"])
                self.assertEqual(body["kind"], "admin")
                self.assertEqual(body["role"], "admin")
                self.assertIn(SESSION_COOKIE, client.cookies)

                me = client.get("/v1/auth/me")
                self.assertTrue(me.json()["authenticated"])
                self.assertEqual(me.json()["kind"], "admin")

                out = client.post("/v1/auth/logout")
                self.assertEqual(out.status_code, 200)
                self.assertFalse(out.json()["authenticated"])

    def test_node_id_login_requires_registry(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            with TestClient(app) as client:
                missing = client.post("/v1/auth/login/node", json={"node_id": "node-x"})
                self.assertEqual(missing.status_code, 401)

                client.post(
                    "/v1/registry/agents",
                    json={"agent_id": "node-x", "base_url": "http://node-x.local"},
                    headers={"x-dagents-agent-id": "node-x"},
                )
                client.patch(
                    "/v1/registry/agents/node-x/groups",
                    json={"discovery_group": ["ops"]},
                )
                ok = client.post("/v1/auth/login/node", json={"node_id": "node-x"})
                self.assertEqual(ok.status_code, 200)
                body = ok.json()
                self.assertTrue(body["authenticated"])
                self.assertEqual(body["kind"], "node")
                self.assertEqual(body["role"], "member")
                self.assertEqual(body["agent_id"], "node-x")
                self.assertEqual(body["discovery_groups"], ["ops"])

                listed = client.get("/v1/registry/agents", params={"discovery_group": "ops", "status": "all"})
                self.assertEqual(listed.status_code, 200)
                self.assertEqual(listed.json()["total"], 1)


if __name__ == "__main__":
    unittest.main()
