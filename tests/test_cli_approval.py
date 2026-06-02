from __future__ import annotations

import unittest

from app.cli.api_client import _parse_sse_block
from app.cli.approval import (
    build_all_approved_decision,
    build_all_rejected_decision,
    build_selection_decision,
    extract_tool_approval_requests,
    parse_selection_tokens,
)


class CliApprovalTests(unittest.TestCase):
    def _payload(self):
        return {
            "approval_args": {
                "tool_calls": [
                    {
                        "id": "call_1",
                        "name": "read_file",
                        "arguments": {"path": "README.md"},
                        "raw_arguments": '{"path":"README.md"}',
                        "approval_reason": "filesystem read",
                        "risk_level": "low",
                    },
                    {
                        "id": "call_2",
                        "name": "bash",
                        "arguments": {"command": "ls"},
                    },
                ]
            }
        }

    def test_extract_tool_approval_requests_reads_sse_payload(self) -> None:
        requests = extract_tool_approval_requests(self._payload())

        self.assertEqual([item.call_id for item in requests], ["call_1", "call_2"])
        self.assertEqual(requests[0].name, "read_file")
        self.assertEqual(requests[0].arguments, {"path": "README.md"})
        self.assertEqual(requests[0].approval_reason, "filesystem read")
        self.assertEqual(requests[0].risk_level, "low")

    def test_approve_and_reject_all_resume_payloads_cover_all_calls(self) -> None:
        requests = extract_tool_approval_requests(self._payload())

        approved = build_all_approved_decision(requests).to_resume_value()
        rejected = build_all_rejected_decision(requests).to_resume_value()

        self.assertEqual(approved, {"type": "selection", "approved": ["call_1", "call_2"], "rejected": []})
        self.assertEqual(rejected, {"type": "selection", "approved": [], "rejected": ["call_1", "call_2"]})

    def test_build_approval_resume_routes_child_session(self) -> None:
        from app.cli.approval import build_approval_resume

        requests = extract_tool_approval_requests(self._payload())
        data = {"child_session_id": "child-s1", "approval_id": "ap-99"}
        resume = build_approval_resume(data, build_all_approved_decision(requests))
        self.assertEqual(resume["child_session_id"], "child-s1")
        self.assertEqual(resume["approval_id"], "ap-99")
        self.assertEqual(resume["approved"], ["call_1", "call_2"])

    def test_selection_resume_payload_rejects_unselected_calls(self) -> None:
        requests = extract_tool_approval_requests(self._payload())

        decision = build_selection_decision(requests, {"call_2"}).to_resume_value()

        self.assertEqual(decision, {"type": "selection", "approved": ["call_2"], "rejected": ["call_1"]})

    def test_selection_tokens_support_indexes_and_call_ids(self) -> None:
        requests = extract_tool_approval_requests(self._payload())

        selected = parse_selection_tokens("1 call_2", requests)

        self.assertEqual(selected, {"call_1", "call_2"})

    def test_selection_tokens_reject_unknown_values(self) -> None:
        requests = extract_tool_approval_requests(self._payload())

        with self.assertRaises(ValueError):
            parse_selection_tokens("3", requests)


class CliSseParserTests(unittest.TestCase):
    def test_parse_sse_block_reads_event_id_and_json_payload(self) -> None:
        event = _parse_sse_block(
            'id: 7\nevent: assistant\ndata: {"session_id":"s1","data":{"content":"hi"}}'
        )

        self.assertIsNotNone(event)
        assert event is not None
        self.assertEqual(event.event_id, "7")
        self.assertEqual(event.event_type, "assistant")
        self.assertEqual(event.session_id, "s1")
        self.assertEqual(event.data, {"content": "hi"})


if __name__ == "__main__":
    unittest.main()
