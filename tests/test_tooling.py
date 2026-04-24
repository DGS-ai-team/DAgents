from __future__ import annotations

import asyncio
import re
import sys
import unittest
from pathlib import Path

_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_ROOT))

from app.context.models import OpenAIConversationContext  # noqa: E402
from app.harness.tools.async_store import AsyncToolResultStore, get_async_tool_result_store  # noqa: E402
from app.harness.tools.tool import tool  # noqa: E402


class ToolingDecoratorTestCase(unittest.TestCase):
    def test_tool_name_fallback_to_function_name(self) -> None:
        @tool("")
        def demo_tool() -> str:
            """demo"""

            return "ok"

        self.assertEqual(getattr(demo_tool, "name"), "demo_tool")

    def test_tool_explicit_name(self) -> None:
        @tool("custom_name")
        def another_tool() -> str:
            """desc"""

            return "ok"

        self.assertEqual(getattr(another_tool, "name"), "custom_name")

    def test_tool_no_args_decorator(self) -> None:
        @tool
        def plain_tool() -> str:
            """plain"""

            return "ok"

        self.assertEqual(getattr(plain_tool, "name"), "plain_tool")

    def test_async_tool_submit_background_job_and_return_ack(self) -> None:
        @tool("async_demo")
        async def async_demo(value: str) -> str:
            """异步测试工具。"""
            await asyncio.sleep(0.01)
            return f"done:{value}"

        async def _run() -> None:
            ack = async_demo("x", context=OpenAIConversationContext(session_id="s-tooling"))
            self.assertIn("工具 async_demo 已执行并转为后台任务", ack)
            matched = re.search(r"job_id=([a-f0-9-]+)", ack)
            self.assertIsNotNone(matched)
            job_id = str(matched.group(1))
            store = get_async_tool_result_store()
            # 后台任务是异步完成的，轮询直到进入终态。
            for _ in range(100):
                job = store.get_job(job_id)
                if job is not None and job.status in {"succeeded", "failed", "cancelled"}:
                    break
                await asyncio.sleep(0.01)
            job = store.get_job(job_id)
            self.assertIsNotNone(job)
            self.assertEqual(job.status, "succeeded")
            self.assertEqual(job.session_id, "s-tooling")
            self.assertEqual(job.result_text, "done:x")

        asyncio.run(_run())

    def test_async_store_notify_completed_pushes_async_tool_result_payload(self) -> None:
        store = AsyncToolResultStore()
        sent: list[tuple[str, dict]] = []

        def _sender(session_id: str, payload: dict) -> None:
            sent.append((session_id, payload))

        async def _run() -> None:
            store.register_message_queue_sender(_sender)

            async def _demo() -> str:
                await asyncio.sleep(0.01)
                return "done-payload"

            job = store.submit_coroutine(
                session_id="s-queue",
                tool_name="demo_tool",
                coroutine_obj=_demo(),
            )
            for _ in range(100):
                if sent:
                    break
                await asyncio.sleep(0.01)
            self.assertTrue(sent)
            sid, payload = sent[0]
            self.assertEqual(sid, "s-queue")
            self.assertEqual(payload["event_type"], "async_tool_result")
            self.assertEqual(payload["job_id"], job.job_id)
            self.assertEqual(payload["session_id"], "s-queue")
            self.assertEqual(payload["tool_name"], "demo_tool")
            self.assertEqual(payload["status"], "succeeded")
            self.assertEqual(payload["result_text"], "done-payload")

        asyncio.run(_run())


if __name__ == "__main__":
    unittest.main()
