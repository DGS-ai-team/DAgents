from __future__ import annotations

import os
import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import patch

from fastapi.testclient import TestClient

_REGISTER_CENTER_ROOT = Path(__file__).resolve().parents[1] / "register_center"
if str(_REGISTER_CENTER_ROOT) not in sys.path:
    sys.path.insert(0, str(_REGISTER_CENTER_ROOT))

import rc_app  # noqa: E402


class RegisterCenterPersistenceTests(unittest.TestCase):
    def test_store_path_persists_records_across_app_instances(self) -> None:
        with TemporaryDirectory() as tmp:
            store_path = Path(tmp) / "registry.json"
            env = {"REGISTER_CENTER_STORE_PATH": str(store_path), "AGENT_PEER_SHARED_TOKEN": ""}
            with patch.dict(os.environ, env):
                app = rc_app.create_app()
                with TestClient(app) as client:
                    client.post(
                        "/v1/agents",
                        json={"agent_id": "agent-a", "base_url": "http://agent.local", "discovery_group": ["g1"]},
                    )

                app = rc_app.create_app()
                with TestClient(app) as client:
                    listed = client.get("/v1/agents", params={"discovery_group": "g1"})
                    deleted = client.delete("/v1/agents/agent-a")

                app = rc_app.create_app()
                with TestClient(app) as client:
                    after_delete = client.get("/v1/agents", params={"discovery_group": "g1"})

        self.assertEqual(listed.status_code, 200)
        self.assertEqual(listed.json()["agents"][0]["agent_id"], "agent-a")
        self.assertEqual(deleted.status_code, 200)
        self.assertEqual(after_delete.json()["agents"], [])

    def test_store_path_prunes_expired_records_on_startup(self) -> None:
        with TemporaryDirectory() as tmp:
            store_path = Path(tmp) / "registry.json"
            store_path.write_text(
                '{"agents":[{"agent_id":"old","base_url":"http://old.local","discovery_group":["g1"],'
                '"capabilities_hint":[],"registered_at_unix":1,"expires_at_unix":1}]}',
                encoding="utf-8",
            )
            with patch.dict(os.environ, {"REGISTER_CENTER_STORE_PATH": str(store_path), "AGENT_PEER_SHARED_TOKEN": ""}):
                app = rc_app.create_app()
                with TestClient(app) as client:
                    listed = client.get("/v1/agents", params={"discovery_group": "g1"})
                persisted_text = store_path.read_text(encoding="utf-8")

        self.assertEqual(listed.status_code, 200)
        self.assertEqual(listed.json()["agents"], [])
        self.assertIn('"agents": []', persisted_text)


class RegisterCenterSharedTokenTests(unittest.TestCase):
    def test_agent_routes_require_shared_token_when_configured(self) -> None:
        with patch.dict(os.environ, {"AGENT_PEER_SHARED_TOKEN": "secret"}):
            app = rc_app.create_app()
            with TestClient(app) as client:
                payload = {
                    "agent_id": "agent-a",
                    "base_url": "http://agent.local",
                    "discovery_group": ["g1"],
                }
                rejected = client.post("/v1/agents", json=payload)
                accepted = client.post("/v1/agents", headers={"x-dagents-a2a-token": "secret"}, json=payload)
                rejected_list = client.get("/v1/agents", params={"discovery_group": "g1"})
                accepted_list = client.get(
                    "/v1/agents",
                    headers={"x-dagents-a2a-token": "secret"},
                    params={"discovery_group": "g1"},
                )

        self.assertEqual(rejected.status_code, 401)
        self.assertEqual(accepted.status_code, 200)
        self.assertEqual(rejected_list.status_code, 401)
        self.assertEqual(accepted_list.status_code, 200)
        self.assertEqual(accepted_list.json()["agents"][0]["agent_id"], "agent-a")


class _FakeDownstreamResponse:
    status_code = 200
    text = ""

    def json(self) -> dict[str, object]:
        return {"accepted": True}


class _FakeRelayAsyncClient:
    posted_url = ""
    posted_payload: dict[str, object] = {}

    def __init__(self, *_args: object, **_kwargs: object) -> None:
        pass

    async def __aenter__(self):
        return self

    async def __aexit__(self, *_args: object) -> None:
        return None

    async def post(self, url: str, *, json: dict[str, object], **_kwargs: object) -> _FakeDownstreamResponse:
        type(self).posted_url = url
        type(self).posted_payload = dict(json)
        return _FakeDownstreamResponse()


class RegisterCenterRelayResumeTests(unittest.TestCase):
    def test_relay_resume_forwards_resume_value_without_content(self) -> None:
        app = rc_app.create_app()
        with TestClient(app) as client:
            client.post(
                "/v1/agents",
                json={"agent_id": "agent-b", "base_url": "http://agent-b.local", "discovery_group": ["g1"]},
            )
            with patch.object(rc_app.httpx, "AsyncClient", _FakeRelayAsyncClient):
                resp = client.post(
                    "/v1/relay",
                    json={
                        "target_agent_id": "agent-b",
                        "caller_groups": ["g1"],
                        "session_id": "peer-s",
                        "client_id": "approve-c",
                        "request_type": "resume",
                        "resume_value": {"type": "approve"},
                        "source": "agent-peer-approve-relay",
                        "priority": "resume",
                    },
                )
                metrics_resp = client.get("/metrics")

        self.assertEqual(resp.status_code, 200)
        self.assertEqual(_FakeRelayAsyncClient.posted_url, "http://agent-b.local/v1/messages")
        self.assertEqual(_FakeRelayAsyncClient.posted_payload["request_type"], "resume")
        self.assertEqual(_FakeRelayAsyncClient.posted_payload["resume_value"], {"type": "approve"})
        self.assertNotIn("content", _FakeRelayAsyncClient.posted_payload)
        self.assertEqual(metrics_resp.status_code, 200)
        self.assertIn(
            'dagents_a2a_operations_total{component="register_center",operation="relay",status="ok"}',
            metrics_resp.text,
        )


if __name__ == "__main__":
    unittest.main()
