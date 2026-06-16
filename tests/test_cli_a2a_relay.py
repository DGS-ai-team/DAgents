"""A2A 中继 HITL 展示辅助与 TUI 边界测试。"""

from __future__ import annotations

import asyncio
import unittest

from app.cli.api_client import StreamEvent
from app.cli.child_agent import (
    a2a_peer_label,
    a2a_relay_tool_suffix,
    approval_header,
    is_a2a_relay_hitl,
)
from app.cli.session_controller import SessionController


def _event(event_type: str, *, data: dict | None = None) -> StreamEvent:
    return StreamEvent(
        event_type=event_type,
        event_id=None,
        payload={"session_id": "s1", "data": dict(data) if data else {}},
    )


def test_is_a2a_relay_hitl() -> None:
    assert not is_a2a_relay_hitl(None)
    assert not is_a2a_relay_hitl({})
    assert is_a2a_relay_hitl({"a2a_relay": True})


def test_a2a_peer_label_prefers_name() -> None:
    data = {"a2a_peer_agent_name": "合规助手", "a2a_peer_agent_id": "node-a"}
    assert a2a_peer_label(data) == "合规助手"
    assert a2a_peer_label({"a2a_peer_agent_id": "node-a"}) == "node-a"
    assert a2a_peer_label({}) == ""


def test_a2a_relay_tool_suffix() -> None:
    suffix = a2a_relay_tool_suffix({"a2a_peer_agent_name": "Node-B"})
    assert "from Node-B" in suffix
    assert "dim cyan" in suffix
    default = a2a_relay_tool_suffix({"a2a_relay": True})
    assert "from 对端 Agent" in default


def test_approval_header_a2a_includes_peer() -> None:
    header = approval_header({"a2a_relay": True, "a2a_peer_agent_name": "合规助手"})
    assert "合规助手" in header
    assert header.startswith("A2A")


def test_approval_header_local_unchanged() -> None:
    assert approval_header({}) == "工具审批"


class A2ARelayControllerTests(unittest.IsolatedAsyncioTestCase):
    async def asyncSetUp(self) -> None:
        self.controller = SessionController(
            api_base="http://test",
            session_id="s1",
            show_reasoning=False,
        )
        self.controller.session_id = "s1"

    async def test_a2a_user_information_releases_turn_wait(self) -> None:
        self.controller._reset_user_turn_wait()
        wait_task = asyncio.create_task(self.controller.wait_user_turn())
        await self.controller._handle_stream_event(
            _event(
                "user_information_required",
                data={
                    "a2a_relay": True,
                    "a2a_task_id": "task-ui",
                    "a2a_peer_agent_name": "合规助手",
                    "user_information_args": {
                        "tool_call_id": "call-ask-1",
                        "question": "请确认环境",
                    },
                },
            ),
        )
        await asyncio.wait_for(wait_task, timeout=1.0)
        self.assertFalse(self.controller._awaiting_user_turn)
        self.assertEqual(self.controller.hitl_queue_len(), 1)

    async def test_a2a_relay_queues_multiple_hitl_kinds(self) -> None:
        await self.controller._handle_stream_event(
            _event(
                "approval_required",
                data={
                    "a2a_relay": True,
                    "approval_args": {"tool_calls": [{"id": "c1", "name": "bash_run", "arguments": {}}]},
                },
            ),
        )
        await self.controller._handle_stream_event(
            _event(
                "user_information_required",
                data={
                    "a2a_relay": True,
                    "user_information_args": {"tool_call_id": "ask-1", "question": "q"},
                },
            ),
        )
        self.assertEqual(self.controller.hitl_queue_len(), 2)

    async def test_a2a_approval_releases_turn_without_tool_result(self) -> None:
        """A2A 中继审批事件应释放 turn 等待，且不依赖本地 tool_result。"""
        self.controller._reset_user_turn_wait()
        wait_task = asyncio.create_task(self.controller.wait_user_turn())
        await self.controller._handle_stream_event(
            _event(
                "approval_required",
                data={
                    "a2a_relay": True,
                    "a2a_peer_agent_name": "Node-A",
                    "approval_args": {
                        "tool_calls": [{"id": "call_x", "name": "bash_run", "arguments": {"command": "date"}}],
                    },
                },
            ),
        )
        await asyncio.wait_for(wait_task, timeout=1.0)
        self.assertEqual(self.controller.hitl_queue_len(), 1)


if __name__ == "__main__":
    unittest.main()
