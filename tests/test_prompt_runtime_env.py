from __future__ import annotations

import sys
import unittest
from pathlib import Path
from unittest.mock import patch

_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_ROOT))

from app.config.host_snapshot import HostSnapshot  # noqa: E402
from app.config.settings import get_settings  # noqa: E402
from app.context.models import OpenAIConversationContext  # noqa: E402
from app.core.main_agent import prompt as prompt_mod  # noqa: E402


class PromptRuntimeEnvTestCase(unittest.TestCase):
    def tearDown(self) -> None:
        prompt_mod._prompt_context_file_cache.clear()

    def test_system_prompt_includes_snapshot_os_and_user(self) -> None:
        """`get_system_prompt` 使用 `get_host_snapshot`，正文含 OS 类别与用户信息。"""
        snap = HostSnapshot(
            captured_at_unix=0.0,
            os_kind="linux",
            sys_platform="linux",
            platform_system="Linux",
            platform_release="test",
            machine="aarch64",
            login_name="alice",
            effective_uid=1000,
            effective_gid=1000,
        )
        with patch.object(prompt_mod, "get_host_snapshot", return_value=snap):
            text = prompt_mod.get_system_prompt(OpenAIConversationContext())
        self.assertIn("## 以下是当前运行环境：", text)
        self.assertIn("操作系统类别：`linux`", text)
        self.assertIn("`alice`", text)
        self.assertIn("`1000` / `1000`", text)
        self.assertIn("## 会话原始消息审计（JSONL）", text)
        self.assertIn("recorded_at", text)

    def test_system_prompt_non_posix_uid_line(self) -> None:
        """非 POSIX 快照下 UID/GID 行写明不适用。"""
        snap = HostSnapshot(
            captured_at_unix=0.0,
            os_kind="windows",
            sys_platform="win32",
            platform_system="Windows",
            platform_release="11",
            machine="AMD64",
            login_name="bob",
            effective_uid=None,
            effective_gid=None,
        )
        with patch.object(prompt_mod, "get_host_snapshot", return_value=snap):
            text = prompt_mod.get_system_prompt(OpenAIConversationContext())
        self.assertIn("操作系统类别：`windows`", text)
        self.assertIn("`bob`", text)
        self.assertIn("不适用（当前运行时非 POSIX 或未提供）", text)

    def test_system_prompt_omits_raw_journal_when_disabled(self) -> None:
        """关闭原始消息审计配置时不注入 JSONL 说明段。"""
        snap = HostSnapshot(
            captured_at_unix=0.0,
            os_kind="linux",
            sys_platform="linux",
            platform_system="Linux",
            platform_release="test",
            machine="aarch64",
            login_name="alice",
            effective_uid=1000,
            effective_gid=1000,
        )
        disabled = get_settings().model_copy(update={"agent_raw_message_history_enabled": False})
        with patch.object(prompt_mod, "get_host_snapshot", return_value=snap):
            with patch.object(prompt_mod, "get_settings", return_value=disabled):
                text = prompt_mod.get_system_prompt(OpenAIConversationContext())
        self.assertNotIn("## 会话原始消息审计（JSONL）", text)


if __name__ == "__main__":
    unittest.main()
