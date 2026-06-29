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

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.config import ManageSettings  # noqa: E402
from manage.manage_app import create_app  # noqa: E402
from manage.registry.status import derive_status  # noqa: E402


class ManageM0Tests(unittest.TestCase):
    def test_health_and_blob_status(self) -> None:
        app = create_app(ManageSettings.from_env())
        with TestClient(app) as client:
            health = client.get("/health")
            metrics = client.get("/metrics")
        self.assertEqual(health.status_code, 200)
        body = health.json()
        self.assertEqual(body["status"], "ok")
        self.assertIn("blob", body)
        self.assertEqual(metrics.status_code, 200)
        self.assertIn("dagents_manage_registry_operations_total", metrics.text)

    def test_console_served(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            root = client.get("/", follow_redirects=False)
            page = client.get("/console/")
        self.assertEqual(root.status_code, 307)
        self.assertEqual(page.status_code, 200)
        self.assertIn("DAgents Manage", page.text)
        self.assertIn('id="app"', page.text)


class ManageRegistryTests(unittest.TestCase):
    def test_register_discover_and_sqlite_persist(self) -> None:
        with TemporaryDirectory() as tmp:
            db_path = Path(tmp) / "manage.db"
            settings = ManageSettings.for_test(db_path=db_path)
            app = create_app(settings)
            payload = {
                "agent_id": "ops-01",
                "base_url": "http://ops.local",
                "name": "运维助手",
                "expose_to_peers": True,
            }
            with TestClient(app) as client:
                reg = client.post("/v1/registry/agents", json=payload)
                groups = client.patch(
                    "/v1/registry/agents/ops-01/groups",
                    json={"discovery_group": ["ops"]},
                )
                discover = client.get("/v1/registry/agents/discover", params={"discovery_group": "ops"})
                listed = client.get("/v1/registry/agents", params={"discovery_group": "ops", "status": "all"})

            app2 = create_app(settings)
            with TestClient(app2) as client:
                reloaded = client.get("/v1/registry/agents", params={"discovery_group": "ops", "status": "all"})

        self.assertEqual(reg.status_code, 200)
        self.assertEqual(reg.json()["agent"]["discovery_group"], [])
        self.assertEqual(groups.status_code, 200)
        self.assertEqual(groups.json()["discovery_group"], ["ops"])
        self.assertEqual(discover.status_code, 200)
        self.assertEqual(len(discover.json()["agents"]), 1)
        self.assertNotIn("base_url", discover.json()["agents"][0])
        self.assertEqual(listed.json()["total"], 1)
        self.assertEqual(reloaded.json()["total"], 1)

    def test_heartbeat_and_deregister(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            client.post(
                "/v1/registry/agents",
                json={"agent_id": "a1", "base_url": "http://a.local"},
            )
            client.patch("/v1/registry/agents/a1/groups", json={"discovery_group": ["g1"]})
            hb = client.post(
                "/v1/registry/agents/a1/heartbeat",
                json={"ttl_seconds": 120, "version": "0.4.0"},
            )
            dereg = client.post("/v1/registry/agents/a1/deregister", json={"reason": "shutdown"})
            after = client.get("/v1/registry/agents", params={"discovery_group": "g1", "status": "all"})
        self.assertEqual(hb.status_code, 200)
        self.assertEqual(hb.json()["version"], "0.4.0")
        self.assertEqual(dereg.status_code, 200)
        self.assertEqual(after.json()["total"], 0)

    def test_list_all_nodes_without_auth(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            client.post(
                "/v1/registry/agents",
                json={"agent_id": "n1", "base_url": "http://n1.local"},
                headers={"x-dagents-agent-id": "n1"},
            )
            listed = client.get("/v1/registry/agents")
        self.assertEqual(listed.status_code, 200)
        body = listed.json()
        self.assertEqual(body["total"], 1)
        self.assertEqual(body["agents"][0]["agent_id"], "n1")

    def test_register_preserves_manage_assigned_groups(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            client.post("/v1/registry/agents", json={"agent_id": "n2", "base_url": "http://n2.local"})
            client.patch("/v1/registry/agents/n2/groups", json={"discovery_group": ["ops", "lab"]})
            again = client.post("/v1/registry/agents", json={"agent_id": "n2", "base_url": "http://n2.local"})
        self.assertEqual(again.status_code, 200)
        self.assertEqual(again.json()["agent"]["discovery_group"], ["ops", "lab"])

    def test_member_auth_requires_group(self) -> None:
        tokens = json.dumps([{"id": "ops", "token": "member-secret", "role": "member", "discovery_groups": ["ops"]}])
        with patch.dict(os.environ, {"MANAGE_TOKENS": tokens, "MANAGE_SHARED_TOKEN": ""}):
            app = create_app()
            with TestClient(app) as client:
                headers = {"x-dagents-a2a-token": "member-secret"}
                missing = client.get("/v1/registry/agents", headers=headers)
                ok = client.get("/v1/registry/agents", headers=headers, params={"discovery_group": "ops"})
        self.assertEqual(missing.status_code, 422)
        self.assertEqual(ok.status_code, 200)

    def test_discover_without_group_matches_caller_groups(self) -> None:
        app = create_app()
        with TestClient(app) as client:
            client.post(
                "/v1/registry/agents",
                json={"agent_id": "caller", "base_url": "http://caller.local", "expose_to_peers": True},
                headers={"x-dagents-agent-id": "caller"},
            )
            client.post(
                "/v1/registry/agents",
                json={"agent_id": "peer-ops", "base_url": "http://peer-ops.local", "expose_to_peers": True},
                headers={"x-dagents-agent-id": "peer-ops"},
            )
            client.post(
                "/v1/registry/agents",
                json={"agent_id": "peer-lab", "base_url": "http://peer-lab.local", "expose_to_peers": True},
                headers={"x-dagents-agent-id": "peer-lab"},
            )
            client.patch(
                "/v1/registry/agents/caller/groups",
                json={"discovery_group": ["ops"]},
            )
            client.patch(
                "/v1/registry/agents/peer-ops/groups",
                json={"discovery_group": ["ops"]},
            )
            client.patch(
                "/v1/registry/agents/peer-lab/groups",
                json={"discovery_group": ["lab"]},
            )
            discover = client.get(
                "/v1/registry/agents/discover",
                headers={"x-dagents-agent-id": "caller"},
            )
            empty = client.get(
                "/v1/registry/agents/discover",
                headers={"x-dagents-agent-id": "unknown-agent"},
            )

        self.assertEqual(discover.status_code, 200)
        ids = {item["agent_id"] for item in discover.json()["agents"]}
        self.assertIn("peer-ops", ids)
        self.assertNotIn("peer-lab", ids)
        self.assertEqual(empty.status_code, 200)
        self.assertEqual(empty.json()["agents"], [])

    def test_derive_status_grace(self) -> None:
        now = 10_000
        self.assertEqual(derive_status(now_unix=now, expires_at_unix=now + 1, grace_seconds=60), "online")
        self.assertEqual(derive_status(now_unix=now, expires_at_unix=now, grace_seconds=60), "offline")


if __name__ == "__main__":
    unittest.main()
