"""`InMemoryEventBus` 单测：`publish` 与 `subscribe_all` 异步迭代。"""

from __future__ import annotations

import asyncio
import unittest

from app.harness.streaming.events import InMemoryEventBus


class InMemoryEventBusTests(unittest.IsolatedAsyncioTestCase):
    """内存总线：按 client 分桶投递与序号递增。"""

    async def test_publish_delivers_to_subscribe_all_same_client(self) -> None:
        """指定 `client_id` 订阅后，`publish` 的同一 `client_id` 事件应进入该订阅队列。"""
        bus = InMemoryEventBus()

        async def consume() -> list[str]:
            out: list[str] = []
            async for ev in bus.subscribe_all(client_id="c1"):
                out.append(ev.type)
                if len(out) >= 2:
                    break
            return out

        task = asyncio.create_task(consume())
        # 让消费协程先进入 `subscribe_all` 并阻塞在 `queue.get()`，再发布，避免事件早于订阅注册而丢失。
        await asyncio.sleep(0.02)
        bus.publish(client_id="c1", session_id="s1", event_type="assistant", data={"meta": {}, "content": "a"})
        bus.publish(client_id="c1", session_id="s1", event_type="done", data={"meta": {}})
        types = await asyncio.wait_for(task, timeout=2.0)
        self.assertEqual(types, ["assistant", "done"])

    async def test_seq_increments_per_client(self) -> None:
        """同一 `client_id` 下 `seq` 单调递增。"""
        bus = InMemoryEventBus()
        e1 = bus.publish(client_id="c2", session_id="s", event_type="t", data={})
        e2 = bus.publish(client_id="c2", session_id="s", event_type="t", data={})
        self.assertEqual(e1.seq + 1, e2.seq)


if __name__ == "__main__":
    unittest.main()
