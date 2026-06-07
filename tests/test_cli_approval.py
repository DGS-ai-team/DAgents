from __future__ import annotations

import unittest

from app.cli.api_client import _decode_utf8_chunks, _parse_sse_block
from app.cli.approval import (
    build_all_approved_decision,
    build_all_rejected_decision,
    build_approval_decision_from_map,
    build_selection_decision,
    clamp_menu_selection_index,
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

    def test_clamp_menu_selection_index_up_from_first_stays(self) -> None:
        """双项菜单在首项按 Up 不应环绕到末项（同意→不同意错位）。"""
        self.assertEqual(clamp_menu_selection_index(0, -1, 2), 0)
        self.assertEqual(clamp_menu_selection_index(0, 1, 2), 1)
        self.assertEqual(clamp_menu_selection_index(1, 1, 2), 1)
        self.assertEqual(clamp_menu_selection_index(1, -1, 2), 0)

    def test_build_approval_decision_from_map(self) -> None:
        requests = extract_tool_approval_requests(self._payload())
        decision = build_approval_decision_from_map(
            requests,
            {"call_1": True, "call_2": False},
        )
        self.assertEqual(decision.approved, ["call_1"])
        self.assertEqual(decision.rejected, ["call_2"])


class CliSseParserTests(unittest.TestCase):
    def test_decode_utf8_chunks_preserves_multibyte_at_chunk_boundary(self) -> None:
        """SSE 分块不得在 UTF-8 码点中间 decode，否则中文会变成 �。"""
        payload = 'data: {"data":{"content":"现在是 2026 年 6 月 2 日"}}\n\n'
        raw = payload.encode("utf-8")
        month = "月".encode("utf-8")
        split_at = raw.index(month) + 1  # 在「月」的第 2 个字节处切开
        chunks = [raw[:split_at], raw[split_at:]]

        broken = chunks[0].decode("utf-8", errors="replace") + chunks[1].decode("utf-8", errors="replace")
        self.assertIn("\ufffd", broken)

        fixed = _decode_utf8_chunks(chunks)
        self.assertNotIn("\ufffd", fixed)
        self.assertIn("6 月 2", fixed)

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
