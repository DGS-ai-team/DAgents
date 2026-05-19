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

    async def test_slow_subscriber_drops_oldest_when_queue_full(self) -> None:
        """慢订阅者队列满时应丢弃最旧事件，避免无界积压并保留最新事件。"""
        bus = InMemoryEventBus(subscriber_queue_size=2)
        stream = bus.subscribe_all(client_id="c3")
        first_task = asyncio.create_task(stream.__anext__())
        await asyncio.sleep(0.02)

        bus.publish(client_id="c3", session_id="s", event_type="one", data={})
        first = await asyncio.wait_for(first_task, timeout=2.0)
        self.assertEqual(first.type, "one")

        bus.publish(client_id="c3", session_id="s", event_type="two", data={})
        bus.publish(client_id="c3", session_id="s", event_type="three", data={})
        bus.publish(client_id="c3", session_id="s", event_type="four", data={})

        second = await asyncio.wait_for(stream.__anext__(), timeout=2.0)
        third = await asyncio.wait_for(stream.__anext__(), timeout=2.0)
        self.assertEqual([second.type, third.type], ["three", "four"])
        await stream.aclose()

    async def test_subscribe_replays_events_after_last_seq_for_same_client(self) -> None:
        """带 `last_seq` 重连时，只回放同一 client 中 seq 更大的近期历史事件。"""
        bus = InMemoryEventBus(history_size=4)
        bus.publish(client_id="c4", session_id="s", event_type="old", data={})
        bus.publish(client_id="other", session_id="s", event_type="other", data={})
        bus.publish(client_id="c4", session_id="s", event_type="missed-a", data={})
        bus.publish(client_id="c4", session_id="s", event_type="missed-b", data={})

        stream = bus.subscribe_all(client_id="c4", last_seq=0)
        first = await asyncio.wait_for(stream.__anext__(), timeout=2.0)
        second = await asyncio.wait_for(stream.__anext__(), timeout=2.0)

        self.assertEqual([first.type, second.type], ["missed-a", "missed-b"])
        self.assertEqual([first.seq, second.seq], [1, 2])
        await stream.aclose()

    async def test_history_is_bounded_to_recent_events(self) -> None:
        """历史缓冲仅保留最近 `history_size` 条事件，避免重连回放自身无界增长。"""
        bus = InMemoryEventBus(history_size=2)
        for event_type in ["one", "two", "three"]:
            bus.publish(client_id="c5", session_id="s", event_type=event_type, data={})

        stream = bus.subscribe_all(client_id="c5", last_seq=-1)
        first = await asyncio.wait_for(stream.__anext__(), timeout=2.0)
        second = await asyncio.wait_for(stream.__anext__(), timeout=2.0)

        self.assertEqual([first.type, second.type], ["two", "three"])
        await stream.aclose()


if __name__ == "__main__":
    unittest.main()
