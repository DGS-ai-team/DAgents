from __future__ import annotations

import sys
import unittest
from pathlib import Path

_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_ROOT))

from app.schemas.approval import (
    ApprovalRequiredEnvelopePayload,
    ApprovalToolCallsArgs,
    ResumeToolApprove,
    ResumeToolReject,
    ToolCallApprovalItem,
    is_tool_execution_approved,
    parse_resume_tool_decision,
)


class SchemaApprovalTestCase(unittest.TestCase):
    def test_parse_resume_approve_and_reject(self) -> None:
        self.assertIsInstance(parse_resume_tool_decision({"type": "approve"}), ResumeToolApprove)
        self.assertIsInstance(parse_resume_tool_decision({"type": "reject"}), ResumeToolReject)
        self.assertIsInstance(parse_resume_tool_decision({}), ResumeToolReject)
        self.assertIsInstance(parse_resume_tool_decision("x"), ResumeToolReject)

    def test_is_tool_execution_approved(self) -> None:
        self.assertTrue(is_tool_execution_approved({"type": "approve"}))
        self.assertFalse(is_tool_execution_approved({"type": "reject"}))

    def test_approval_envelope_roundtrip_dump(self) -> None:
        p = ApprovalRequiredEnvelopePayload(
            message="m",
            args=ApprovalToolCallsArgs(
                tool_calls=[ToolCallApprovalItem(id="1", name="t", arguments={"a": 1})]
            ),
            description="d",
        )
        d = p.model_dump()
        self.assertEqual(d["args"]["tool_calls"][0]["id"], "1")
        self.assertEqual(d["display_type"], "normal_text")


if __name__ == "__main__":
    unittest.main()
