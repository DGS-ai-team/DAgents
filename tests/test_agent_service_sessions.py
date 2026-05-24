"""AgentService session 列表与 sqlite 删除单测。"""

from __future__ import annotations

import asyncio
import tempfile
import unittest
from pathlib import Path
from unittest.mock import AsyncMock, MagicMock, patch

from tests.test_support.stub_settings import settings_namespace

try:
    from app.harness.memory.store import SqliteMessageStore
    from app.harness.service.agent_service import AgentService
except ImportError as exc:  # pragma: no cover
    AgentService = None  # type: ignore[misc, assignment]
    SqliteMessageStore = None  # type: ignore[misc, assignment]
    _SKIP = f"AgentService 依赖链未就绪（{exc!r}）；请执行 pip install -r requirements.txt"
else:
    _SKIP = ""


@unittest.skipIf(AgentService is None or SqliteMessageStore is None, _SKIP)
class AgentServiceSessionAdminTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self) -> None:
        self._orch = MagicMock()
        self._orch.handle_message = AsyncMock()
        self._orch.cancel_all_summary_tasks = AsyncMock()
        self._orch.cancel_session_summary_task = AsyncMock()
        self._async_store_mock = MagicMock()
        self._tmpdir = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmpdir.cleanup)
        self._store = SqliteMessageStore(Path(self._tmpdir.name) / "session.sqlite3")

    async def _make_service(self) -> AgentService:
        def _factory(*_a: object, **_k: object) -> MagicMock:
            return self._orch

        patches = [
            patch(
                "app.harness.service.agent_service.get_settings",
                return_value=settings_namespace(agent_session_store_enabled=True),
            ),
            patch("app.harness.service.agent_service.get_model_config", return_value={"model": "unit"}),
            patch("app.harness.service.agent_service.MainAgentTurnOrchestrator", side_effect=_factory),
            patch("app.harness.service.agent_service.refresh_session_context_metrics"),
            patch("app.harness.service.agent_service.refresh_session_queue_metrics"),
            patch("app.core.main_agent.runtime_openai.get_openai_client", return_value=MagicMock()),
            patch(
                "app.harness.service.agent_service.get_async_tool_result_store",
                return_value=self._async_store_mock,
            ),
            patch("app.harness.service.agent_service.session_sqlite_path", return_value=Path(self._tmpdir.name) / "unused.sqlite3"),
        ]
        for item in patches:
            item.start()
            self.addCleanup(item.stop)
        assert AgentService is not None
        return AgentService(max_queue_size=0, message_store=self._store)

    async def test_list_sessions_marks_active_and_persisted(self) -> None:
        svc = await self._make_service()
        await svc.start()
        await svc.create_session("active-s")
        await svc.submit_message(
            session_id="active-s",
            content="first user message",
            source="test",
            priority="human",
            client_id="cli-1",
        )
        self._store.save_conversation_content(
            "persisted-only",
            __import__("app.context.models", fromlist=["ConversationContext"]).ConversationContext(),
            first_request_message="old chat",
        )
        await asyncio.sleep(0.05)

        data = await svc.list_sessions()
        active_ids = {row["session_id"] for row in data["active"]}
        self.assertIn("active-s", active_ids)
        persisted = {row["session_id"]: row for row in data["persisted"]}
        self.assertIn("active-s", persisted)
        self.assertTrue(persisted["active-s"]["in_queue"])
        self.assertIn("persisted-only", persisted)
        self.assertFalse(persisted["persisted-only"]["in_queue"])
        self.assertEqual(persisted["persisted-only"]["first_request_message"], "old chat")
        await svc.stop()

    async def test_delete_persisted_session_rejects_active_queue(self) -> None:
        svc = await self._make_service()
        await svc.start()
        await svc.create_session("active-s")
        self._store.save_conversation_content(
            "active-s",
            __import__("app.context.models", fromlist=["ConversationContext"]).ConversationContext(),
            first_request_message="still active",
        )

        with self.assertRaises(RuntimeError):
            await svc.delete_persisted_session("active-s")

        self.assertTrue(await svc.delete_persisted_session("missing-s") is False)
        await svc.stop()

    async def test_delete_persisted_session_removes_sqlite_row(self) -> None:
        svc = await self._make_service()
        self._store.save_conversation_content(
            "gone-s",
            __import__("app.context.models", fromlist=["ConversationContext"]).ConversationContext(),
            first_request_message="bye",
        )

        deleted = await svc.delete_persisted_session("gone-s")
        self.assertTrue(deleted)
        self.assertEqual(self._store.list_session_summaries(), [])


if __name__ == "__main__":
    unittest.main()
