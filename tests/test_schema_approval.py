"""`app.schemas.approval` 单测：resume 决策解析与 approve 判定。"""

from __future__ import annotations

import unittest

from app.schemas.approval import (
    ResumeToolApprove,
    ResumeToolReject,
    ResumeToolSelection,
    is_tool_execution_approved,
    parse_resume_tool_decision,
)


class ParseResumeToolDecisionTests(unittest.TestCase):
    """`parse_resume_tool_decision`：合法 dict、非法 dict、非 dict。"""

    def test_approve_dict(self) -> None:
        """显式 approve 应校验为 `ResumeToolApprove`。"""
        d = parse_resume_tool_decision({"type": "approve"})
        self.assertIsInstance(d, ResumeToolApprove)

    def test_selection_dict(self) -> None:
        """selection 携带 approved/rejected 列表。"""
        d = parse_resume_tool_decision(
            {"type": "selection", "approved": ["a"], "rejected": ["b"]},
        )
        self.assertIsInstance(d, ResumeToolSelection)
        self.assertEqual(d.approved, ["a"])
        self.assertEqual(d.rejected, ["b"])

    def test_invalid_or_non_dict_becomes_reject(self) -> None:
        """未知 `type` 或非 dict 时回落为 `ResumeToolReject`（不向外抛校验异常）。"""
        self.assertIsInstance(parse_resume_tool_decision({"type": "unknown"}), ResumeToolReject)
        self.assertIsInstance(parse_resume_tool_decision(None), ResumeToolReject)
        self.assertIsInstance(parse_resume_tool_decision("x"), ResumeToolReject)


class IsToolExecutionApprovedTests(unittest.TestCase):
    """`is_tool_execution_approved` 与 `parse_resume_tool_decision` 组合语义。"""

    def test_only_approve_returns_true(self) -> None:
        """仅 approve 结构返回 True；其余为 False。"""
        self.assertTrue(is_tool_execution_approved({"type": "approve"}))
        self.assertFalse(is_tool_execution_approved({"type": "reject"}))
        self.assertFalse(is_tool_execution_approved({"type": "selection", "approved": ["x"]}))


if __name__ == "__main__":
    unittest.main()
