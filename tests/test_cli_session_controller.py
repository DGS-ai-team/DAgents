from __future__ import annotations

import asyncio
import unittest
from unittest.mock import AsyncMock, MagicMock

from app.cli.api_client import StreamEvent
from app.cli.approval import build_all_approved_decision
from app.cli.render import TranscriptKind
from app.cli.session_controller import SessionController


def _event(event_type: str, *, content: str = "", message: str = "") -> StreamEvent:
    data: dict[str, object] = {}
    if content:
        data["content"] = content
    if message:
        data["message"] = message
    return StreamEvent(
        event_type=event_type,
        event_id=None,
        payload={"session_id": "s1", "data": data},
    )


class SessionControllerRenderTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self) -> None:
        self.controller = SessionController(
            api_base="http://test",
            session_id="s1",
            client_id="c1",
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

    async def test_approval_skips_first_done_before_user_turn_completes(self) -> None:
        """approval 后第一条 done 被 skip，第二条 done 才结束用户 turn 等待。"""
        mock_client = MagicMock()
        mock_client.submit_resume = AsyncMock()
        self.controller._client = mock_client

        async def approve(reqs: list) -> object:
            return build_all_approved_decision(reqs)

        self.controller.on_approval(approve)

        self.controller._reset_user_turn_wait()
        wait_task = asyncio.create_task(self.controller.wait_user_turn())
        skip_holder = {"v": False}

        await self.controller._handle_stream_event(_event("assistant", content="plan"), skip_holder)
        skip_holder["v"] = await self.controller._handle_approval(
            {
                "approval_args": {
                    "tool_calls": [
                        {"id": "call_1", "name": "read_file", "arguments": {"path": "a.txt"}},
                    ]
                }
            }
        )
        await self.controller._handle_stream_event(_event("done"), skip_holder)
        await asyncio.sleep(0)
        self.assertFalse(self.controller._user_turn_done.is_set())

        await self.controller._handle_stream_event(_event("assistant", content="done work"), skip_holder)
        await self.controller._handle_stream_event(_event("done"), skip_holder)

        await asyncio.wait_for(wait_task, timeout=1.0)
        mock_client.submit_resume.assert_awaited_once()


class SessionControllerBindTriggersTests(unittest.IsolatedAsyncioTestCase):
    async def test_bind_triggers_patches_matching_session(self) -> None:
        """bind_triggers_to_client 应 PATCH 同 session 且 client_id 不匹配的 trigger。"""
        controller = SessionController(
            api_base="http://test",
            session_id="s1",
            client_id="cli-1",
            show_reasoning=False,
        )
        controller.session_id = "s1"
        mock_client = MagicMock()
        mock_client.list_triggers = AsyncMock(
            return_value={
                "triggers": [
                    {"trigger_id": "t1", "target_session_id": "s1", "client_id": "trigger-t1"},
                    {"trigger_id": "t2", "target_session_id": "other", "client_id": ""},
                    {"trigger_id": "t3", "target_session_id": "s1", "client_id": "cli-1"},
                ]
            }
        )
        mock_client.patch_trigger = AsyncMock(return_value={})
        controller._client = mock_client

        bound = await controller.bind_triggers_to_client()

        self.assertEqual(bound, 1)
        mock_client.patch_trigger.assert_awaited_once_with(
            "t1",
            {"target_session_id": "s1", "client_id": "cli-1"},
        )


if __name__ == "__main__":
    unittest.main()
