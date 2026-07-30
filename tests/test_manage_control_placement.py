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


def _register_peer(
    client: TestClient,
    *,
    node_id: str,
    base_url: str,
    groups: list[str],
    metadata: dict | None = None,
) -> None:
    payload = {
        "agent_id": node_id,
        "base_url": base_url,
        "name": node_id,
        "expose_to_peers": True,
        "metadata": metadata or {
            "host_info": {"os_kind": "linux", "sys_platform": "linux", "machine": "x86_64"},
            "display": {"available": False, "label": "Linux"},
            "placement": {"allow_peer_create": True, "allow_screen_view": False},
        },
    }
    reg = client.post("/v1/registry/agents", json=payload)
    assert reg.status_code == 200, reg.text
    groups_resp = client.patch(f"/v1/registry/agents/{node_id}/groups", json={"discovery_group": groups})
    assert groups_resp.status_code == 200, groups_resp.text
    hb = client.post(f"/v1/registry/agents/{node_id}/heartbeat", json={"ttl_seconds": 300})
    assert hb.status_code == 200, hb.text


class ManageControlPlacementTests(unittest.TestCase):
    def test_peers_list_same_group(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            with TestClient(app) as client:
                _register_peer(client, node_id="owner-01", base_url="http://owner.local", groups=["ops"])
                _register_peer(
                    client,
                    node_id="home-01",
                    base_url="http://home.local",
                    groups=["ops"],
                    metadata={
                        "host_info": {"os_kind": "windows", "sys_platform": "windows", "machine": "amd64"},
                        "display": {"available": True, "label": "Windows"},
                        "placement": {"allow_peer_create": True, "allow_screen_view": True},
                    },
                )
                peers = client.get(
                    "/v1/control/peers",
                    headers={"x-dagents-agent-id": "owner-01"},
                )
            self.assertEqual(peers.status_code, 200, peers.text)
            body = peers.json()
            self.assertEqual(body["self_node_id"], "owner-01")
            self.assertEqual(len(body["nodes"]), 1)
            node = body["nodes"][0]
            self.assertEqual(node["node_id"], "home-01")
            self.assertEqual(node["host"]["display_label"], "Windows")
            self.assertTrue(node["allow_peer_create"])
            self.assertTrue(node["allow_screen_view"])

    def test_peers_list_includes_ops_nodes_without_a2a_expose(self) -> None:
        """回归：ops 角色注册 expose_to_peers=false，同组仍应出现在 Placement peers。"""
        with TemporaryDirectory() as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            with TestClient(app) as client:
                for nid in ("owner-ops", "home-ops"):
                    reg = client.post(
                        "/v1/registry/agents",
                        json={
                            "agent_id": nid,
                            "base_url": f"http://{nid}.local",
                            "name": nid,
                            "expose_to_peers": False,
                            "metadata": {
                                "host_info": {"os_kind": "linux", "sys_platform": "linux"},
                                "placement": {"allow_peer_create": True, "allow_screen_view": False},
                            },
                        },
                    )
                    self.assertEqual(reg.status_code, 200, reg.text)
                    groups = client.patch(
                        f"/v1/registry/agents/{nid}/groups",
                        json={"discovery_group": ["demo"]},
                    )
                    self.assertEqual(groups.status_code, 200, groups.text)
                    hb = client.post(f"/v1/registry/agents/{nid}/heartbeat", json={"ttl_seconds": 300})
                    self.assertEqual(hb.status_code, 200, hb.text)

                peers = client.get(
                    "/v1/control/peers",
                    headers={"x-dagents-agent-id": "owner-ops"},
                )
            self.assertEqual(peers.status_code, 200, peers.text)
            body = peers.json()
            self.assertEqual(len(body["nodes"]), 1)
            self.assertEqual(body["nodes"][0]["node_id"], "home-ops")
            self.assertTrue(body["nodes"][0]["allow_peer_create"])

            # A2A discover 仍应过滤掉未 expose 的节点
            discover = client.get(
                "/v1/registry/agents/discover",
                headers={"x-dagents-agent-id": "owner-ops"},
            )
            self.assertEqual(discover.status_code, 200, discover.text)
            self.assertEqual(discover.json().get("agents") or [], [])

    def test_create_and_delete_via_home_mock(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            with TestClient(app) as client:
                _register_peer(client, node_id="owner-01", base_url="http://owner.local", groups=["ops"])
                _register_peer(client, node_id="home-01", base_url="http://home.local", groups=["ops"])

                created_home = {
                    "agent_id": "agt-remote-1",
                    "display_name": "远端助手",
                    "sandbox_enabled": False,
                    "sandbox_backend": "process",
                    "config_snapshot": {"defaults": {}},
                    "host": {
                        "os_kind": "linux",
                        "sys_platform": "linux",
                        "machine": "x86_64",
                        "display_available": False,
                        "display_label": "Linux",
                    },
                    "created_at": "2026-07-29T00:00:00Z",
                    "updated_at": "2026-07-29T00:00:00Z",
                }

                with patch(
                    "manage.control.routes.call_home_create_agent",
                    return_value=created_home,
                ) as create_mock:
                    resp = client.post(
                        "/v1/control/nodes/home-01/agents",
                        headers={"x-dagents-agent-id": "owner-01"},
                        json={
                            "owner_node_id": "owner-01",
                            "display_name": "远端助手",
                            "defaults": {},
                        },
                    )
                self.assertEqual(resp.status_code, 200, resp.text)
                body = resp.json()
                self.assertEqual(body["agent_id"], "agt-remote-1")
                self.assertEqual(body["home_node_id"], "home-01")
                self.assertEqual(body["owner_node_id"], "owner-01")
                self.assertEqual(body["origin"], "remote")
                create_mock.assert_called_once()
                call_kwargs = create_mock.call_args.kwargs
                self.assertEqual(call_kwargs["base_url"], "http://home.local")
                self.assertEqual(call_kwargs["home_node_id"], "home-01")
                self.assertEqual(call_kwargs["payload"]["placement"]["owner_node_id"], "owner-01")

                with patch(
                    "manage.control.routes.call_home_delete_agent",
                    return_value={"ok": True, "agent_id": "agt-remote-1", "home_deleted": True},
                ) as delete_mock:
                    deleted = client.delete(
                        "/v1/control/nodes/home-01/agents/agt-remote-1",
                        headers={"x-dagents-agent-id": "owner-01"},
                    )
                self.assertEqual(deleted.status_code, 200, deleted.text)
                self.assertTrue(deleted.json()["home_deleted"])
                delete_mock.assert_called_once()
                self.assertEqual(delete_mock.call_args.kwargs["owner_node_id"], "owner-01")

    def test_create_rejects_same_home_as_owner(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            with TestClient(app) as client:
                _register_peer(client, node_id="owner-01", base_url="http://owner.local", groups=["ops"])
                resp = client.post(
                    "/v1/control/nodes/owner-01/agents",
                    headers={"x-dagents-agent-id": "owner-01"},
                    json={
                        "owner_node_id": "owner-01",
                        "display_name": "x",
                    },
                )
            self.assertEqual(resp.status_code, 400)


if __name__ == "__main__":
    unittest.main()
