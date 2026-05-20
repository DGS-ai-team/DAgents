from __future__ import annotations

import json
import unittest
from unittest.mock import patch

from app.harness.tools import agent_peer, agent_peer_common
from tests.test_support.stub_settings import settings_namespace


class _FakeStreamResponse:
    def __init__(self, lines: list[str]) -> None:
        self._lines = lines

    async def __aenter__(self):
        return self

    async def __aexit__(self, *_args: object) -> None:
        return None

    def raise_for_status(self) -> None:
        return None

    async def aiter_lines(self):
        for line in self._lines:
            yield line


class _FakeAsyncClient:
    stream_kwargs: dict[str, object] = {}
    stream_url = ""

    def __init__(self, *_args: object, **_kwargs: object) -> None:
        pass

    async def __aenter__(self):
        return self

    async def __aexit__(self, *_args: object) -> None:
        return None

    def stream(self, _method: str, url: str, **kwargs: object) -> _FakeStreamResponse:
        type(self).stream_url = url
        type(self).stream_kwargs = kwargs
        return _FakeStreamResponse(
            [
                "event: assistant",
                'data: {"session_id":"peer-s","data":{"content":"hello"}}',
                "",
                "event: done",
                'data: {"session_id":"peer-s","data":{}}',
                "",
            ]
        )


class AgentPeerAuthHeaderTests(unittest.TestCase):
    def test_a2a_auth_headers_include_shared_token_when_configured(self) -> None:
        with patch(
            "app.harness.tools.agent_peer_common.get_settings",
            return_value=settings_namespace(agent_peer_shared_token="secret"),
        ):
            self.assertEqual(agent_peer_common.a2a_auth_headers(), {"x-dagents-a2a-token": "secret"})


class AgentPeerStreamSummaryTests(unittest.IsolatedAsyncioTestCase):
    async def test_collect_peer_stream_summary_requests_history_replay(self) -> None:
        with patch("app.harness.tools.agent_peer_common.httpx.AsyncClient", _FakeAsyncClient):
            summary = await agent_peer._collect_peer_stream_summary(
                base_url="http://peer.example",
                client_id="peer-c",
                session_id="peer-s",
                timeout_seconds=1,
            )

        self.assertEqual(summary.text, "hello")
        self.assertEqual(summary.final_state, "succeeded")
        self.assertEqual(_FakeAsyncClient.stream_url, "http://peer.example/v1/streams?client_id=peer-c")
        self.assertEqual(_FakeAsyncClient.stream_kwargs.get("headers"), {"Last-Event-ID": "-1"})

    async def test_collect_peer_stream_summary_sends_shared_token_on_stream(self) -> None:
        with patch("app.harness.tools.agent_peer_common.httpx.AsyncClient", _FakeAsyncClient), patch(
            "app.harness.tools.agent_peer_common.get_settings",
            return_value=settings_namespace(agent_peer_shared_token="secret"),
        ):
            await agent_peer._collect_peer_stream_summary(
                base_url="http://peer.example",
                client_id="peer-c",
                session_id="peer-s",
                timeout_seconds=1,
            )

        self.assertEqual(
            _FakeAsyncClient.stream_kwargs.get("headers"),
            {"Last-Event-ID": "-1", "x-dagents-a2a-token": "secret"},
        )


class _FakeRelayResponse:
    status_code = 200
    text = ""

    def raise_for_status(self) -> None:
        return None

    def json(self) -> dict[str, object]:
        return {
            "accepted": True,
            "target_agent_id": "agent-b",
            "target_base_url": "http://agent-b.local",
            "session_id": "peer-s",
            "client_id": "approve-c",
        }


class _FakeRelayAsyncClient:
    posted_payload: dict[str, object] = {}
    posted_url = ""

    def __init__(self, *_args: object, **_kwargs: object) -> None:
        pass

    async def __aenter__(self):
        return self

    async def __aexit__(self, *_args: object) -> None:
        return None

    async def post(self, url: str, *, json: dict[str, object], **_kwargs: object) -> _FakeRelayResponse:
        type(self).posted_url = url
        type(self).posted_payload = dict(json)
        return _FakeRelayResponse()


class AgentPeerApproveRelayTests(unittest.IsolatedAsyncioTestCase):
    async def test_agent_peer_approve_tools_uses_relay_for_resume_when_configured(self) -> None:
        async def fake_summary(**_kwargs: object) -> agent_peer_common.PeerStreamSummary:
            return agent_peer_common.PeerStreamSummary(text="done", final_state="succeeded")

        settings = settings_namespace(
            agent_id="agent-a",
            discovery_groups=["g1"],
            registry_url="http://rc.local",
            agent_peer_delivery_mode="relay",
            agent_peer_stream_timeout_seconds=1,
        )
        with patch("app.harness.tools.agent_peer.httpx.AsyncClient", _FakeRelayAsyncClient), patch(
            "app.harness.tools.agent_peer.get_settings",
            return_value=settings,
        ), patch("app.harness.tools.agent_peer_registry.get_settings", return_value=settings), patch(
            "app.harness.tools.agent_peer._collect_peer_stream_summary", fake_summary
        ):
            result = await agent_peer.agent_peer_approve_tools.__wrapped__(
                target_agent_id="agent-b",
                target_session_id="peer-s",
                decision="approve",
            )

        payload = json.loads(result)
        self.assertEqual(_FakeRelayAsyncClient.posted_url, "http://rc.local/v1/relay")
        self.assertEqual(_FakeRelayAsyncClient.posted_payload["request_type"], "resume")
        self.assertEqual(_FakeRelayAsyncClient.posted_payload["resume_value"], {"type": "approve"})
        self.assertNotIn("content", _FakeRelayAsyncClient.posted_payload)
        self.assertEqual(payload["payload"]["content"]["target_base_url"], "http://agent-b.local")
        self.assertEqual(payload["task"]["state"], "succeeded")


class AgentPeerCommonHelperTests(unittest.TestCase):
    def test_build_resume_value_validates_selection_overlap(self) -> None:
        self.assertEqual(agent_peer_common.build_resume_value(decision="approve", approved_call_ids=None, rejected_call_ids=None), {"type": "approve"})
        self.assertEqual(agent_peer_common.build_resume_value(decision="reject", approved_call_ids=None, rejected_call_ids=None), {"type": "reject"})
        self.assertEqual(
            agent_peer_common.build_resume_value(
                decision="selection",
                approved_call_ids=[" call-b ", "call-a", "call-a"],
                rejected_call_ids=["call-c"],
            ),
            {"type": "selection", "approved": ["call-a", "call-b"], "rejected": ["call-c"]},
        )

        with self.assertRaises(ValueError):
            agent_peer_common.build_resume_value(decision="selection", approved_call_ids=["call-a"], rejected_call_ids=["call-a"])
        with self.assertRaises(ValueError):
            agent_peer_common.build_resume_value(decision="selection", approved_call_ids=[], rejected_call_ids=[])


if __name__ == "__main__":
    unittest.main()
