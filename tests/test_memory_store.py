"""`SqliteMessageStore` 会话内容持久化测试。"""

from __future__ import annotations

import tempfile
import unittest
from pathlib import Path

from app.context.models import ConversationContext
from app.harness.memory.store import SqliteMessageStore


class SqliteMessageStoreConversationContentTests(unittest.TestCase):
    """覆盖会话上下文整包保存与恢复。"""

    def test_save_and_load_preserves_loaded_skills(self) -> None:
        """已加载 skills 应随会话上下文一起持久化，避免恢复后 prompt 状态丢失。"""
        with tempfile.TemporaryDirectory() as tmp:
            store = SqliteMessageStore(Path(tmp) / "session.sqlite3")
            payload = ConversationContext(
                openai_messages=[{"role": "user", "content": "hi"}],
                loaded_skills=[
                    {"skill_name": "debugging", "description": "systematic debugging"},
                    {"skill_name": "planning", "description": ""},
                ],
            )

            store.save_conversation_content("s1", payload)
            restored = store.load_conversation_content("s1")

            self.assertEqual(restored.loaded_skills, payload.loaded_skills)

    def test_append_message_keeps_loaded_skills(self) -> None:
        """追加展示历史时不应覆盖已持久化的 loaded_skills。"""
        with tempfile.TemporaryDirectory() as tmp:
            store = SqliteMessageStore(Path(tmp) / "session.sqlite3")
            payload = ConversationContext(
                openai_messages=[{"role": "user", "content": "hi"}],
                loaded_skills=[{"skill_name": "debugging", "description": "systematic debugging"}],
            )

            store.save_conversation_content("s1", payload)
            store.append_message("s1", role="assistant", content="ok")
            restored = store.load_conversation_content("s1")

            self.assertEqual(restored.loaded_skills, payload.loaded_skills)


if __name__ == "__main__":
    unittest.main()
