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

    async def test_submit_message_clears_hitl_queue(self) -> None:
        """新用户消息应丢弃本地 HITL，避免 stale call_id 提交 resume。"""
        mock_client = MagicMock()
        mock_client.submit_message = AsyncMock()
        self.controller._client = mock_client
        self.controller._sse_ready.set()

        await self.controller._handle_stream_event(
            _event(
                "approval_required",
                data={
                    "approval_args": {
                        "tool_calls": [{"id": "call_old", "name": "bash_run", "arguments": {}}],
                    }
                },
            ),
            {"v": False},
        )
        self.assertEqual(self.controller.hitl_queue_len(), 1)

        await self.controller.submit_message("改问深圳天气")

        self.assertEqual(self.controller.hitl_queue_len(), 0)
        mock_client.submit_message.assert_awaited_once()

    async def test_new_approval_replaces_stale_approval_in_queue(self) -> None:
        """新的 approval_required 应替换队列中旧 approval。"""
        old_data = {
            "approval_args": {
                "tool_calls": [{"id": "call_old", "name": "bash_run", "arguments": {}}],
            }
        }
        new_data = {
            "approval_args": {
                "tool_calls": [{"id": "call_new", "name": "bash_run", "arguments": {}}],
            }
        }
        await self.controller._handle_stream_event(
            _event("approval_required", data=old_data),
            {"v": False},
        )
        await self.controller._handle_stream_event(
            _event("approval_required", data=new_data),
            {"v": False},
        )
        self.assertEqual(self.controller.hitl_queue_len(), 1)
        item = self.controller.peek_hitl()
        self.assertIsNotNone(item)
        ids = [r.call_id for r in extract_tool_approval_requests(item.data)]  # type: ignore[union-attr]
        self.assertEqual(ids, ["call_new"])

    async def test_done_refreshes_context_tokens(self) -> None:
        """done 后应拉取 context 并更新 input strip token 统计。"""
        mock_client = MagicMock()
        mock_client.get_session_context = AsyncMock(return_value={"messages_total_tokens": 4321})
        self.controller._client = mock_client
        notified = asyncio.Event()
        self.controller.on_child_strip(lambda: notified.set())

        await self.controller._handle_stream_event(_event("done"), {"v": False})
        await asyncio.wait_for(notified.wait(), timeout=1.0)

        mock_client.get_session_context.assert_awaited_once_with("s1")
        self.assertEqual(self.controller._messages_total_tokens, 4321)
        self.assertEqual(self.controller.input_strip_token_text(), "ctx 4,321")


if __name__ == "__main__":
    unittest.main()
