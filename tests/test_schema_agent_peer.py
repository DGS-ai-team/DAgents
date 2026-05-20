from __future__ import annotations

import unittest

from app.schemas.agent_peer import AgentPeerTarget, build_agent_peer_envelope, parse_agent_peer_envelope_from_text


class AgentPeerEnvelopeSchemaTests(unittest.TestCase):
    def test_build_envelope_preserves_trace_and_payload(self) -> None:
        envelope = build_agent_peer_envelope(
            caller_agent_id="agent-a",
            caller_session_id="session-a",
            caller_groups=["g1"],
            target_agent_id="agent-b",
            target_groups=None,
            intent="delegate",
            payload_content={"question": "hi"},
            payload_content_type="application/json",
            trace_id="trace-unit",
        )

        self.assertEqual(envelope.protocol_version, "a2a-dagents/1.0")
        self.assertEqual(envelope.trace_id, "trace-unit")
        self.assertTrue(envelope.message_id.startswith("msg-"))
        self.assertEqual(envelope.caller.agent_id, "agent-a")
        self.assertEqual(envelope.target.agent_id, "agent-b")
        self.assertEqual(envelope.payload.content, {"question": "hi"})

    def test_target_requires_exactly_one_routing_mode(self) -> None:
        self.assertEqual(AgentPeerTarget(agent_id=None, discovery_groups=[" g1 "]).discovery_groups, ["g1"])
        self.assertEqual(AgentPeerTarget(agent_id=" agent-b ", discovery_groups=[]).agent_id, "agent-b")

        with self.assertRaises(ValueError):
            AgentPeerTarget(agent_id=None, discovery_groups=[])
        with self.assertRaises(ValueError):
            AgentPeerTarget(agent_id="agent-b", discovery_groups=["g1"])
        with self.assertRaises(ValueError):
            AgentPeerTarget(agent_id=None, discovery_groups=["g1", " "])

    def test_parse_agent_peer_envelope_from_text_returns_none_for_invalid_input(self) -> None:
        envelope = build_agent_peer_envelope(
            caller_agent_id="agent-a",
            caller_session_id="session-a",
            caller_groups=["g1"],
            target_agent_id="agent-b",
            target_groups=None,
            intent="ask",
            payload_content="hello",
        )

        self.assertEqual(parse_agent_peer_envelope_from_text(envelope.model_dump_json()), envelope)
        self.assertIsNone(parse_agent_peer_envelope_from_text("not-json"))
        self.assertIsNone(parse_agent_peer_envelope_from_text('{"protocol_version":"a2a-dagents/1.0"}'))


if __name__ == "__main__":
    unittest.main()
