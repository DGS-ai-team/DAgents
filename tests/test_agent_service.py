from __future__ import annotations

import asyncio
import io
import sys
import time
import unittest
from contextlib import redirect_stdout
from pathlib import Path

_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(_ROOT))

from app.context.models import OpenAIConversationContext
from app.harness.service.agent_service import AgentService
from app.harness.service.interface import AgentEventEnvelope, AgentSubmitRequest

_MAP_BASE_META = {"session_id": "s", "model": "unit-model"}


class AgentSubmitRequestPriorityTestCase(unittest.TestCase):
    def test_resume_coerces_default_other_to_resume(self) -> None:
        r = AgentSubmitRequest(
            session_id="s",
            request_type="resume",
            resume_value={"type": "approve"},
        )
        self.assertEqual(r.priority, "resume")

    def test_resume_explicit_human_unchanged(self) -> None:
        r = AgentSubmitRequest(
            session_id="s",
            request_type="resume",
            resume_value={"type": "approve"},
            priority="human",
        )
        self.assertEqual(r.priority, "human")


class HangRuntime:
    """用于验证取消时 `_handle_message` 会调用 `flush_cancelled_turn`。"""

    def __init__(self) -> None:
        self.started = asyncio.Event()
        self.flush_count = 0

    async def run_turn(self, ctx: OpenAIConversationContext, *, request_type: str, content=None, resume_value=None):
        self.started.set()
        await asyncio.sleep(30.0)
        yield AgentEventEnvelope(event_type="done", payload={}, meta={})

    def flush_cancelled_turn(self, ctx: OpenAIConversationContext) -> None:
        self.flush_count += 1


class HangThenPongRuntime:
    """首条 `run_turn` 在长时间 sleep 上直至被取消；第二条正常产出事件。"""

    def __init__(self) -> None:
        self.run_turn_calls = 0
        self.flush_count = 0

    async def run_turn(self, ctx: OpenAIConversationContext, *, request_type: str, content=None, resume_value=None):
        self.run_turn_calls += 1
        if self.run_turn_calls == 1:
            await asyncio.sleep(3600.0)
        yield AgentEventEnvelope(
            event_type="assistant",
            payload={"content": "second-done"},
            meta={},
        )
        yield AgentEventEnvelope(event_type="done", payload={}, meta={})

    def flush_cancelled_turn(self, ctx: OpenAIConversationContext) -> None:
        self.flush_count += 1


class FakeRuntime:
    def __init__(self) -> None:
        self.calls = 0
        self.last_kwargs: dict | None = None
        self.last_ctx: OpenAIConversationContext | None = None

    async def run_turn(self, ctx: OpenAIConversationContext, *, request_type: str, content=None, resume_value=None):
        self.calls += 1
        self.last_ctx = ctx
        self.last_kwargs = {
            "request_type": request_type,
            "content": content,
            "resume_value": resume_value,
        }
        yield AgentEventEnvelope(
            event_type="assistant",
            payload={"content": "pong"},
            meta={},
        )
        yield AgentEventEnvelope(
            event_type="approval_required",
            payload={
                "approval_type": "execute_tool",
                "message": "test interrupt",
                "args": {},
                "description": "test desc",
                "approval_id": "approval-1",
            },
            meta={},
        )
        yield AgentEventEnvelope(event_type="done", payload={}, meta={})


