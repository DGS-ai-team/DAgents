"""`app.context.models` 单测：持久化态与推理态往返、`run_turn_phase` 规范化。"""

from __future__ import annotations

import unittest

from app.context.models import (
    ConversationContext,
    OpenAIConversationContext,
    PendingToolCall,
    RunTurnPhase,
)


class OpenAIConversationContextRoundTripTests(unittest.TestCase):
    """`from_conversation_context` / `to_conversation_context` 与 `normalized_run_turn_phase_for_persist`。"""

    def test_round_trip_preserves_messages_and_pending(self) -> None:
        """推理态 → 持久化态 → 推理态后，消息与 pending 规格保持一致。"""
        ctx = OpenAIConversationContext(
            session_id="sid-1",
            messages=[{"role": "user", "content": "hi"}],
            pending_tool_calls=[
                PendingToolCall(call_id="c1", name="bash_run", arguments={"cmd": "echo"}),
            ],
            run_turn_phase=RunTurnPhase.AWAITING_TOOL_EXECUTION,
            messages_total_tokens=10,
            tool_loop_count=2,
            loaded_skills=[{"skill_name": "demo", "description": "d"}],
        )
        cc = ctx.to_conversation_context()
        self.assertEqual(len(cc.openai_messages), 1)
        self.assertEqual(len(cc.pending_tool_calls), 1)
        restored = OpenAIConversationContext.from_conversation_context(cc)
        restored.session_id = ctx.session_id
        self.assertEqual(restored.messages, ctx.messages)
        self.assertEqual(len(restored.pending_tool_calls), 1)
        self.assertEqual(restored.pending_tool_calls[0].call_id, "c1")
        self.assertEqual(restored.tool_loop_count, 2)

    def test_normalized_run_turn_phase_model_streaming_maps_to_idle(self) -> None:
        """流式中阶段不落库为 MODEL_STREAMING / BRANCH_RESOLVING，应规范为 IDLE。"""
        ctx = OpenAIConversationContext(
            session_id="s",
            messages=[],
            run_turn_phase=RunTurnPhase.MODEL_STREAMING,
        )
        cc = ctx.to_conversation_context()
        self.assertEqual(cc.run_turn_phase, RunTurnPhase.IDLE)

    def test_normalized_run_turn_phase_awaiting_without_pending_maps_to_idle(self) -> None:
        """无 pending 时 AWAITING_TOOL_EXECUTION 应回落 IDLE，避免 sqlite 写入悬挂阶段。"""
        ctx = OpenAIConversationContext(
            session_id="s",
            messages=[],
            pending_tool_calls=[],
            run_turn_phase=RunTurnPhase.AWAITING_TOOL_EXECUTION,
        )
        cc = ctx.to_conversation_context()
        self.assertEqual(cc.run_turn_phase, RunTurnPhase.IDLE)


class ConversationContextUnpackTests(unittest.TestCase):
    """`ConversationContext.unpack_for_openai_runtime` 过滤无效 pending。"""

    def test_unpack_skips_pending_without_call_id(self) -> None:
        """缺 `call_id` 的 pending 项不入 runtime 规格列表，避免下游无法关联 tool 消息。"""
        cc = ConversationContext(
            openai_messages=[],
            pending_tool_calls=[{"name": "x", "arguments": {}}],
        )
        _msgs, specs, _tok, _skills = cc.unpack_for_openai_runtime()
        self.assertEqual(specs, [])


if __name__ == "__main__":
    unittest.main()
