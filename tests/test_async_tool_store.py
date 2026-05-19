"""`AsyncToolResultStore.submit_coroutine` 对 `client_id` 的约束单测。"""

from __future__ import annotations

import asyncio
import unittest

from app.harness.tools.async_store import AsyncToolResultStore


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


if __name__ == "__main__":
    unittest.main()
