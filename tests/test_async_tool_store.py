"""`AsyncToolResultStore.submit_coroutine` 对 `client_id` 的约束单测。"""

from __future__ import annotations

import asyncio
import unittest
from types import SimpleNamespace
from unittest.mock import patch

from app.context.models import OpenAIConversationContext
from app.harness.tools.async_store import AsyncToolResultStore
from app.harness.tools.tool import tool


class AsyncToolSubmitClientIdTests(unittest.IsolatedAsyncioTestCase):
    """异步工具提交须携带非空 `client_id`，以便终态回灌路由 SSE。"""

    async def test_submit_coroutine_rejects_blank_client_id(self) -> None:
        """`client_id` 仅空白时应在建 task 前抛 `ValueError`。"""
        store = AsyncToolResultStore()

        async def _coro() -> str:
            return "ok"

        with self.assertRaises(ValueError):
            store.submit_coroutine(session_id="s1", client_id="", tool_name="t", coroutine_obj=_coro())
        with self.assertRaises(ValueError):
            store.submit_coroutine(session_id="s1", client_id="  \t  ", tool_name="t", coroutine_obj=_coro())

    async def test_submit_coroutine_accepts_client_id_and_completes(self) -> None:
        """非空 `client_id` 写入 `AsyncToolJob` 且任务可跑至终态（无 message_queue 发送器）。"""
        store = AsyncToolResultStore()

        async def _coro() -> str:
            return "result"

        job = store.submit_coroutine(session_id="s2", client_id="client-z", tool_name="demo", coroutine_obj=_coro())
        self.assertEqual(job.client_id, "client-z")
        await asyncio.sleep(0.05)
        snap = store.get_job(job.job_id)
        self.assertIsNotNone(snap)
        assert snap is not None
        self.assertEqual(snap.status, "succeeded")
        self.assertEqual(snap.client_id, "client-z")


class AsyncToolRoutingContextTests(unittest.TestCase):
    """异步工具装饰器应优先使用当前入站消息的 client_id。"""

    def test_async_tool_prefers_active_client_id_over_last_seen_client_id(self) -> None:
        """active_client_id 存在时，后台任务路由不应误用旧的 sse_client_id。"""
        captured: dict[str, object] = {}

        class Store:
            def submit_coroutine(self, *, session_id, client_id, tool_name, coroutine_obj):
                captured.update(session_id=session_id, client_id=client_id, tool_name=tool_name)
                coroutine_obj.close()
                return SimpleNamespace(job_id="job-1")

        @tool("async_demo")
        async def async_demo(*, context: OpenAIConversationContext) -> str:
            del context
            return "ok"

        ctx = OpenAIConversationContext(
            session_id="s1",
            active_client_id="current-client",
            sse_client_id="old-client",
        )

        with patch("app.harness.tools.tool.get_async_tool_result_store", return_value=Store()):
            result = async_demo(context=ctx)

        self.assertIn("job-1", result)
        self.assertEqual(captured["session_id"], "s1")
        self.assertEqual(captured["client_id"], "current-client")
        self.assertEqual(captured["tool_name"], "async_demo")

    def test_async_tool_falls_back_to_last_seen_client_id(self) -> None:
        """无 active_client_id 时保留最近 SSE 通道路由，兼容内部回灌路径。"""
        captured: dict[str, object] = {}

        class Store:
            def submit_coroutine(self, *, session_id, client_id, tool_name, coroutine_obj):
                captured.update(session_id=session_id, client_id=client_id, tool_name=tool_name)
                coroutine_obj.close()
                return SimpleNamespace(job_id="job-2")

        @tool("async_demo_fallback")
        async def async_demo_fallback(*, context: OpenAIConversationContext) -> str:
            del context
            return "ok"

        ctx = OpenAIConversationContext(session_id="s2", active_client_id="", sse_client_id="last-client")

        with patch("app.harness.tools.tool.get_async_tool_result_store", return_value=Store()):
            result = async_demo_fallback(context=ctx)

        self.assertIn("job-2", result)
        self.assertEqual(captured["client_id"], "last-client")


if __name__ == "__main__":
    unittest.main()
