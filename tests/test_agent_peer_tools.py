from __future__ import annotations

import unittest
from unittest.mock import patch

from app.harness.tools import agent_peer
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
            "app.harness.tools.agent_peer.get_settings",
            return_value=settings_namespace(agent_peer_shared_token="secret"),
        ):
            self.assertEqual(agent_peer._a2a_auth_headers(), {"x-dagents-a2a-token": "secret"})


class AgentPeerStreamSummaryTests(unittest.IsolatedAsyncioTestCase):
    async def test_collect_peer_stream_summary_requests_history_replay(self) -> None:
        with patch("app.harness.tools.agent_peer.httpx.AsyncClient", _FakeAsyncClient):
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
        with patch("app.harness.tools.agent_peer.httpx.AsyncClient", _FakeAsyncClient), patch(
            "app.harness.tools.agent_peer.get_settings",
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


if __name__ == "__main__":
    unittest.main()
