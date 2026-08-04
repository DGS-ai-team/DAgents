"""A2A HTTP 路由：inbox / invoke 退役后返回 410。"""

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


def _register_agent(client: TestClient, agent_id: str, *, expose: bool = True) -> None:
    client.post(
        "/v1/registry/agents",
        json={"agent_id": agent_id, "base_url": f"http://{agent_id}.local", "expose_to_peers": expose},
        headers={"x-dagents-agent-id": agent_id},
    )
    client.post(
        f"/v1/registry/agents/{agent_id}/heartbeat",
        json={"ttl_seconds": 120},
        headers={"x-dagents-agent-id": agent_id},
    )


class ManageA2ARetiredTests(unittest.TestCase):
    def test_inbox_and_invoke_routes_return_410(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db", a2a_expire_sweep_seconds=0)
            app = create_app(settings)
            with TestClient(app) as client:
                _register_agent(client, "caller-01")
                _register_agent(client, "callee-01")

                created = client.post(
                    "/v1/a2a/tasks",
                    json={
                        "from_agent_id": "caller-01",
                        "to_agent_id": "callee-01",
                        "kind": "invoke",
                        "content": "ping",
                    },
                    headers={"x-dagents-agent-id": "caller-01"},
                )
                self.assertEqual(created.status_code, 410)
                self.assertEqual(created.json()["error"]["code"], "a2a_inbox_retired")

                inbox = client.get(
                    "/v1/a2a/inbox",
                    params={"agent_id": "callee-01"},
                    headers={"x-dagents-agent-id": "callee-01"},
                )
                self.assertEqual(inbox.status_code, 410)

                for path in (
                    "/v1/a2a/tasks/t1/ack",
                    "/v1/a2a/tasks/t1/reply",
                    "/v1/a2a/tasks/t1/caller_notify",
                    "/v1/a2a/tasks/t1/caller_resume",
                ):
                    resp = client.post(path, json={}, headers={"x-dagents-agent-id": "caller-01"})
                    self.assertEqual(resp.status_code, 410, path)

                caller_input = client.get(
                    "/v1/a2a/tasks/t1/caller_input",
                    params={"agent_id": "callee-01"},
                    headers={"x-dagents-agent-id": "callee-01"},
                )
                self.assertEqual(caller_input.status_code, 410)


if __name__ == "__main__":
    unittest.main()
