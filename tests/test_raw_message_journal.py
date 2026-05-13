"""原始消息 JSONL 落盘记录单元测试。"""

from __future__ import annotations

import json
import os
import sys
import unittest
import uuid
from datetime import datetime
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import patch

_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_ROOT))

from app.config.settings import get_settings  # noqa: E402
from app.context.models import OpenAIConversationContext  # noqa: E402
from app.harness.history.raw_message_journal import (  # noqa: E402
    append_openai_message_with_journal,
    record_raw_openai_message_append,
)


class RawMessageJournalTestCase(unittest.TestCase):
    """验证 JSONL 记录写入路径与开关。"""

    _ENV_KEYS = ("AGENT_RAW_MESSAGE_HISTORY_ENABLED", "AGENT_RAW_MESSAGE_HISTORY_DIR")

    def tearDown(self) -> None:
        for key in self._ENV_KEYS:
            os.environ.pop(key, None)
        get_settings(reload=True)

    def test_append_writes_jsonl_line(self) -> None:
        """启用开关时追加一行 JSONL，且内容为插入快照。"""
        os.environ["AGENT_RAW_MESSAGE_HISTORY_ENABLED"] = "true"
        os.environ["AGENT_RAW_MESSAGE_HISTORY_DIR"] = "hist"
        get_settings(reload=True)
        day = datetime.now().strftime("%Y%m%d")
        with TemporaryDirectory() as raw:
            root = Path(raw)
            with patch(
                "app.harness.history.raw_message_journal.resolve_runtime_root",
                return_value=root,
            ):
                ctx = OpenAIConversationContext(session_id="sess-a", messages=[])
                msg = {"role": "user", "content": "hello"}
                append_openai_message_with_journal(ctx, msg)
                msg["content"] = "mutated"
            path = root / "hist" / f"sess-a_{day}.jsonl"
            self.assertTrue(path.is_file())
            line = path.read_text(encoding="utf-8").strip().splitlines()[0]
            row = json.loads(line)
            self.assertEqual(row["message"]["content"], "hello")
            self.assertIn("recorded_at", row)
            self.assertEqual(ctx.messages[0]["content"], "mutated")

    def test_disabled_skips_file(self) -> None:
        """关闭开关时不创建文件。"""
        os.environ["AGENT_RAW_MESSAGE_HISTORY_ENABLED"] = "false"
        get_settings(reload=True)
        with TemporaryDirectory() as raw:
            root = Path(raw)
            with patch(
                "app.harness.history.raw_message_journal.resolve_runtime_root",
                return_value=root,
            ):
                record_raw_openai_message_append(f"sid-{uuid.uuid4().hex}", {"role": "user", "content": "x"})
            self.assertEqual(list(root.rglob("*.jsonl")), [])

    def test_empty_session_skips(self) -> None:
        """session_id 为空时不写。"""
        os.environ["AGENT_RAW_MESSAGE_HISTORY_ENABLED"] = "true"
        get_settings(reload=True)
        with TemporaryDirectory() as raw:
            root = Path(raw)
            with patch(
                "app.harness.history.raw_message_journal.resolve_runtime_root",
                return_value=root,
            ):
                record_raw_openai_message_append("", {"role": "user", "content": "x"})
            self.assertEqual(list(root.rglob("*.jsonl")), [])


if __name__ == "__main__":
    unittest.main()
