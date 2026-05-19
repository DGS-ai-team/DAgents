"""主 Agent system prompt 分层与缓存行为测试。"""

from __future__ import annotations

import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock, patch

from app.config.host_snapshot import HostSnapshot
from app.context.models import OpenAIConversationContext
from app.core.main_agent import prompt as prompt_module


class MainAgentPromptTests(unittest.TestCase):
    def setUp(self) -> None:
        prompt_module._stable_system_prompt_cache.clear()

    def tearDown(self) -> None:
        prompt_module._stable_system_prompt_cache.clear()

    def _settings(
        self,
        *,
        skills_enabled: bool = True,
        skills_allow_create: bool = False,
        raw_history_enabled: bool = False,
        max_skills: int = 3,
    ) -> SimpleNamespace:
        return SimpleNamespace(
            agent_skills_enabled=skills_enabled,
            agent_skills_allow_create=skills_allow_create,
            agent_raw_message_history_enabled=raw_history_enabled,
            agent_skills_max_in_prompt=max_skills,
        )

    def _host_snapshot(self) -> HostSnapshot:
        return HostSnapshot(
            captured_at_unix=1.0,
            os_kind="darwin",
            sys_platform="darwin",
            platform_system="Darwin",
            platform_release="24.0.0",
            machine="arm64",
            login_name="unit-user",
            effective_uid=501,
            effective_gid=20,
        )

    def test_system_prompt_keeps_volatile_sections_after_stable_prefix(self) -> None:
        """侧车、已加载 skills、custom 与 session 后缀应按稳定到易变顺序拼接。"""

        def _context_reader(filename: str) -> str:
            return {
                prompt_module.SOUL_MD: "SOUL-CONTEXT",
                prompt_module.USER_MD: "USER-CONTEXT",
                prompt_module.CUSTOM_MD: "CUSTOM-CONTEXT",
            }.get(filename, "")

        ctx = OpenAIConversationContext(
            session_id="session-prompt-order",
            loaded_skills=[{"skill_name": "debugging", "description": "debug"}],
        )

        with patch.object(prompt_module, "get_settings", return_value=self._settings()):
            with patch.object(prompt_module, "get_host_snapshot", return_value=self._host_snapshot()):
                with patch.object(prompt_module, "list_enabled_skill_metadata", return_value=[{"name": "debugging"}]):
                    with patch.object(prompt_module, "render_skill_metadata_prompt", return_value="SKILL-METADATA"):
                        with patch.object(prompt_module, "_read_prompt_context_markdown", side_effect=_context_reader):
                            with patch.object(prompt_module, "_read_long_term_memory", return_value="LONG-TERM-MEMORY"):
                                with patch.object(prompt_module, "select_skill_by_name", return_value=object()):
                                    with patch.object(prompt_module, "render_skills_prompt", return_value="LOADED-SKILL-BODY"):
                                        text = prompt_module.get_system_prompt(ctx)

        ordered_markers = [
            "## 最高优先级规则",
            "SKILL-METADATA",
            "## 以下是当前运行环境",
            "## `.runtime` 工作目录约定",
            "SOUL-CONTEXT",
            "USER-CONTEXT",
            "LONG-TERM-MEMORY",
            "LOADED-SKILL-BODY",
            "CUSTOM-CONTEXT",
            "session_id: session-prompt-order",
        ]
        positions = [text.index(marker) for marker in ordered_markers]
        self.assertEqual(positions, sorted(positions))

    def test_loaded_skills_section_is_session_dependent(self) -> None:
        """稳定前缀不应包含已加载 skill 正文，不同 session 可注入不同 loaded_skills。"""
        stable_settings = self._settings()
        ctx_without_skill = OpenAIConversationContext(session_id="s-no-skill")
        ctx_with_skill = OpenAIConversationContext(
            session_id="s-with-skill",
            loaded_skills=[{"skill_name": "planning", "description": "plan"}],
        )

        def _select_skill(name: str) -> object | None:
            return object() if name == "planning" else None

        def _render_skills(skills: list[object]) -> str:
            return "PLANNING-SKILL-BODY" if skills else ""

        with patch.object(prompt_module, "get_settings", return_value=stable_settings):
            with patch.object(prompt_module, "get_host_snapshot", return_value=self._host_snapshot()):
                with patch.object(prompt_module, "list_enabled_skill_metadata", return_value=[]):
                    with patch.object(prompt_module, "render_skill_metadata_prompt", return_value=""):
                        with patch.object(prompt_module, "_read_prompt_context_markdown", return_value=""):
                            with patch.object(prompt_module, "_read_long_term_memory", return_value=""):
                                with patch.object(prompt_module, "select_skill_by_name", side_effect=_select_skill):
                                    with patch.object(prompt_module, "render_skills_prompt", side_effect=_render_skills):
                                        without_skill = prompt_module.get_system_prompt(ctx_without_skill)
                                        with_skill = prompt_module.get_system_prompt(ctx_with_skill)

        self.assertNotIn("PLANNING-SKILL-BODY", without_skill)
        self.assertIn("PLANNING-SKILL-BODY", with_skill)
        self.assertLess(with_skill.index("PLANNING-SKILL-BODY"), with_skill.index("session_id: s-with-skill"))

    def test_stable_prompt_cache_reuses_built_prefix_for_same_key(self) -> None:
        """相同稳定配置下重复构造应复用已生成的稳定前缀正文。"""
        workspace_formatter = Mock(return_value="RUNTIME-WORKSPACE")

        with patch.object(prompt_module, "get_settings", return_value=self._settings()):
            with patch.object(prompt_module, "get_host_snapshot", return_value=self._host_snapshot()):
                with patch.object(prompt_module, "list_enabled_skill_metadata", return_value=[{"name": "one"}]):
                    with patch.object(prompt_module, "render_skill_metadata_prompt", return_value="META-ONE"):
                        with patch.object(prompt_module, "_format_runtime_workspace_section", workspace_formatter):
                            first = prompt_module.build_stable_system_prompt()
                            second = prompt_module.build_stable_system_prompt()

        self.assertEqual(first, second)
        workspace_formatter.assert_called_once()

    def test_stable_prompt_cache_key_tracks_skill_creation_rules(self) -> None:
        """允许创建 skills 的开关会改变稳定前缀缓存键与输出内容。"""
        with patch.object(prompt_module, "get_host_snapshot", return_value=self._host_snapshot()):
            with patch.object(prompt_module, "list_enabled_skill_metadata", return_value=[]):
                with patch.object(prompt_module, "render_skill_metadata_prompt", return_value=""):
                    with patch.object(prompt_module, "skills_dir", return_value=Path("/tmp/unit-skills")):
                        with patch.object(prompt_module, "get_settings", return_value=self._settings(skills_allow_create=False)):
                            disabled = prompt_module.build_stable_system_prompt()
                        with patch.object(prompt_module, "get_settings", return_value=self._settings(skills_allow_create=True)):
                            enabled = prompt_module.build_stable_system_prompt()

        self.assertNotIn("你可以自主创建 skills", disabled)
        self.assertIn("你可以自主创建 skills", enabled)
        self.assertIn("/tmp/unit-skills", enabled)


if __name__ == "__main__":
    unittest.main()
