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

    def test_first_request_message_is_saved_once(self) -> None:
        """首条请求消息应写入独立列且不会被后续覆盖。"""
        with tempfile.TemporaryDirectory() as tmp:
            store = SqliteMessageStore(Path(tmp) / "session.sqlite3")
            payload = ConversationContext(openai_messages=[{"role": "user", "content": "hello world"}])

            store.save_conversation_content("s1", payload, first_request_message="hello world")
            store.save_conversation_content("s1", payload, first_request_message="later message")

            summaries = store.list_session_summaries()
            self.assertEqual(len(summaries), 1)
            self.assertEqual(summaries[0]["session_id"], "s1")
            self.assertEqual(summaries[0]["first_request_message"], "hello world")

    def test_delete_session_if_exists(self) -> None:
        """delete_session_if_exists 应删除 sqlite 行并返回是否命中。"""
        with tempfile.TemporaryDirectory() as tmp:
            store = SqliteMessageStore(Path(tmp) / "session.sqlite3")
            store.save_conversation_content("s1", ConversationContext())

            self.assertTrue(store.delete_session_if_exists("s1"))
            self.assertFalse(store.delete_session_if_exists("s1"))
            self.assertEqual(store.list_session_summaries(), [])


if __name__ == "__main__":
    unittest.main()
