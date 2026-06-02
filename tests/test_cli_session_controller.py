from __future__ import annotations

import asyncio
import unittest
from unittest.mock import AsyncMock, MagicMock

from app.cli.api_client import StreamEvent
from app.cli.approval import build_all_approved_decision, extract_tool_approval_requests
from app.cli.render import TranscriptKind
from app.cli.session_controller import SessionController


def _event(event_type: str, *, content: str = "", message: str = "", data: dict | None = None) -> StreamEvent:
    payload_data: dict[str, object] = dict(data) if data is not None else {}
    if content:
        payload_data["content"] = content
    if message:
        payload_data["message"] = message
    return StreamEvent(
        event_type=event_type,
        event_id=None,
        payload={"session_id": "s1", "data": payload_data},
    )


class SessionControllerRenderTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self) -> None:
        self.controller = SessionController(
            api_base="http://test",
            session_id="s1",
            show_reasoning=False,
        )
        self.controller.session_id = "s1"
        self.updates: list = []
        self.controller.on_transcript(lambda update: self.updates.append(update))

    async def test_background_assistant_renders_without_user_submit(self) -> None:
        """空闲期 SSE 事件应被 render 回调消费，不依赖用户 submit。"""
        skip_holder = {"v": False}
        await self.controller._handle_stream_event(_event("assistant", content="hello"), skip_holder)

        self.assertEqual(len(self.updates), 1)
        self.assertEqual(self.updates[0].kind, TranscriptKind.ASSISTANT_DELTA)
        self.assertEqual(self.updates[0].text, "hello")

    async def test_wait_user_turn_ignores_done_before_user_turn_starts(self) -> None:
        """submit 后、用户 turn 内容出现前的 done（如在途 trigger）不应结束等待。"""
        self.controller._reset_user_turn_wait()
        wait_task = asyncio.create_task(self.controller.wait_user_turn())
        skip_holder = {"v": False}

        await self.controller._handle_stream_event(_event("done"), skip_holder)
        await asyncio.sleep(0)
        self.assertFalse(self.controller._user_turn_done.is_set())

        await self.controller._handle_stream_event(_event("assistant", content="reply"), skip_holder)
        await self.controller._handle_stream_event(_event("done"), skip_holder)

        await asyncio.wait_for(wait_task, timeout=1.0)
        self.assertTrue(self.controller._user_turn_done.is_set())

    async def test_approval_enqueued_and_skip_done_after_resume(self) -> None:
        """approval 入队不阻塞；complete 后 skip 第一条 done。"""
        mock_client = MagicMock()
        mock_client.submit_resume = AsyncMock()
        self.controller._client = mock_client

        self.controller._reset_user_turn_wait()
        wait_task = asyncio.create_task(self.controller.wait_user_turn())
        skip_holder = {"v": False}

        await self.controller._handle_stream_event(_event("assistant", content="plan"), skip_holder)
        await self.controller._handle_stream_event(
            _event(
                "approval_required",
                data={
                    "approval_args": {
                        "tool_calls": [
                            {"id": "call_1", "name": "read_file", "arguments": {"path": "a.txt"}},
                        ]
                    }
                },
            ),
            skip_holder,
        )
        item = self.controller.peek_hitl()
        self.assertIsNotNone(item)

        requests = extract_tool_approval_requests(item.data)  # type: ignore[union-attr]
        await self.controller.complete_hitl_approval(build_all_approved_decision(requests))

        await self.controller._handle_stream_event(_event("done"), skip_holder)
        await asyncio.sleep(0)
        self.assertFalse(self.controller._user_turn_done.is_set())

        await self.controller._handle_stream_event(_event("assistant", content="done work"), skip_holder)
        await self.controller._handle_stream_event(_event("done"), skip_holder)

        await asyncio.wait_for(wait_task, timeout=1.0)
        mock_client.submit_resume.assert_awaited_once()


if __name__ == "__main__":
    unittest.main()
