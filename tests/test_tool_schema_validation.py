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
    build_openai_toolkit,
    decide_tool_approval,
)
from app.harness.tools.triggers import trigger_create, trigger_fire, trigger_list


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

    def test_trigger_tools_are_registered_and_schema_validates(self) -> None:
        """触发器工具应进入模型可见工具表，并通过 Pydantic schema 拒绝未知字段。"""
        tools_payload, tool_map = build_openai_toolkit()
        tool_names = {item["function"]["name"] for item in tools_payload}

        self.assertIn("trigger_list", tool_names)
        self.assertIn("trigger_create", tool_names)
        self.assertIn("trigger_fire", tool_names)
        self.assertIn("trigger_id", tool_map["trigger_fire"].parameters["properties"])

        create_spec = _tool_to_spec(trigger_create)
        self.assertIn("name", create_spec.parameters["required"])
        self.assertIn("task_template", create_spec.parameters["required"])
        with self.assertRaisesRegex(ValueError, "extra"):
            create_spec.invoke({"name": "n", "task_template": "t", "extra": True}, None)

    def test_trigger_tool_approval_policy(self) -> None:
        """触发器只读工具自动放行，写入和 fire 默认要求审批。"""
        settings = SimpleNamespace(agent_tool_approval_mode="rule")

        with patch("app.harness.tools.tool.get_settings", return_value=settings):
            read_decision = decide_tool_approval(tool_name="trigger_list", tool_args={})
            write_decision = decide_tool_approval(tool_name="trigger_create", tool_args={})
            fire_decision = decide_tool_approval(tool_name="trigger_fire", tool_args={})

        self.assertFalse(read_decision.require_approval)
        self.assertEqual(read_decision.mode, "trigger:read")
        self.assertTrue(write_decision.require_approval)
        self.assertEqual(write_decision.mode, "trigger:write")
        self.assertEqual(write_decision.risk_level, "medium")
        self.assertTrue(fire_decision.require_approval)
        self.assertEqual(fire_decision.risk_level, "high")

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
