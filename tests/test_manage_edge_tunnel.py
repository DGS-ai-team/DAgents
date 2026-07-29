from __future__ import annotations

import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import AsyncMock, patch

from fastapi.testclient import TestClient

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.config import ManageSettings  # noqa: E402
from manage.edge.proxy import path_allowed  # noqa: E402
from manage.manage_app import create_app  # noqa: E402


def _register_peer(client: TestClient, *, node_id: str, base_url: str, groups: list[str]) -> None:
    reg = client.post(
        "/v1/registry/agents",
        json={
            "agent_id": node_id,
            "base_url": base_url,
            "name": node_id,
            "expose_to_peers": True,
        },
    )
    assert reg.status_code == 200, reg.text
    groups_resp = client.patch(
        f"/v1/registry/agents/{node_id}/groups",
        json={"discovery_group": groups},
    )
    assert groups_resp.status_code == 200, groups_resp.text
    hb = client.post(f"/v1/registry/agents/{node_id}/heartbeat", json={"ttl_seconds": 300})
    assert hb.status_code == 200, hb.text


class EdgePathAllowedTests(unittest.TestCase):
    def test_scopes(self) -> None:
        self.assertTrue(path_allowed("/v1/agents/agt-1/hydrate", agent_id="agt-1", scopes=["agent"]))
        self.assertFalse(path_allowed("/v1/agents/agt-2/hydrate", agent_id="agt-1", scopes=["agent"]))
        self.assertTrue(path_allowed("/v1/messages", agent_id="agt-1", scopes=["messages"]))
        self.assertTrue(path_allowed("/v1/streams", agent_id="agt-1", scopes=["streams"]))
        self.assertFalse(path_allowed("/v1/messages", agent_id="agt-1", scopes=["agent"]))


class ManageEdgeTunnelTests(unittest.TestCase):
    def test_create_session_and_proxy(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            with TestClient(app) as client:
                _register_peer(client, node_id="owner-01", base_url="http://owner.local", groups=["ops"])
                _register_peer(client, node_id="home-01", base_url="http://home.local", groups=["ops"])

                created = client.post(
                    "/v1/edge/sessions",
                    headers={"x-dagents-agent-id": "owner-01"},
                    json={
                        "home_node_id": "home-01",
                        "agent_id": "agt-1",
                        "scopes": ["agent", "messages", "streams"],
                    },
                )
                self.assertEqual(created.status_code, 200, created.text)
                sess = created.json()
                self.assertTrue(sess["edge_session_id"].startswith("edge_"))
                self.assertIn("/v1/edge/", sess["proxy_prefix"])

                denied = client.get(
                    f"/v1/edge/{sess['edge_session_id']}/proxy/v1/agents/other/hydrate",
                    headers={"x-dagents-agent-id": "owner-01"},
                )
                self.assertEqual(denied.status_code, 403)

                from fastapi.responses import Response

                async def fake_forward(**kwargs):
                    return Response(
                        content=b'{"ok":true,"via":"edge"}',
                        media_type="application/json",
                        status_code=200,
                    )

                with patch("manage.edge.routes.forward_to_home", new=AsyncMock(side_effect=fake_forward)) as fwd:
                    proxied = client.get(
                        f"/v1/edge/{sess['edge_session_id']}/proxy/v1/agents/agt-1/hydrate",
                        headers={"x-dagents-agent-id": "owner-01"},
                    )
                self.assertEqual(proxied.status_code, 200, proxied.text)
                self.assertEqual(proxied.json()["via"], "edge")
                fwd.assert_awaited()
                self.assertEqual(fwd.await_args.kwargs["home_node_id"], "home-01")
                self.assertEqual(fwd.await_args.kwargs["target_path"], "/v1/agents/agt-1/hydrate")

    def test_proxy_requires_owner(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            with TestClient(app) as client:
                _register_peer(client, node_id="owner-01", base_url="http://owner.local", groups=["ops"])
                _register_peer(client, node_id="home-01", base_url="http://home.local", groups=["ops"])
                _register_peer(client, node_id="other-01", base_url="http://other.local", groups=["ops"])
                created = client.post(
                    "/v1/edge/sessions",
                    headers={"x-dagents-agent-id": "owner-01"},
                    json={"home_node_id": "home-01", "agent_id": "agt-1"},
                )
                sid = created.json()["edge_session_id"]
                stolen = client.get(
                    f"/v1/edge/{sid}/proxy/v1/agents/agt-1",
                    headers={"x-dagents-agent-id": "other-01"},
                )
                self.assertEqual(stolen.status_code, 403)


if __name__ == "__main__":
    unittest.main()
