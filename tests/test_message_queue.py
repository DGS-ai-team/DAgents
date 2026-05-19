"""`MessageQueue` / `MessageEnvelope` 单测：优先级、同优先 FIFO、关闭语义与观测堆序。"""

from __future__ import annotations

import asyncio
import unittest
from dataclasses import dataclass

from app.harness.queue.message_queue import MessageEnvelope, MessageQueue


@dataclass(frozen=True)
class _CustomEnvelope:
    """无 `session_id` / `request_type` 的自定义载荷：验证 enqueue 日志路径不因 getattr 崩溃。"""

    label: str


class MessageQueueAsyncTests(unittest.IsolatedAsyncioTestCase):
    """异步队列行为：`receive` 与 `stop` 协作。"""

    async def test_priority_order_tool_human_resume_other(self) -> None:
        """数值越小越优先：`tool_result` < `human` < `resume` < `other`。"""
        q: MessageQueue[MessageEnvelope] = MessageQueue()
        for i, pr in enumerate(["other", "resume", "human", "tool_result"]):
            q.enqueue(
                envelope=MessageEnvelope(session_id="s", content=f"m{i}", source="t"),
                priority=pr,  # type: ignore[arg-type]
            )
        out = [await q.receive() for _ in range(4)]
        self.assertEqual([m.content for m in out], ["m3", "m2", "m1", "m0"])

    async def test_same_priority_fifo_by_enqueue_order(self) -> None:
        """同优先级下按入队顺序出队（内部 `_seq` 稳定）。"""
        q: MessageQueue[MessageEnvelope] = MessageQueue()
        for i in range(3):
            q.enqueue(
                envelope=MessageEnvelope(session_id="s", content=str(i), source="t"),
                priority="human",
            )
        contents: list[str] = []
        for _ in range(3):
            contents.append((await q.receive()).content)
        self.assertEqual(contents, ["0", "1", "2"])

    async def test_stop_unblocks_receive_with_runtime_error(self) -> None:
        """先 `pause_consuming` 使 `receive` 阻塞在闸门上；`stop` 置 `_closed` 并 `set` gate 后抛 `RuntimeError`。"""
        q: MessageQueue[MessageEnvelope] = MessageQueue()
        q.pause_consuming()

        async def waiter() -> None:
            with self.assertRaises(RuntimeError):
                await q.receive()

        t = asyncio.create_task(waiter())
        await asyncio.sleep(0)
        await q.stop()
        await t

    async def test_enqueue_after_stop_raises(self) -> None:
        """关闭后禁止再入队，避免静默丢消息。"""
        q: MessageQueue[MessageEnvelope] = MessageQueue()
        await q.stop()
        with self.assertRaises(RuntimeError):
            q.enqueue(envelope=MessageEnvelope(session_id="s", content="x", source="t"), priority="human")

    def test_custom_envelope_enqueue_no_crash(self) -> None:
        """泛型 envelope 无 `session_id` 时仍应完成入队（日志侧 getattr）。"""
        q: MessageQueue[_CustomEnvelope] = MessageQueue()
        q.enqueue(envelope=_CustomEnvelope("a"), priority="other")
        self.assertEqual(q.pending_metrics_rows()[0][2].label, "a")

    async def test_pending_metrics_rows_sorted_like_dequeue(self) -> None:
        """`pending_metrics_rows` 按 (priority, seq) 排序，与真实出队顺序一致。"""
        q: MessageQueue[MessageEnvelope] = MessageQueue()
        q.enqueue(envelope=MessageEnvelope(session_id="s", content="a", source="t"), priority="human")
        q.enqueue(envelope=MessageEnvelope(session_id="s", content="b", source="t"), priority="human")
        q.enqueue(envelope=MessageEnvelope(session_id="s", content="c", source="t"), priority="tool_result")
        rows = q.pending_metrics_rows()
        self.assertEqual(len(rows), 3)
        self.assertEqual(rows[0][0], -1)
        self.assertEqual(rows[0][2].content, "c")

        received = await q.receive()
        self.assertEqual(received.content, "c")
        rows_after_receive = q.pending_metrics_rows()
        self.assertEqual([row[2].content for row in rows_after_receive], ["a", "b"])


if __name__ == "__main__":
    unittest.main()
