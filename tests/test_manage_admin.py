from __future__ import annotations

import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

from fastapi.testclient import TestClient

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.config import ManageSettings  # noqa: E402
from manage.manage_app import create_app  # noqa: E402


def _register_agent(client: TestClient, agent_id: str, *, base_url: str | None = None) -> None:
    url = base_url or f"http://{agent_id}.local"
    client.post(
        "/v1/registry/agents",
        json={"agent_id": agent_id, "base_url": url, "expose_to_peers": True},
        headers={"x-dagents-agent-id": agent_id},
    )
    client.post(
        f"/v1/registry/agents/{agent_id}/heartbeat",
        json={"ttl_seconds": 120},
        headers={"x-dagents-agent-id": agent_id},
    )


def _assign_groups(client: TestClient, agent_id: str, groups: list[str] | None = None) -> None:
    payload = {"discovery_group": groups or ["default-lab"]}
    resp = client.patch(
        f"/v1/registry/agents/{agent_id}/groups",
        json=payload,
    )
    assert resp.status_code == 200, resp.text


class ManageAdminTests(unittest.TestCase):
    def test_list_a2a_tasks_readonly(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings(
                host="127.0.0.1",
                port=8020,
                db_path=Path(tmp) / "manage.db",
                blob_dir=None,
                blob_max_bytes=None,
                offline_grace_seconds=86400,
                audit_max_entries=100,
                legacy_direct_relay=False,
                a2a_inbox_content_max_chars=4096,
                a2a_expire_sweep_seconds=0,
            )
            app = create_app(settings)
            with TestClient(app) as client:
                _register_agent(client, "caller-x")
                _register_agent(client, "callee-y")
                _assign_groups(client, "caller-x")
                _assign_groups(client, "callee-y")

                created = client.post(
                    "/v1/a2a/tasks",
                    json={
                        "from_agent_id": "caller-x",
                        "to_agent_id": "callee-y",
                        "content": "hello admin",
                    },
                    headers={"x-dagents-agent-id": "caller-x"},
                )
                self.assertEqual(created.status_code, 200)
                task_id = created.json()["task_id"]

                listed = client.get("/v1/admin/a2a/tasks")
                self.assertEqual(listed.status_code, 200)
                body = listed.json()
                self.assertGreaterEqual(body["total"], 1)
                ids = [t["task_id"] for t in body["tasks"]]
                self.assertIn(task_id, ids)
                self.assertEqual(body["tasks"][0]["status"], "queued")

                polled = client.get("/v1/a2a/inbox", params={"agent_id": "callee-y"})
                self.assertEqual(polled.status_code, 200)
                self.assertEqual(len(polled.json()["tasks"]), 1)

                listed2 = client.get("/v1/admin/a2a/tasks", params={"status": "delivered"})
                self.assertTrue(any(t["task_id"] == task_id for t in listed2.json()["tasks"]))

    def test_admin_node_session_proxy_removed(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings(
                host="127.0.0.1",
                port=8020,
                db_path=Path(tmp) / "manage.db",
                blob_dir=None,
                blob_max_bytes=None,
                offline_grace_seconds=86400,
                audit_max_entries=100,
                legacy_direct_relay=False,
                a2a_inbox_content_max_chars=4096,
                a2a_expire_sweep_seconds=0,
            )
            app = create_app(settings)
            with TestClient(app) as client:
                _register_agent(client, "node-z", base_url="http://node-z.test")
                for path in (
                    "/v1/admin/nodes/node-z/sessions",
                    "/v1/admin/nodes/node-z/sessions/s-1/context",
                ):
                    resp = client.get(path)
                    self.assertEqual(resp.status_code, 404, path)


if __name__ == "__main__":
    unittest.main()
