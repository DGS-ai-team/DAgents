"""OpenAIConversationContext 清空对话态单测。"""

from __future__ import annotations

import unittest

from app.context.models import OpenAIConversationContext, PendingToolCall, RunTurnPhase


class OpenAIConversationContextResetTests(unittest.TestCase):
    def test_reset_conversation_state_clears_history_and_preserves_skills(self) -> None:
        ctx = OpenAIConversationContext(
            session_id="s1",
            sse_client_id="cli-1",
            active_client_id="cli-active",
            messages=[{"role": "user", "content": "hello"}],
            pending_tool_calls=[
                PendingToolCall(call_id="c1", name="read_file", arguments={"path": "a.txt"}),
            ],
            run_turn_phase=RunTurnPhase.AWAITING_TOOL_EXECUTION,
            messages_total_tokens=42,
            tool_loop_count=3,
            loaded_skills=[{"skill_name": "debugging", "description": "debug"}],
            assistant_stream_buffer="partial",
        )

        ctx.reset_conversation_state()

        self.assertEqual(ctx.session_id, "s1")
        self.assertEqual(ctx.sse_client_id, "cli-1")
        self.assertEqual(ctx.loaded_skills, [{"skill_name": "debugging", "description": "debug"}])
        self.assertEqual(ctx.messages, [])
        self.assertEqual(ctx.pending_tool_calls, [])
        self.assertEqual(ctx.run_turn_phase, RunTurnPhase.IDLE)
        self.assertEqual(ctx.messages_total_tokens, 0)
        self.assertEqual(ctx.tool_loop_count, 0)
        self.assertEqual(ctx.assistant_stream_buffer, "")
        self.assertEqual(ctx.active_client_id, "")


if __name__ == "__main__":
    unittest.main()
