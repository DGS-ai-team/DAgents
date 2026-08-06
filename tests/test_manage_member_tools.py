"""Member 工具权威目录与 OpenAI schema 对齐测试。"""

from __future__ import annotations

import unittest

from manage.workgroup.member_tools import (
    MEMBER_EXECUTABLE_TOOL_NAMES,
    default_allow_tool_names,
    member_openai_tools,
    member_tool_catalog,
    side_effect_for_tool,
)


class MemberToolCatalogTests(unittest.TestCase):
    def test_catalog_covers_executable_names(self) -> None:
        catalog = member_tool_catalog()
        ids = [t["id"] for t in catalog["tools"]]
        self.assertEqual(ids, MEMBER_EXECUTABLE_TOOL_NAMES)
        self.assertEqual(catalog["default_allow_names"], default_allow_tool_names())
        # v0.9.1：默认仅 fs；bash 在目录中但 default=False
        self.assertTrue(set(default_allow_tool_names()).issubset(set(ids)))
        self.assertNotIn("bash_run", default_allow_tool_names())
        self.assertIn("read_file", default_allow_tool_names())
        self.assertIn("search_replace", default_allow_tool_names())

    def test_openai_tools_for_all_names(self) -> None:
        tools = member_openai_tools(MEMBER_EXECUTABLE_TOOL_NAMES)
        self.assertEqual(len(tools), len(MEMBER_EXECUTABLE_TOOL_NAMES))
        names = [t["function"]["name"] for t in tools]
        self.assertEqual(names, MEMBER_EXECUTABLE_TOOL_NAMES)

    def test_side_effects(self) -> None:
        self.assertEqual(side_effect_for_tool("read_file"), "fs_read")
        self.assertEqual(side_effect_for_tool("search_replace"), "fs_write")
        self.assertEqual(side_effect_for_tool("bash_run"), "shell")
        self.assertEqual(side_effect_for_tool("unknown_x"), "other")

    def test_unknown_names_skipped(self) -> None:
        tools = member_openai_tools(["read_file", "not_a_tool", "bash_run"])
        self.assertEqual([t["function"]["name"] for t in tools], ["read_file", "bash_run"])


if __name__ == "__main__":
    unittest.main()
