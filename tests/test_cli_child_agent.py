from __future__ import annotations

import asyncio
import unittest

from app.cli.api_client import StreamEvent
from app.cli.approval import build_all_approved_decision, build_approval_resume, extract_tool_approval_requests
from app.cli.child_agent import (
    ChildAgentTracker,
    format_child_agents_list,
    format_child_lifecycle_line,
    should_skip_child_runtime_display,
)
from app.cli.render import TranscriptKind
from app.cli.session_controller import SessionController


def _event(event_type: str, *, data: dict | None = None) -> StreamEvent:
    payload_data = data if data is not None else {}
    return StreamEvent(
        event_type=event_type,
        event_id=None,
        payload={"session_id": "parent", "data": payload_data},
    )


class ChildAgentScopeTests(unittest.TestCase):
    def test_should_skip_child_runtime_but_not_approval(self) -> None:
        child_data = {"child_session_id": "child-1", "content": "secret"}
        self.assertTrue(should_skip_child_runtime_display("assistant", child_data))
        self.assertFalse(should_skip_child_runtime_display("approval_required", child_data))
        self.assertFalse(should_skip_child_runtime_display("child_agent_created", child_data))

    def test_format_child_lifecycle_created(self) -> None:
        line = format_child_lifecycle_line(
            "child_agent_created",
            {"child_session_id": "abc", "purpose": "scan logs"},
        )
        self.assertIn("子任务已创建", line)
        self.assertIn("scan logs", line)

    def test_build_approval_resume_injects_child_routing(self) -> None:
        data = {"child_session_id": "child-1", "approval_id": "ap-1"}
        resume = build_approval_resume(
            data,
            build_all_approved_decision(
                extract_tool_approval_requests(
                    {
                        "approval_args": {
                            "tool_calls": [{"id": "call_1", "name": "bash", "arguments": {}}],
                        }
                    }
                )
            ),
        )
        self.assertEqual(resume["child_session_id"], "child-1")
        self.assertEqual(resume["approval_id"], "ap-1")

    def test_format_child_agents_list_empty(self) -> None:
        self.assertEqual(format_child_agents_list([]), "活跃子 Agent: (无)")


class ChildAgentTrackerTests(unittest.TestCase):
    def test_tracker_counts_and_strip(self) -> None:
        tracker = ChildAgentTracker()
        tracker.on_created({"child_session_id": "c1", "purpose": "p1"})
        tracker.set_awaiting_approval("c1", True)
        active, pending = tracker.counts()
        self.assertEqual(active, 1)
        self.assertEqual(pending, 1)
        text = tracker.input_strip_text(queue_len=2)
        self.assertIn("1 活跃", text)
        self.assertIn("1 待审批", text)
        self.assertIn("队列 2", text)


class SessionControllerChildFilterTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self) -> None:
        self.controller = SessionController(
            api_base="http://test",
            session_id="parent",
            show_reasoning=False,
        )
        self.controller.session_id = "parent"
        self.updates: list = []
        self.controller.on_transcript(lambda update: self.updates.append(update))

    async def test_child_assistant_not_rendered(self) -> None:
        await self.controller._handle_stream_event(
            _event("assistant", data={"child_session_id": "c1", "content": "hidden"}),
        )
        self.assertEqual(len(self.updates), 0)

    async def test_child_lifecycle_emits_system_line(self) -> None:
        await self.controller._handle_stream_event(
            _event(
                "child_agent_created",
                data={"child_session_id": "c1", "purpose": "task"},
            ),
        )
        self.assertEqual(len(self.updates), 1)
        self.assertEqual(self.updates[0].kind, TranscriptKind.LINE)
        self.assertIn("子任务已创建", self.updates[0].text)
        self.assertEqual(self.controller.child_tracker.counts()[0], 1)

    async def test_approval_enqueued_without_blocking(self) -> None:
        notified = asyncio.Event()
        self.controller.on_hitl_pending(lambda: notified.set())
        await self.controller._handle_stream_event(
            _event(
                "approval_required",
                data={
                    "child_session_id": "c1",
                    "approval_args": {
                        "tool_calls": [{"id": "call_1", "name": "bash", "arguments": {}}],
                    },
                },
            ),
        )
        await asyncio.wait_for(notified.wait(), timeout=1.0)
        item = self.controller.peek_hitl()
        self.assertIsNotNone(item)
        assert item is not None
        self.assertEqual(item.kind, "approval")
        self.assertEqual(self.controller.child_tracker.counts()[1], 1)


if __name__ == "__main__":
    unittest.main()
