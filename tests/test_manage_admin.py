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


class ManageAdminTests(unittest.TestCase):
    def test_admin_node_session_proxy_removed(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            with TestClient(app) as client:
                _register_agent(client, "node-z", base_url="http://node-z.test")
                for path in (
                    "/v1/admin/nodes/node-z/sessions",
                    "/v1/admin/nodes/node-z/sessions/s-1/context",
                    "/v1/admin/a2a/tasks",
                ):
                    resp = client.get(path)
                    self.assertEqual(resp.status_code, 404, path)

    def test_a2a_http_routes_gone(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            with TestClient(app) as client:
                resp = client.post("/v1/a2a/tasks", json={})
                self.assertEqual(resp.status_code, 404)
                resp = client.get("/v1/a2a/inbox")
                self.assertEqual(resp.status_code, 404)


if __name__ == "__main__":
    unittest.main()
