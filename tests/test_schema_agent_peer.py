from __future__ import annotations

import sys
import unittest
from pathlib import Path

_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_ROOT))

from app.schemas.agent_peer import (  # noqa: E402
    AgentPeerEnvelope,
    AgentPeerTarget,
    AgentPeerTask,
    build_agent_peer_envelope,
    parse_agent_peer_envelope_from_text,
)


class AgentPeerSchemaTestCase(unittest.TestCase):
    def test_target_mutual_exclusion(self) -> None:
        with self.assertRaises(Exception):
            AgentPeerTarget(agent_id=None, discovery_groups=[])
        with self.assertRaises(Exception):
            AgentPeerTarget(agent_id="a1", discovery_groups=["g1"])
        ok_single = AgentPeerTarget(agent_id="a1", discovery_groups=[])
        self.assertEqual(ok_single.agent_id, "a1")
        ok_group = AgentPeerTarget(agent_id=None, discovery_groups=["g1"])
        self.assertEqual(ok_group.discovery_groups, ["g1"])

    def test_build_and_parse_envelope(self) -> None:
        env = build_agent_peer_envelope(
            caller_agent_id="agent-a",
            caller_session_id="sid-1",
            caller_groups=["team-a"],
            target_agent_id="agent-b",
            target_groups=None,
            intent="delegate",
            payload_content="hello",
            payload_content_type="text/plain",
            task=AgentPeerTask(task_id="task-1", state="queued"),
        )
        self.assertEqual(env.protocol_version, "a2a-dagents/1.0")
        dumped = env.model_dump_json()
        parsed = parse_agent_peer_envelope_from_text(dumped)
        self.assertIsNotNone(parsed)
        assert parsed is not None
        self.assertEqual(parsed.trace_id, env.trace_id)
        self.assertEqual(parsed.task.task_id if parsed.task else "", "task-1")

    def test_invalid_text_returns_none(self) -> None:
        self.assertIsNone(parse_agent_peer_envelope_from_text("not-json"))
        self.assertIsNone(parse_agent_peer_envelope_from_text(""))
        self.assertIsNone(parse_agent_peer_envelope_from_text("[]"))

    def test_task_update_requires_manual_task_field(self) -> None:
        env = AgentPeerEnvelope.model_validate(
            {
                "protocol_version": "a2a-dagents/1.0",
                "trace_id": "t1",
                "message_id": "m1",
                "timestamp_unix_ms": 1,
                "caller": {"agent_id": "a", "session_id": "s", "discovery_groups": ["g1"]},
                "target": {"agent_id": "b", "discovery_groups": []},
                "intent": "task_update",
                "payload": {"content_type": "application/json", "content": {"ok": True}},
            }
        )
        self.assertEqual(env.intent, "task_update")


if __name__ == "__main__":
    unittest.main()
