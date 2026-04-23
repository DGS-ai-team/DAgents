from __future__ import annotations

import asyncio
import json
import os
import sys
import unittest
from pathlib import Path
from unittest.mock import AsyncMock, MagicMock, patch

_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_ROOT))

from app.config.settings import get_settings  # noqa: E402
from app.context.models import OpenAIConversationContext  # noqa: E402
from app.harness.tools.agent_peer import (  # noqa: E402
    _cache_agent_list,
    _clear_agent_list_cache,
    _is_agent_list_cache_stale,
    _resolve_target_agent,
    agent_discover,
    agent_send_message,
)


class AgentPeerToolsTestCase(unittest.TestCase):
    def setUp(self) -> None:
        self._old_env = dict(os.environ)
        os.environ["AGENT_ID"] = "agent-test"
        os.environ["AGENT_ID_FILE_PATH"] = str(_ROOT / ".runtime" / "tests-agent-id")
        os.environ["DISCOVERY_GROUPS"] = "team-a,team-b"
        os.environ["REGISTRY_URL"] = "http://registry.local"
        get_settings(reload=True)
        _clear_agent_list_cache()

    def tearDown(self) -> None:
        os.environ.clear()
        os.environ.update(self._old_env)
        get_settings(reload=True)
        _clear_agent_list_cache()

    def test_agent_discover_with_capability_filter(self) -> None:
        agents = [
            {"agent_id": "a1", "base_url": "http://a1", "discovery_group": ["team-a"], "capabilities_hint": ["code"]},
            {"agent_id": "a2", "base_url": "http://a2", "discovery_group": ["team-a"], "capabilities_hint": ["review"]},
        ]
        with patch("app.harness.tools.agent_peer._discover_agents_by_groups", return_value=agents):
            with patch(
                "app.harness.tools.agent_peer._attach_agent_card_summary",
                side_effect=lambda item: {
                    **item,
                    "agent_card": {
                        "access_url": item.get("base_url"),
                        "access_host": "a2",
                        "access_port": 80,
                        "card_url": "http://a2/.well-known/agent-card.json",
                        "card_payload": {"name": item["agent_id"]},
                        "error": None,
                    },
                },
            ):
                raw = agent_discover(discovery_groups=["team-a"], capability_tags=["review"])
        body = json.loads(raw)
        self.assertEqual(len(body["payload"]["content"]["agents"]), 1)
        self.assertEqual(body["payload"]["content"]["agents"][0]["agent_id"], "a2")
        self.assertEqual(body["payload"]["content"]["agents"][0]["agent_card"]["card_payload"]["name"], "a2")

    def test_agent_send_message_success(self) -> None:
        target = {"agent_id": "a2", "base_url": "http://a2.local", "discovery_group": ["team-a"]}
        fake_resp = MagicMock()
        fake_resp.json.return_value = {"accepted": True, "session_id": "s-peer"}
        fake_resp.raise_for_status.return_value = None
        fake_client_ctx = MagicMock()
        fake_client = fake_client_ctx.__aenter__.return_value
        fake_client.post = AsyncMock(return_value=fake_resp)

        async def _run() -> None:
            with patch("app.harness.tools.agent_peer._resolve_target_agent", return_value=target):
                with patch("app.harness.tools.agent_peer.httpx.AsyncClient", return_value=fake_client_ctx):
                    with patch(
                        "app.harness.tools.agent_peer._collect_peer_stream_output",
                        new=AsyncMock(return_value=("assistant: hello", False)),
                    ):
                        raw = await agent_send_message.__wrapped__(  # type: ignore[attr-defined]
                            target_agent_id="a2",
                            message="hello",
                            context=OpenAIConversationContext(session_id="s-peer"),
                        )
            body = json.loads(raw)
            self.assertTrue(str(body["task"]["task_id"]).startswith("peer-trace-"))
            self.assertEqual(body["task"]["state"], "queued")
            self.assertEqual(body["payload"]["content"]["stream_output"], "assistant: hello")

        asyncio.run(_run())

    def test_agent_send_message_relay_success(self) -> None:
        os.environ["AGENT_PEER_DELIVERY_MODE"] = "relay"
        get_settings(reload=True)
        fake_resp = MagicMock()
        fake_resp.json.return_value = {
            "accepted": True,
            "target_agent_id": "a2",
            "target_base_url": "http://relay-a2.local",
            "session_id": "s-peer",
            "client_id": "peer-client-1",
        }
        fake_resp.raise_for_status.return_value = None
        fake_client_ctx = MagicMock()
        fake_client = fake_client_ctx.__aenter__.return_value
        fake_client.post = AsyncMock(return_value=fake_resp)

        async def _run() -> None:
            with patch("app.harness.tools.agent_peer._resolve_target_agent", side_effect=AssertionError("should not call")):
                with patch("app.harness.tools.agent_peer._require_registry_url", return_value="http://registry.local"):
                    with patch("app.harness.tools.agent_peer.httpx.AsyncClient", return_value=fake_client_ctx):
                        with patch(
                            "app.harness.tools.agent_peer._collect_peer_stream_output",
                            new=AsyncMock(return_value=("relay: ok", False)),
                        ):
                            raw = await agent_send_message.__wrapped__(  # type: ignore[attr-defined]
                                target_agent_id="a2",
                                message="hello-relay",
                                context=OpenAIConversationContext(session_id="s-peer"),
                            )
            body = json.loads(raw)
            self.assertTrue(str(body["task"]["task_id"]).startswith("peer-trace-"))
            self.assertEqual(body["payload"]["content"]["target_base_url"], "http://relay-a2.local")
            self.assertEqual(body["payload"]["content"]["stream_output"], "relay: ok")

        asyncio.run(_run())

    def test_resolve_target_agent_uses_cached_agent_list_after_discover(self) -> None:
        agents = [
            {"agent_id": "a1", "base_url": "http://a1", "discovery_group": ["team-a"]},
            {"agent_id": "a2", "base_url": "http://a2", "discovery_group": ["team-a"]},
        ]
        with patch("app.harness.tools.agent_peer._discover_agents_by_groups", return_value=agents):
            with patch("app.harness.tools.agent_peer._attach_agent_card_summary", side_effect=lambda item: item):
                _ = agent_discover(discovery_groups=["team-a"])
        # 若这里还回源网络，测试应失败；命中缓存则不会触发 side_effect。
        with patch("app.harness.tools.agent_peer._discover_agents_by_groups", side_effect=AssertionError("should not call")):
            target = _resolve_target_agent("a2")
        self.assertEqual(target["agent_id"], "a2")

    def test_resolve_target_agent_refreshes_when_cache_stale(self) -> None:
        stale_agents = [
            {"agent_id": "a1", "base_url": "http://old-a1", "discovery_group": ["team-a"]},
        ]
        fresh_agents = [
            {"agent_id": "a1", "base_url": "http://new-a1", "discovery_group": ["team-a"]},
        ]
        with patch("app.harness.tools.agent_peer._discover_agents_by_groups", return_value=stale_agents):
            with patch("app.harness.tools.agent_peer._attach_agent_card_summary", side_effect=lambda item: item):
                _ = agent_discover(discovery_groups=["team-a"])
        with patch("app.harness.tools.agent_peer._is_agent_list_cache_stale", return_value=True):
            with patch("app.harness.tools.agent_peer._discover_agents_by_groups", return_value=fresh_agents) as mocked:
                target = _resolve_target_agent("a1")
        self.assertEqual(target["base_url"], "http://new-a1")
        mocked.assert_called_once()

    def test_agent_send_message_failure_triggers_agent_list_refresh(self) -> None:
        target = {"agent_id": "a2", "base_url": "http://a2.local", "discovery_group": ["team-a"]}
        fake_client_ctx = MagicMock()
        fake_client = fake_client_ctx.__aenter__.return_value
        fake_client.post = AsyncMock(side_effect=RuntimeError("connect failed"))

        async def _run() -> None:
            with patch("app.harness.tools.agent_peer._resolve_target_agent", return_value=target):
                with patch("app.harness.tools.agent_peer.httpx.AsyncClient", return_value=fake_client_ctx):
                    with patch("app.harness.tools.agent_peer._refresh_agent_list_for_visible_groups") as refreshed:
                        _ = await agent_send_message.__wrapped__(  # type: ignore[attr-defined]
                            target_agent_id="a2",
                            message="hello",
                            context=OpenAIConversationContext(session_id="s-peer"),
                        )
            refreshed.assert_called_once()

        asyncio.run(_run())

    def test_agent_list_cache_ttl_reads_from_settings(self) -> None:
        _cache_agent_list([{"agent_id": "a1", "base_url": "http://a1", "discovery_group": ["team-a"]}])
        os.environ["AGENT_PEER_CACHE_TTL_SECONDS"] = "999"
        get_settings(reload=True)
        with patch("app.harness.tools.agent_peer.time.time", return_value=1.0):
            is_stale = _is_agent_list_cache_stale()
        self.assertFalse(is_stale)


if __name__ == "__main__":
    unittest.main()
