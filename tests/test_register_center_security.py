from __future__ import annotations

import os
import sys
import unittest
from pathlib import Path
from unittest.mock import patch

from fastapi.testclient import TestClient

_REGISTER_CENTER_ROOT = Path(__file__).resolve().parents[1] / "register_center"
if str(_REGISTER_CENTER_ROOT) not in sys.path:
    sys.path.insert(0, str(_REGISTER_CENTER_ROOT))

import rc_app  # noqa: E402


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

        self.assertEqual(resp.status_code, 200)
        self.assertEqual(_FakeRelayAsyncClient.posted_url, "http://agent-b.local/v1/messages")
        self.assertEqual(_FakeRelayAsyncClient.posted_payload["request_type"], "resume")
        self.assertEqual(_FakeRelayAsyncClient.posted_payload["resume_value"], {"type": "approve"})
        self.assertNotIn("content", _FakeRelayAsyncClient.posted_payload)


if __name__ == "__main__":
    unittest.main()
