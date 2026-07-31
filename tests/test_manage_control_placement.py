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


def _register_peer(
    client: TestClient,
    *,
    node_id: str,
    base_url: str,
    groups: list[str],
) -> None:
    payload = {
        "agent_id": node_id,
        "base_url": base_url,
        "name": node_id,
        "expose_to_peers": True,
        "metadata": {
            "host_info": {"os_kind": "linux", "sys_platform": "linux", "machine": "x86_64"},
            "display": {"available": False, "label": "Linux"},
        },
    }
    reg = client.post("/v1/registry/agents", json=payload)
    assert reg.status_code == 200, reg.text
    groups_resp = client.patch(f"/v1/registry/agents/{node_id}/groups", json={"discovery_group": groups})
    assert groups_resp.status_code == 200, groups_resp.text
    hb = client.post(f"/v1/registry/agents/{node_id}/heartbeat", json={"ttl_seconds": 300})
    assert hb.status_code == 200, hb.text


class ManageControlPlacementTests(unittest.TestCase):
    def test_peers_list_gone(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            with TestClient(app) as client:
                _register_peer(client, node_id="owner-01", base_url="http://owner.local", groups=["ops"])
                peers = client.get(
                    "/v1/control/peers",
                    headers={"x-dagents-agent-id": "owner-01"},
                )
            self.assertEqual(peers.status_code, 410, peers.text)
            self.assertEqual(peers.json()["detail"]["code"], "placement_deprecated")

    def test_create_gone(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            with TestClient(app) as client:
                _register_peer(client, node_id="owner-01", base_url="http://owner.local", groups=["ops"])
                _register_peer(client, node_id="home-01", base_url="http://home.local", groups=["ops"])
                resp = client.post(
                    "/v1/control/nodes/home-01/agents",
                    headers={"x-dagents-agent-id": "owner-01"},
                    json={
                        "owner_node_id": "owner-01",
                        "display_name": "远端助手",
                        "defaults": {},
                    },
                )
            self.assertEqual(resp.status_code, 410, resp.text)
            self.assertEqual(resp.json()["detail"]["code"], "placement_deprecated")

    def test_delete_gone(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            with TestClient(app) as client:
                _register_peer(client, node_id="owner-01", base_url="http://owner.local", groups=["ops"])
                _register_peer(client, node_id="home-01", base_url="http://home.local", groups=["ops"])
                deleted = client.delete(
                    "/v1/control/nodes/home-01/agents/agt-remote-1",
                    headers={"x-dagents-agent-id": "owner-01"},
                )
            self.assertEqual(deleted.status_code, 410, deleted.text)
            self.assertEqual(deleted.json()["detail"]["code"], "placement_deprecated")


if __name__ == "__main__":
    unittest.main()
