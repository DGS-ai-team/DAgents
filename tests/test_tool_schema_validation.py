"""工具参数 schema 与运行时校验测试。"""

from __future__ import annotations

import unittest
from types import SimpleNamespace
from unittest.mock import patch

from app.context.models import PendingToolCall
from app.core.main_agent.tool_execution import build_approval_required_payload, pending_tool_call_to_approval_item
from app.harness.tools.bash import bash_run
from app.harness.tools.fs import search_replace, write_file
from app.harness.tools.tool import (
    ToolApprovalDecision,
    _tool_to_spec,
    _validate_tool_arguments,
    decide_tool_approval,
)


class ToolSchemaValidationTests(unittest.TestCase):
    def test_bash_run_schema_requires_command_and_forbids_extra_args(self) -> None:
        """bash_run 应暴露显式 schema，并在执行前拒绝缺必填或未知参数。"""
        spec = _tool_to_spec(bash_run)

        self.assertIn("command", spec.parameters["required"])
        self.assertFalse(spec.parameters["additionalProperties"])

        with self.assertRaisesRegex(ValueError, "command"):
            spec.invoke({}, None)
        with self.assertRaisesRegex(ValueError, "unexpected"):
            spec.invoke({"command": "pwd", "unexpected": True}, None)

    def test_fs_write_schema_forbids_unknown_args_before_filesystem_touch(self) -> None:
        """write_file 的未知字段应在工具函数执行前被参数模型拒绝。"""
        spec = _tool_to_spec(write_file)

        self.assertEqual(set(spec.parameters["required"]), {"path", "content"})
        with self.assertRaisesRegex(ValueError, "extra"):
            spec.invoke({"path": "x.txt", "content": "ok", "extra": "no"}, None)

    def test_search_replace_validated_args_preserve_defaults(self) -> None:
        """Pydantic 校验后的参数应保留工具默认值，供执行层稳定展开。"""
        args_schema = getattr(search_replace, "args_schema")

        validated = _validate_tool_arguments(
            args_schema,
            {"path": "a.txt", "old_string": "old", "new_string": "new"},
        )

        self.assertEqual(validated["replace_all"], False)
        with self.assertRaisesRegex(ValueError, "old_string"):
            _validate_tool_arguments(
                args_schema,
                {"path": "a.txt", "old_string": "", "new_string": "new"},
            )

    def test_decide_tool_approval_returns_reason_and_risk(self) -> None:
        """结构化审批决策应保留原 bool 语义并说明来源。"""
        settings = SimpleNamespace(agent_tool_approval_mode="always")

        with patch("app.harness.tools.tool.get_settings", return_value=settings):
            decision = decide_tool_approval(tool_name="write_file", tool_args={})

        self.assertTrue(decision.require_approval)
        self.assertEqual(decision.risk_level, "high")
        self.assertEqual(decision.mode, "global:always")
        self.assertIn("always", decision.reason)

    def test_approval_payload_includes_reason_metadata(self) -> None:
        """approval_required 的 tool_calls 应带审批原因、风险等级与策略来源。"""
        decision = ToolApprovalDecision(
            require_approval=True,
            reason="需要人工确认写文件。",
            risk_level="high",
            mode="tool:always",
        )
        item = pending_tool_call_to_approval_item(
            PendingToolCall(call_id="call-1", name="write_file", arguments={"path": "a.txt"}),
            decision=decision,
        )

        payload = build_approval_required_payload([item], assistant_content="need edit")
        tool_call = payload["args"]["tool_calls"][0]

        self.assertEqual(tool_call["approval_reason"], decision.reason)
        self.assertEqual(tool_call["risk_level"], "high")
        self.assertEqual(tool_call["approval_mode"], "tool:always")


if __name__ == "__main__":
    unittest.main()
