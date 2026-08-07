"""Member 工具权威目录与 OpenAI schema 对齐测试。"""

from __future__ import annotations

import unittest
from types import SimpleNamespace

from manage.workgroup.member_tools import (
    MEMBER_EXECUTABLE_TOOL_NAMES,
    build_member_system_prompt,
    default_allow_tool_names,
    host_env_from_registry,
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


class MemberSystemPromptTests(unittest.TestCase):
    def test_includes_static_env_workspace_user_info_and_soul(self) -> None:
        prompt = build_member_system_prompt(
            soul_md="资料员",
            user_md="这段旧 user.md 不应再出现",
            host_env={
                "home_node_id": "node-a",
                "os_kind": "linux",
                "sys_platform": "linux",
                "platform_release": "6.1.0",
                "machine": "x86_64",
                "login_name": "dagents",
                "host_ips": "10.0.0.8",
                "node_version": "0.8.5",
                "node_name": "desk-a",
                "owner": "alice@example.com",
                "team": "research",
                "discovery_group": "lab-1",
            },
            member_id="mb_01h00000000000000000000001",
            display_name="资料员",
            workgroup_id="wg_01h00000000000000000000001",
            workgroup_name="调研组",
            created_by_node_id="node-b",
            workspace_path="/tmp/member-ws",
        )
        self.assertIn("## 最高优先级规则", prompt)
        self.assertIn("## 运行环境", prompt)
        self.assertIn("操作系统类别：`linux`", prompt)
        self.assertIn("## 工作区目录", prompt)
        self.assertIn("`/tmp/member-ws`", prompt)
        self.assertIn("## 以下是你的设定：", prompt)
        self.assertIn("资料员", prompt)
        self.assertIn("## 以下是用户信息：", prompt)
        self.assertIn("工作组", prompt)
        self.assertIn("不一定", prompt)
        self.assertIn("所属 Home Node ID：`node-a`", prompt)
        self.assertIn("Node 所属者（owner）：alice@example.com", prompt)
        self.assertIn("Node 所属团队（team）：research", prompt)
        self.assertIn("当前工作组：调研组", prompt)
        self.assertIn("工作组创建者 Node：`node-b`", prompt)
        self.assertNotIn("## User", prompt)
        self.assertNotIn("这段旧 user.md 不应再出现", prompt)
        self.assertNotIn("You are a Workgroup Member agent", prompt)

    def test_host_env_from_registry(self) -> None:
        store = SimpleNamespace(
            get=lambda node_id: SimpleNamespace(
                host_ips="127.0.0.1",
                version="0.9.0",
                name="desk",
                owner="bob",
                team="ops",
                discovery_group=["g1", "g2"],
                metadata={
                    "host_info": {
                        "os_kind": "windows",
                        "sys_platform": "windows",
                        "platform_release": "10",
                        "machine": "AMD64",
                        "login_name": "Alice",
                    }
                },
            )
            if node_id == "node-win"
            else None
        )
        env = host_env_from_registry(store, "node-win")
        self.assertEqual(env["os_kind"], "windows")
        self.assertEqual(env["login_name"], "Alice")
        self.assertEqual(env["host_ips"], "127.0.0.1")
        self.assertEqual(env["node_version"], "0.9.0")
        self.assertEqual(env["owner"], "bob")
        self.assertEqual(env["team"], "ops")
        self.assertEqual(env["discovery_group"], "g1, g2")
        missing = host_env_from_registry(store, "missing")
        self.assertEqual(missing["home_node_id"], "missing")
        self.assertEqual(missing.get("os_kind", ""), "")


if __name__ == "__main__":
    unittest.main()
