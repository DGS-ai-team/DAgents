"""成员工具展示文案的纯函数测试。

成员真正可调用的工具、参数和执行策略由 Node Agent 自己注册；Manage 只
负责把事件中的工具名转换成面向用户的简短文案，不再维护第二份工具目录。
"""

from __future__ import annotations

import unittest

from manage.workgroup.member_tools import call_purpose_from_arguments, purpose_for_tool


class MemberToolPurposeTests(unittest.TestCase):
    def test_known_tools_have_user_facing_purpose(self) -> None:
        self.assertEqual(purpose_for_tool("read_file"), "读取文件")
        self.assertEqual(purpose_for_tool("bash_run"), "执行命令")
        self.assertEqual(purpose_for_tool("unknown_x"), "执行成员工具")

    def test_call_purpose_uses_explicit_argument_when_present(self) -> None:
        self.assertEqual(
            call_purpose_from_arguments(
                '{"call_purpose":"查找 README 的标题"}',
                "读取文件",
            ),
            "查找 README 的标题",
        )

    def test_invalid_or_missing_argument_falls_back_to_tool_purpose(self) -> None:
        self.assertEqual(call_purpose_from_arguments('{"path":"README"}', "读取文件"), "读取文件")
        self.assertEqual(call_purpose_from_arguments("not-json", "执行命令"), "执行命令")


if __name__ == "__main__":
    unittest.main()