class AgentServiceTestCase(unittest.IsolatedAsyncioTestCase):
    async def test_create_session_binds_queue_and_runtime(self) -> None:
        fake_runtime = FakeRuntime()
        service = AgentService(max_queue_size=0)
        service._message_store = None
        service._get_runtime = lambda: fake_runtime  # type: ignore[method-assign]
        with redirect_stdout(io.StringIO()):
            await service.start()
            sid = await service.create_session()
            self.assertIn(sid, service._session_queues)
            await service.stop()
        self.assertTrue(sid)

    async def test_service_can_consume_queue_and_process_message(self) -> None:
        fake_runtime = FakeRuntime()
        service = AgentService(max_queue_size=0)
        service._message_store = None
        service._get_runtime = lambda: fake_runtime  # type: ignore[method-assign]

        buf = io.StringIO()
        with redirect_stdout(buf):
            await service.start()
            await service.submit_message(
                session_id="test-session",
                content="ping",
                source="unit-test",
            )
            await asyncio.sleep(0.1)
            await service.stop()

        output = buf.getvalue()
        self.assertEqual(fake_runtime.calls, 1)
        self.assertIsInstance(fake_runtime.last_ctx, OpenAIConversationContext)
        self.assertEqual(fake_runtime.last_kwargs, {
            "request_type": "human_message",
            "content": "ping",
            "resume_value": None,
        })
        self.assertIn("[agent-service][stream] session=test-session: type=assistant", output)

    async def test_service_emits_stream_events_when_callback_provided(self) -> None:
        fake_runtime = FakeRuntime()
        events: list[tuple[str, str, dict]] = []

        async def on_stream_event(client_id: str, session_id: str, event_type: str, data: dict):
            events.append((client_id, session_id, event_type, data))

        service = AgentService(max_queue_size=0, on_stream_event=on_stream_event)
        service._message_store = None
        service._get_runtime = lambda: fake_runtime  # type: ignore[method-assign]

        with redirect_stdout(io.StringIO()):
            await service.start()
            await service.submit_message(
                session_id="s-event",
                client_id="client-1",
                content="ping",
                source="unit-test",
            )
            await asyncio.sleep(0.1)
            await service.stop()

        self.assertTrue(events)
        self.assertEqual(events[0][0], "client-1")
        self.assertEqual(events[0][1], "s-event")
        self.assertEqual(events[0][2], "assistant")
        self.assertEqual(events[0][3]["meta"]["session_id"], "s-event")
        self.assertIn("model", events[0][3]["meta"])
        self.assertEqual(events[1][2], "approval_required")
        self.assertEqual(events[1][3]["approval_type"], "execute_tool")
        self.assertEqual(events[1][3]["meta"]["session_id"], "s-event")
        self.assertEqual(events[-1][2], "done")

    async def test_human_priority_does_not_auto_cancel_in_flight_turn(self) -> None:
        """仅 human 入队不会打断在途 turn；显式 cancel 后第二条才会执行。"""
        rt = HangThenPongRuntime()
        service = AgentService(max_queue_size=0)
        service._message_store = None
        service._get_runtime = lambda: rt  # type: ignore[method-assign]

        with redirect_stdout(io.StringIO()):
            await service.start()
            await service.submit_message(
                session_id="s-human",
                client_id="client-h",
                content="first",
                source="unit-test",
                priority="other",
            )
            for _ in range(200):
                if rt.run_turn_calls >= 1:
                    break
                await asyncio.sleep(0.02)
            self.assertEqual(rt.run_turn_calls, 1)
            await service.submit_message(
                session_id="s-human",
                client_id="client-h",
                content="second",
                source="unit-test",
                priority="human",
            )
            for _ in range(50):
                await asyncio.sleep(0.02)
            self.assertEqual(rt.run_turn_calls, 1)
            self.assertTrue(service.cancel_current_turn("s-human"))
            for _ in range(200):
                if rt.run_turn_calls >= 2:
                    break
                await asyncio.sleep(0.02)
            await service.stop()

        self.assertGreaterEqual(rt.flush_count, 1)
        self.assertGreaterEqual(rt.run_turn_calls, 2)

    async def test_cancel_current_turn_triggers_flush(self) -> None:
        hang = HangRuntime()
        service = AgentService(max_queue_size=0)
        service._message_store = None
        service._get_runtime = lambda: hang  # type: ignore[method-assign]

        with redirect_stdout(io.StringIO()):
            await service.start()
            await service.submit_message(session_id="s-cancel", content="x", source="unit-test")
            await asyncio.wait_for(hang.started.wait(), timeout=2.0)
            self.assertTrue(service.cancel_current_turn("s-cancel"))
            for _ in range(100):
                if hang.flush_count >= 1:
                    break
                await asyncio.sleep(0.02)
            await service.stop()

        self.assertGreaterEqual(hang.flush_count, 1)

    async def test_service_limits_active_session_queues_to_three(self) -> None:
        service = AgentService(max_queue_size=0)
        service._message_store = None
        service._get_runtime = lambda: FakeRuntime()  # type: ignore[method-assign]

        with redirect_stdout(io.StringIO()):
            await service.start()
            await service.submit_message(
                session_id="s1",
                content="ping1",
                source="unit-test",
            )
            await service.submit_message(
                session_id="s2",
                content="ping2",
                source="unit-test",
            )
            await service.submit_message(
                session_id="s3",
                content="ping3",
                source="unit-test",
            )

            with self.assertRaises(RuntimeError):
                await service.submit_message(
                    session_id="s4",
                    content="ping4",
                    source="unit-test",
                )
            await service.stop()

    async def test_evict_idle_session_when_at_capacity(self) -> None:
        """达上限时，若存在闲置超阈值的 session，则淘汰最久未活动者以接纳新 session。"""
        service = AgentService(max_queue_size=0)
        service._message_store = None
        service._get_runtime = lambda: FakeRuntime()  # type: ignore[method-assign]

        with redirect_stdout(io.StringIO()):
            await service.start()
            await service.submit_message(
                session_id="s1",
                content="ping1",
                source="unit-test",
            )
            await service.submit_message(
                session_id="s2",
                content="ping2",
                source="unit-test",
            )
            await service.submit_message(
                session_id="s3",
                content="ping3",
                source="unit-test",
            )
            # 等待三条会话各完成一轮 `_handle_message`（其开头会 touch），再把 s1 标为闲置超时。
            await asyncio.sleep(0.25)
            service._session_last_activity["s1"] = time.time() - 400.0

            await service.submit_message(
                session_id="s4",
                content="ping4",
                source="unit-test",
            )
            await asyncio.sleep(0.15)
            self.assertNotIn("s1", service._session_queues)
            self.assertIn("s4", service._session_queues)
            await service.stop()

    def test_map_event_envelope_approval_shape(self) -> None:
        event_type, data = AgentService._map_event_envelope_to_stream(
            AgentEventEnvelope(
                event_type="approval_required",
                payload={
                    "approval_type": "execute_tool",
                    "message": "msg",
                    "args": {"a": 1},
                    "description": "desc",
                    "approval_id": "a-1",
                },
                meta={},
            ),
            base_meta=_MAP_BASE_META,
        )
        self.assertEqual(event_type, "approval_required")
        self.assertEqual(data["approval_type"], "execute_tool")
        self.assertEqual(data["meta"]["session_id"], "s")

    def test_map_event_envelope_tool_result_shape(self) -> None:
        event_type, data = AgentService._map_event_envelope_to_stream(
            AgentEventEnvelope(
                event_type="tool_result",
                payload={"tool_call_id": "tc1", "content": "ok", "partial": True},
                meta={},
            ),
            base_meta=_MAP_BASE_META,
        )
        self.assertEqual(event_type, "tool_result")
        self.assertEqual(data["tool_call_id"], "tc1")
        self.assertTrue(data["partial"])
        self.assertEqual(data["meta"]["session_id"], "s")

    def test_map_event_envelope_reasoning_shape(self) -> None:
        event_type, data = AgentService._map_event_envelope_to_stream(
            AgentEventEnvelope(
                event_type="reasoning",
                payload={"content": "思考中"},
                meta={},
            ),
            base_meta=_MAP_BASE_META,
        )
        self.assertEqual(event_type, "reasoning")
        self.assertEqual(data["content"], "思考中")
        self.assertEqual(data["meta"]["model"], "unit-model")

    def test_map_event_envelope_usage_shape(self) -> None:
        event_type, data = AgentService._map_event_envelope_to_stream(
            AgentEventEnvelope(
                event_type="usage",
                payload={"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30},
                meta={"extra": 1},
            ),
            base_meta=_MAP_BASE_META,
        )
        self.assertEqual(event_type, "usage")
        self.assertEqual(data["prompt_tokens"], 10)
        self.assertEqual(data["completion_tokens"], 20)
        self.assertEqual(data["total_tokens"], 30)
        self.assertEqual(data["meta"]["session_id"], "s")
        self.assertEqual(data["meta"]["extra"], 1)

if __name__ == "__main__":
    unittest.main()

