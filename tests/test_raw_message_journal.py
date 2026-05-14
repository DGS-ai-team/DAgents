"""`app.harness.history.raw_message_journal` 单测：开关、空 session、追加与 JSONL 行结构。"""

from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from app.context.models import OpenAIConversationContext
from app.harness.history import raw_message_journal as journal


class RecordRawOpenaiMessageAppendTests(unittest.TestCase):
    """`record_raw_openai_message_append`：配置与 session 边界。"""

    def test_disabled_or_empty_session_skips_write(self) -> None:
        """关闭开关或 `session_id` 为空时不写文件。"""
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            with patch.object(journal, "get_settings") as gs:
                gs.return_value.agent_raw_message_history_enabled = False
                journal.record_raw_openai_message_append("s1", {"role": "user", "content": "x"})
            self.assertEqual(list(root.rglob("*.jsonl")), [])

        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            with patch.object(journal, "get_settings") as gs, patch(
                "app.config.runtime_layout.resolve_runtime_root", return_value=root
            ):
                gs.return_value.agent_raw_message_history_enabled = True
                journal.record_raw_openai_message_append("", {"role": "user", "content": "x"})
            self.assertEqual(list((root / ".runtime" / "history").rglob("*.jsonl")), [])


class AppendOpenaiMessageWithJournalTests(unittest.TestCase):
    """`append_openai_message_with_journal`：列表追加与 JSONL 字段。"""

    def test_append_writes_jsonl_line_with_message_snapshot(self) -> None:
        """应在配置目录下生成 JSONL，行内含 `recorded_at` 与 `message` 快照。"""
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            hist = root / ".runtime" / "history"
            with patch.object(journal, "get_settings") as gs, patch(
                "app.config.runtime_layout.resolve_runtime_root", return_value=root
            ):
                gs.return_value.agent_raw_message_history_enabled = True
                ctx = OpenAIConversationContext(session_id="sess-a", messages=[])
                journal.append_openai_message_with_journal(ctx, {"role": "user", "content": "hello"})
            self.assertEqual(len(ctx.messages), 1)
            files = list(hist.glob("*.jsonl"))
            self.assertEqual(len(files), 1)
            line = files[0].read_text(encoding="utf-8").strip()
            obj = json.loads(line)
            self.assertIn("recorded_at", obj)
            self.assertEqual(obj["message"]["content"], "hello")


class InsertOpenaiMessageWithJournalTests(unittest.TestCase):
    """`insert_openai_message_with_journal`：插入顺序与 JSONL 追加顺序解耦。"""

    def test_insert_prepends_message_and_still_appends_journal(self) -> None:
        """`insert(0, ...)` 后列表首条为新消息；JSONL 仍按调用顺序追加一行。"""
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            hist = root / ".runtime" / "history"
            with patch.object(journal, "get_settings") as gs, patch(
                "app.config.runtime_layout.resolve_runtime_root", return_value=root
            ):
                gs.return_value.agent_raw_message_history_enabled = True
                ctx = OpenAIConversationContext(session_id="sess-b", messages=[{"role": "user", "content": "old"}])
                journal.insert_openai_message_with_journal(ctx, 0, {"role": "system", "content": "sys"})
            self.assertEqual(ctx.messages[0]["role"], "system")
            self.assertEqual(ctx.messages[1]["content"], "old")
            files = list(hist.glob("*.jsonl"))
            self.assertEqual(len(files), 1)
            nlines = len([ln for ln in files[0].read_text(encoding="utf-8").splitlines() if ln.strip()])
            self.assertEqual(nlines, 1)


if __name__ == "__main__":
    unittest.main()
