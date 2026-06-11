from __future__ import annotations

import json
import os
import sys
import time
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import patch

from fastapi.testclient import TestClient

_REGISTER_CENTER_ROOT = Path(__file__).resolve().parents[1] / "register_center"
if str(_REGISTER_CENTER_ROOT) not in sys.path:
    sys.path.insert(0, str(_REGISTER_CENTER_ROOT))

import rc_app  # noqa: E402
from rc_status import derive_status  # noqa: E402


class RegisterCenterPhase1ModelTests(unittest.TestCase):
    def test_derive_status_with_grace(self) -> None:
        now = 10_000
        self.assertEqual(derive_status(now_unix=now, expires_at_unix=now + 1, grace_seconds=60), "online")
        self.assertEqual(derive_status(now_unix=now, expires_at_unix=now, grace_seconds=60), "offline")
        self.assertEqual(derive_status(now_unix=now + 60, expires_at_unix=now, grace_seconds=60), "expired")
        self.assertEqual(derive_status(now_unix=now, expires_at_unix=now, grace_seconds=0), "expired")

    def test_upsert_returns_extended_fields(self) -> None:
        app = rc_app.create_app()
        with TestClient(app) as client:
            resp = client.post(
                "/v1/agents",
                json={
                    "agent_id": "ops-01",
                    "base_url": "http://ops.local",
                    "discovery_group": ["ops"],
                    "name": "运维助手",
                    "team": "platform",
                    "tools": ["bash_run"],
                    "version": "0.2.17",
                },
            )
        self.assertEqual(resp.status_code, 200)
        body = resp.json()
        self.assertEqual(body["name"], "运维助手")
        self.assertEqual(body["team"], "platform")
        self.assertEqual(body["tools"], ["bash_run"])
        self.assertEqual(body["status"], "online")
        self.assertIn("updated_at_unix", body)
        self.assertIn("last_seen_unix", body)

    def test_registered_at_preserved_on_reupsert(self) -> None:
        app = rc_app.create_app()
        with TestClient(app) as client:
            first = client.post(
                "/v1/agents",
                json={"agent_id": "a1", "base_url": "http://a.local", "discovery_group": ["g1"]},
            )
            time.sleep(1)
            second = client.post(
                "/v1/agents",
                json={"agent_id": "a1", "base_url": "http://a.local", "discovery_group": ["g1"], "team": "t1"},
            )
        self.assertEqual(first.status_code, 200)
        self.assertEqual(second.status_code, 200)
        self.assertEqual(first.json()["registered_at_unix"], second.json()["registered_at_unix"])
        self.assertLess(first.json()["updated_at_unix"], second.json()["updated_at_unix"])


class RegisterCenterPhase1AuthTests(unittest.TestCase):
    def test_member_requires_discovery_group_on_list(self) -> None:
        tokens = json.dumps(
            [{"id": "ops", "token": "member-secret", "role": "member", "discovery_groups": ["ops"]}]
        )
        env = {"REGISTER_CENTER_TOKENS": tokens, "AGENT_PEER_SHARED_TOKEN": ""}
        with patch.dict(os.environ, env):
            app = rc_app.create_app()
            with TestClient(app) as client:
                headers = {"x-dagents-a2a-token": "member-secret"}
                missing = client.get("/v1/agents", headers=headers)
                global_list = client.get("/v1/agents", headers=headers, params={"discovery_group": "ops"})
                forbidden = client.get("/v1/agents", headers=headers, params={"discovery_group": "other"})
                admin_audit = client.get("/v1/admin/audit", headers=headers)

        self.assertEqual(missing.status_code, 422)
        self.assertEqual(global_list.status_code, 200)
        self.assertEqual(forbidden.status_code, 403)
        self.assertEqual(admin_audit.status_code, 403)

    def test_admin_can_list_without_discovery_group(self) -> None:
        tokens = json.dumps([{"id": "admin", "token": "admin-secret", "role": "admin"}])
        env = {"REGISTER_CENTER_TOKENS": tokens, "AGENT_PEER_SHARED_TOKEN": ""}
        with patch.dict(os.environ, env):
            app = rc_app.create_app()
            with TestClient(app) as client:
                headers = {"x-dagents-a2a-token": "admin-secret"}
                client.post(
                    "/v1/agents",
                    headers=headers,
                    json={"agent_id": "x1", "base_url": "http://x.local", "discovery_group": ["g1"]},
                )
                listed = client.get("/v1/agents", headers=headers, params={"status": "all"})
                audit = client.get("/v1/admin/audit", headers=headers)

        self.assertEqual(listed.status_code, 200)
        self.assertEqual(listed.json()["total"], 1)
        self.assertEqual(audit.status_code, 200)
        self.assertTrue(audit.json()["events"])


class RegisterCenterPhase1RelayOfflineTests(unittest.TestCase):
    def test_relay_rejects_offline_agent(self) -> None:
        with TemporaryDirectory() as tmp:
            store_path = Path(tmp) / "registry.json"
            now = int(time.time())
            store_path.write_text(
                json.dumps(
                    {
                        "schema_version": 2,
                        "agents": [
                            {
                                "agent_id": "offline-a",
                                "base_url": "http://offline.local",
                                "discovery_group": ["g1"],
                                "capabilities_hint": [],
                                "name": "offline-a",
                                "description": "",
                                "owner": "",
                                "team": "",
                                "capabilities": [],
                                "tools": [],
                                "skills": [],
                                "auth_method": "shared_token",
                                "risk_level": "medium",
                                "allowed_scopes": [],
                                "version": "",
                                "metadata": {},
                                "last_error_summary": None,
                                "recent_task_summary": None,
                                "registered_at_unix": now - 3600,
                                "updated_at_unix": now - 3600,
                                "last_seen_unix": now - 3600,
                                "expires_at_unix": now - 10,
                            }
                        ],
                    }
                ),
                encoding="utf-8",
            )
            env = {
                "REGISTER_CENTER_STORE_PATH": str(store_path),
                "REGISTER_CENTER_OFFLINE_GRACE_SECONDS": "3600",
                "AGENT_PEER_SHARED_TOKEN": "",
            }
            with patch.dict(os.environ, env):
                app = rc_app.create_app()
                with TestClient(app) as client:
                    listed = client.get("/v1/agents", params={"discovery_group": "g1", "status": "offline"})
                    relay = client.post(
                        "/v1/relay",
                        json={
                            "target_agent_id": "offline-a",
                            "caller_groups": ["g1"],
                            "session_id": "s1",
                            "client_id": "c1",
                            "request_type": "message",
                            "content": "hello",
                        },
                    )

        self.assertEqual(listed.status_code, 200)
        self.assertEqual(listed.json()["total"], 1)
        self.assertEqual(listed.json()["agents"][0]["status"], "offline")
        self.assertEqual(relay.status_code, 409)


if __name__ == "__main__":
    unittest.main()
