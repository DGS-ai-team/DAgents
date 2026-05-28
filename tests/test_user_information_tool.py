"""`ask_user_information` 工具与审批策略单测。"""

from __future__ import annotations

import unittest

from app.harness.tools.tool import decide_tool_approval, get_tools
from app.harness.tools.user_information import ASK_USER_INFORMATION_TOOL, AskUserInformationArgs


class AskUserInformationToolTests(unittest.TestCase):
    def test_tool_registered_in_get_tools(self) -> None:
        names = [getattr(item, "name", "") for item in get_tools()]
        self.assertIn(ASK_USER_INFORMATION_TOOL, names)

    def test_decide_tool_approval_never_requires_approval(self) -> None:
        decision = decide_tool_approval(
            tool_name=ASK_USER_INFORMATION_TOOL,
            tool_args={"question": "hello"},
        )
        self.assertFalse(decision.require_approval)

    def test_args_schema_accepts_options(self) -> None:
        parsed = AskUserInformationArgs.model_validate(
            {
                "question": "选择数据库",
                "options": [{"id": "pg", "label": "PostgreSQL", "value": "postgres"}],
                "allow_multiple": False,
            }
        )
        self.assertEqual(parsed.question, "选择数据库")
        self.assertEqual(len(parsed.options or []), 1)
