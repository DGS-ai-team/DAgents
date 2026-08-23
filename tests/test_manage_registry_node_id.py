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
from manage.registry.models import AgentRegisterRequest  # noqa: E402


class RegistryNodeIDTests(unittest.TestCase):
    def test_register_request_normalizes_node_id(self) -> None:
        req = AgentRegisterRequest(node_id="node-01", base_url="http://n.local")
        self.assertEqual(req.node_id, "node-01")
        self.assertEqual(req.agent_id, "node-01")
        self.assertEqual(req.metadata.get("node_id"), "node-01")

        legacy = AgentRegisterRequest(agent_id="legacy-01", base_url="http://l.local")
        self.assertEqual(legacy.node_id, "legacy-01")
        self.assertEqual(legacy.agent_id, "legacy-01")

        agent = AgentRegisterRequest(node_id="node-a", agent_id="agent-b", base_url="http://x.local")
        self.assertEqual(agent.node_id, "node-a")
        self.assertEqual(agent.agent_id, "agent-b")

    def test_register_via_node_id_only(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            with TestClient(app) as client:
                reg = client.post(
                    "/v1/registry/agents",
                    json={
                        "node_id": "node-p5",
                        "base_url": "http://p5.local",
                        "name": "P5 Node",
                    },
                )
                self.assertEqual(reg.status_code, 200, reg.text)
                agent = reg.json()["agent"]
                self.assertEqual(agent["agent_id"], "node-p5")
                self.assertEqual(agent["node_id"], "node-p5")

                listed = client.get("/v1/registry/agents", params={"status": "all"})
                self.assertEqual(listed.status_code, 200)
                rows = listed.json()["agents"]
                self.assertTrue(any(r.get("node_id") == "node-p5" for r in rows))

                # heartbeat 刷新 TTL
                hb = client.post(
                    "/v1/registry/agents/node-p5/heartbeat",
                    json={"ttl_seconds": 120},
                )
                self.assertEqual(hb.status_code, 200, hb.text)
                self.assertEqual((hb.json().get("metadata") or {}).get("node_id"), "node-p5")

                # 节点别名
                by_node = client.get("/v1/registry/nodes/node-p5")
                self.assertEqual(by_node.status_code, 200, by_node.text)
                self.assertEqual(by_node.json()["node_id"], "node-p5")

                # One outbound Node registration can advertise multiple
                # existing Agents without collapsing them into one Node row.
                second = client.post(
                    "/v1/registry/agents",
                    json={
                        "node_id": "node-p5",
                        "agent_id": "agent-p5-b",
                        "base_url": "http://p5.local",
                        "name": "Second Agent",
                    },
                )
                self.assertEqual(second.status_code, 200, second.text)
                self.assertEqual(second.json()["agent"]["node_id"], "node-p5")
                self.assertEqual(second.json()["agent"]["agent_id"], "agent-p5-b")


if __name__ == "__main__":
    unittest.main()
