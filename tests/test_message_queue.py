from __future__ import annotations

import asyncio
import sys
import unittest
from dataclasses import dataclass
from pathlib import Path

_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_ROOT))

from app.harness.queue.message_queue import MessageEnvelope, MessageQueue


class MessageQueuePendingMetricsTestCase(unittest.TestCase):
    def test_pending_metrics_rows_heap_order_matches_dequeue(self) -> None:
        """pending_metrics_rows 按优先级与入队次序排序，与预期出队顺序一致。"""
        mq = MessageQueue[MessageEnvelope](max_queue_size=0)
        mq.enqueue(envelope=MessageEnvelope(session_id="s1", content="other-1"))
        mq.enqueue(
            envelope=MessageEnvelope(session_id="s1", content="resume-1"),
            priority="resume",
        )
        mq.enqueue(
            envelope=MessageEnvelope(session_id="s1", content="human-1"),
            priority="human",
        )
        rows = mq.pending_metrics_rows()
        self.assertEqual(len(rows), 3)
        self.assertEqual(rows[0][2].content, "human-1")
        self.assertEqual(rows[1][2].content, "resume-1")
        self.assertEqual(rows[2][2].content, "other-1")


class MessageQueuePriorityTestCase(unittest.IsolatedAsyncioTestCase):
    async def test_human_priority_has_higher_priority_than_other(self) -> None:
        consumed: list[str] = []

        async def handler(env: MessageEnvelope) -> None:
            c = env.content or ""
            consumed.append(c)

        mq = MessageQueue(max_queue_size=0)

        async def consumer() -> None:
            while True:
                try:
                    env = await mq.receive()
                except RuntimeError:
                    return
                await handler(env)

        t = asyncio.create_task(consumer())
        try:
            mq.enqueue(
                envelope=MessageEnvelope(session_id="s1", content="normal-1"),
            )
            mq.enqueue(
                envelope=MessageEnvelope(session_id="s1", content="normal-2"),
            )
            mq.enqueue(
                envelope=MessageEnvelope(session_id="s1", content="human-1"),
                priority="human",
            )
            await asyncio.sleep(0.1)
        finally:
            await mq.stop()
            t.cancel()
            try:
                await t
            except asyncio.CancelledError:
                pass

        self.assertEqual(consumed, ["human-1", "normal-1", "normal-2"])

    async def test_priority_human_resume_other_order(self) -> None:
        consumed: list[str] = []

        async def handler(env: MessageEnvelope) -> None:
            consumed.append(env.content or "")

        mq = MessageQueue(max_queue_size=0)

        async def consumer() -> None:
            while True:
                try:
                    env = await mq.receive()
                except RuntimeError:
                    return
                await handler(env)

        t = asyncio.create_task(consumer())
        try:
            mq.enqueue(envelope=MessageEnvelope(session_id="s1", content="other-1"))
            mq.enqueue(
                envelope=MessageEnvelope(session_id="s1", content="resume-1"),
                priority="resume",
            )
            mq.enqueue(
                envelope=MessageEnvelope(session_id="s1", content="human-1"),
                priority="human",
            )
            await asyncio.sleep(0.1)
        finally:
            await mq.stop()
            t.cancel()
            try:
                await t
            except asyncio.CancelledError:
                pass

        self.assertEqual(consumed, ["human-1", "resume-1", "other-1"])

    async def test_tool_result_priority_higher_than_human(self) -> None:
        consumed: list[str] = []

        async def handler(env: MessageEnvelope) -> None:
            consumed.append(env.content or "")

        mq = MessageQueue(max_queue_size=0)

        async def consumer() -> None:
            while True:
                try:
                    env = await mq.receive()
                except RuntimeError:
                    return
                await handler(env)

        t = asyncio.create_task(consumer())
        try:
            mq.enqueue(
                envelope=MessageEnvelope(session_id="s1", content="human-1"),
                priority="human",
            )
            mq.enqueue(
                envelope=MessageEnvelope(session_id="s1", content="async-result-1"),
                priority="tool_result",
            )
            mq.enqueue(
                envelope=MessageEnvelope(session_id="s1", content="resume-1"),
                priority="resume",
            )
            await asyncio.sleep(0.1)
        finally:
            await mq.stop()
            t.cancel()
            try:
                await t
            except asyncio.CancelledError:
                pass

        self.assertEqual(consumed, ["async-result-1", "human-1", "resume-1"])

    async def test_queue_can_use_custom_envelope_object(self) -> None:
        @dataclass(frozen=True)
        class CustomEnvelope:
            sid: str
            body: str
            src: str

        consumed: list[CustomEnvelope] = []

        async def handler(env: CustomEnvelope) -> None:
            consumed.append(env)

        mq = MessageQueue[CustomEnvelope](max_queue_size=0)

        async def consumer() -> None:
            while True:
                try:
                    env = await mq.receive()
                except RuntimeError:
                    return
                await handler(env)

        t = asyncio.create_task(consumer())
        try:
            mq.enqueue(
                envelope=CustomEnvelope(sid="s-custom", body="hello", src="unit-test"),
                priority="resume",
            )
            await asyncio.sleep(0.1)
        finally:
            await mq.stop()
            t.cancel()
            try:
                await t
            except asyncio.CancelledError:
                pass

        self.assertEqual(len(consumed), 1)
        self.assertEqual(consumed[0], CustomEnvelope(sid="s-custom", body="hello", src="unit-test"))


if __name__ == "__main__":
    unittest.main()
