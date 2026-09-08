from __future__ import annotations

import os
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import patch

from fastapi.testclient import TestClient

from manage.config import ManageSettings
from manage.manage_app import create_app


class ManageAuthP0Tests(unittest.TestCase):
    def _client(self, tmp: str, env: dict[str, str] | None = None):
        values = {"MANAGE_TOKENS": "", "MANAGE_SHARED_TOKEN": "", "MANAGE_ADMIN_PASSWORD": ""}
        values.update(env or {})
        return patch.dict(os.environ, values, clear=False), TestClient(
            create_app(ManageSettings.for_test(db_path=Path(tmp) / "manage.db"))
        )

    def test_anonymous_api_rejected_when_all_credentials_empty(self):
        with (
            TemporaryDirectory() as tmp,
            patch.dict(
                os.environ, {"MANAGE_TOKENS": "", "MANAGE_SHARED_TOKEN": "", "MANAGE_ADMIN_PASSWORD": ""}, clear=False
            ),
        ):
            with TestClient(create_app(ManageSettings.for_test(db_path=Path(tmp) / "manage.db"))) as client:
                self.assertEqual(client.get("/v1/registry/agents").status_code, 401)

    def test_default_password_and_missing_password_rejected(self):
        with (
            TemporaryDirectory() as tmp,
            patch.dict(
                os.environ,
                {"MANAGE_TOKENS": "", "MANAGE_SHARED_TOKEN": "", "MANAGE_ADMIN_PASSWORD": ""},
                clear=False,
            ),
        ):
            with TestClient(create_app(ManageSettings.for_test(db_path=Path(tmp) / "manage.db"))) as client:
                self.assertEqual(
                    client.post("/v1/auth/login", json={"username": "admin", "password": "admin"}).status_code, 503
                )

    def test_old_cookie_does_not_authenticate(self):
        with (
            TemporaryDirectory() as tmp,
            patch.dict(
                os.environ, {"MANAGE_TOKENS": "", "MANAGE_SHARED_TOKEN": "", "MANAGE_ADMIN_PASSWORD": "old-secret"}, clear=False
            ),
        ):
            with TestClient(create_app(ManageSettings.for_test(db_path=Path(tmp) / "manage.db"))) as client:
                self.assertEqual(
                    client.post("/v1/auth/login", json={"username": "admin", "password": "old-secret"}).status_code, 200
                )
                sid = client.cookies.get("dagents_manage_session_v2")
                client.cookies.clear()
                client.cookies.set("dagents_manage_session", sid)
                self.assertEqual(client.get("/v1/registry/agents").status_code, 401)

    def test_member_token_cannot_claim_node(self):
        env = {
            "MANAGE_TOKENS": '[{"role":"member","token":"member","discovery_groups":["ops"]}]',
            "MANAGE_SHARED_TOKEN": "",
            "MANAGE_ADMIN_PASSWORD": "",
        }
        with TemporaryDirectory() as tmp, patch.dict(os.environ, env, clear=False):
            with TestClient(
                create_app(ManageSettings.for_test(db_path=Path(tmp) / "manage.db")),
                headers={"x-dagents-a2a-token": "member"},
            ) as client:
                self.assertEqual(
                    client.post(
                        "/v1/registry/agents", json={"agent_id": "a", "node_id": "node-a", "base_url": "http://a.local"}
                    ).status_code,
                    403,
                )

    def test_node_a_cannot_login_or_ws_as_node_b_but_can_heartbeat_own_agent(self):
        env = {
            "MANAGE_TOKENS": '[{"role":"node","agent_id":"node-a","token":"token-a"},{"role":"node","agent_id":"node-b","token":"token-b"}]',
            "MANAGE_SHARED_TOKEN": "",
            "MANAGE_ADMIN_PASSWORD": "",
        }
        with TemporaryDirectory() as tmp, patch.dict(os.environ, env, clear=False):
            with TestClient(create_app(ManageSettings.for_test(db_path=Path(tmp) / "manage.db"))) as client:
                ha = {"x-dagents-a2a-token": "token-a", "x-dagents-agent-id": "node-a"}
                self.assertEqual(
                    client.post(
                        "/v1/registry/agents",
                        headers=ha,
                        json={"agent_id": "agent-a", "node_id": "node-a", "base_url": "http://a.local"},
                    ).status_code,
                    200,
                )
                self.assertEqual(
                    client.post("/v1/registry/agents/agent-a/heartbeat", headers=ha, json={}).status_code, 200
                )
                self.assertEqual(
                    client.post("/v1/auth/login/node", json={"node_id": "node-b", "token": "token-a"}).status_code, 401
                )
                hb = {"x-dagents-a2a-token": "token-b", "x-dagents-agent-id": "node-b"}
                self.assertEqual(
                    client.post(
                        "/v1/registry/agents",
                        headers=hb,
                        json={"agent_id": "node-b", "node_id": "node-b", "base_url": "http://b.local"},
                    ).status_code,
                    200,
                )
                with client.websocket_connect(
                    "/v1/workgroups/ws", headers={"x-dagents-a2a-token": "token-a", "x-dagents-agent-id": "node-b"}
                ) as ws:
                    self.assertEqual(ws.receive_json()["payload"]["code"], "not_authorized")

    def test_empty_node_groups_do_not_become_wildcard(self):
        env = {
            "MANAGE_TOKENS": '[{"role":"node","agent_id":"node-a","token":"token-a"}]',
            "MANAGE_SHARED_TOKEN": "",
            "MANAGE_ADMIN_PASSWORD": "",
        }
        with TemporaryDirectory() as tmp, patch.dict(os.environ, env, clear=False):
            with TestClient(create_app(ManageSettings.for_test(db_path=Path(tmp) / "manage.db"))) as client:
                ha = {"x-dagents-a2a-token": "token-a", "x-dagents-agent-id": "node-a"}
                self.assertEqual(
                    client.post(
                        "/v1/registry/agents",
                        headers=ha,
                        json={"agent_id": "node-a", "node_id": "node-a", "base_url": "http://a.local"},
                    ).status_code,
                    200,
                )
                self.assertEqual(
                    client.post(
                        "/v1/auth/login/node", headers=ha, json={"node_id": "node-a", "token": "token-a"}
                    ).status_code,
                    200,
                )
                self.assertEqual(client.get("/v1/registry/agents", params={"discovery_group": "ops"}).status_code, 403)
