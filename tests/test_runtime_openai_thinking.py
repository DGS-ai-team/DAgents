"""DeepSeek 思考模式：`reasoning_content` 流式累积与历史回传。"""

from __future__ import annotations

import unittest
from typing import Any
from unittest.mock import AsyncMock, MagicMock, patch

from app.context.models import OpenAIConversationContext, _openai_messages_to_message_records
from app.core.main_agent.runtime_openai import OpenAIImplicitReActRuntime


class RunTurnReasoningPersistenceTests(unittest.IsolatedAsyncioTestCase):
  async def test_tool_call_turn_persists_reasoning_content_in_messages(self) -> None:
    runtime = object.__new__(OpenAIImplicitReActRuntime)
    runtime._max_tool_loops = 16
    runtime._stream_include_usage = False
    runtime._tools_payload = []
    runtime._model_cfg = {"model": "deepseek-v4-pro", "temperature": 0.1, "extra_body": {}}

    async def fake_stream(_messages: list[dict[str, Any]], _system: str):
      yield {"kind": "reasoning_delta", "text": "think step"}
      yield {
        "kind": "final",
        "message": {
          "content": "",
          "tool_calls": [
            {
              "id": "call_1",
              "type": "function",
              "function": {"name": "bash_run", "arguments": "{}"},
            }
          ],
          "reasoning_content": "think step",
        },
        "finish_reason": "tool_calls",
      }

    runtime._request_model_stream = fake_stream  # type: ignore[method-assign]

    ctx = OpenAIConversationContext(session_id="s1", messages=[])
    events: list[str] = []
    async for ev in runtime.run_turn(ctx, request_type="human_message", content="hi"):
      events.append(ev.event_type)

    self.assertEqual(events.count("tool_call"), 1)
    assistant_rows = [m for m in ctx.messages if m.get("role") == "assistant"]
    self.assertEqual(len(assistant_rows), 1)
    self.assertEqual(assistant_rows[0].get("reasoning_content"), "think step")
    self.assertTrue(assistant_rows[0].get("tool_calls"))

  async def test_final_reply_always_includes_reasoning_content_key(self) -> None:
    runtime = object.__new__(OpenAIImplicitReActRuntime)
    runtime._max_tool_loops = 16
    runtime._stream_include_usage = False
    runtime._tools_payload = []
    runtime._model_cfg = {"model": "deepseek-v4-pro", "temperature": 0.1, "extra_body": {}}

    async def fake_stream(_messages: list[dict[str, Any]], _system: str):
      yield {
        "kind": "final",
        "message": {"content": "hello", "tool_calls": [], "reasoning_content": ""},
        "finish_reason": "stop",
      }

    runtime._request_model_stream = fake_stream  # type: ignore[method-assign]

    ctx = OpenAIConversationContext(session_id="s1", messages=[])
    async for _ in runtime.run_turn(ctx, request_type="human_message", content="hi"):
      pass

    assistant_rows = [m for m in ctx.messages if m.get("role") == "assistant"]
    self.assertEqual(len(assistant_rows), 1)
    self.assertIn("reasoning_content", assistant_rows[0])
    self.assertEqual(assistant_rows[0].get("reasoning_content"), "")

  async def test_request_model_stream_accumulates_reasoning_across_chunks(self) -> None:
    runtime = object.__new__(OpenAIImplicitReActRuntime)
    runtime._client = MagicMock()
    runtime._model_cfg = {
      "model": "deepseek-v4-pro",
      "temperature": 0.1,
      "extra_body": {},
    }
    runtime._tools_payload = []
    runtime._stream_include_usage = False

    chunk_reason_1 = MagicMock()
    chunk_reason_1.usage = None
    chunk_reason_1.choices = [MagicMock()]
    chunk_reason_1.choices[0].finish_reason = None
    chunk_reason_1.choices[0].delta = MagicMock(
      role="assistant", content=None, tool_calls=[], reasoning_content="part-a"
    )

    chunk_tool = MagicMock()
    chunk_tool.usage = None
    chunk_tool.choices = [MagicMock()]
    chunk_tool.choices[0].finish_reason = None
    chunk_tool.choices[0].delta = MagicMock(
      role="assistant",
      content=None,
      reasoning_content=None,
      tool_calls=[
        MagicMock(
          index=0,
          id="call_1",
          type="function",
          function=MagicMock(name="bash_run", arguments="{}"),
        )
      ],
    )

    chunk_reason_2 = MagicMock()
    chunk_reason_2.usage = None
    chunk_reason_2.choices = [MagicMock()]
    chunk_reason_2.choices[0].finish_reason = "tool_calls"
    chunk_reason_2.choices[0].delta = MagicMock(
      role="assistant", content=None, tool_calls=[], reasoning_content=" part-b"
    )

    async def async_iter():
      for item in (chunk_reason_1, chunk_tool, chunk_reason_2):
        yield item

    runtime._client.chat.completions.create = AsyncMock(return_value=async_iter())

    finals: list[dict[str, Any]] = []
    with patch(
      "app.core.main_agent.runtime_openai.get_settings",
      return_value=MagicMock(llm_stream_include_usage=False),
    ):
      async for event in runtime._request_model_stream([], "sys"):
        if event.get("kind") == "final":
          finals.append(event["message"])

    self.assertEqual(len(finals), 1)
    self.assertEqual(finals[0].get("reasoning_content"), "part-a part-b")
    self.assertIn("reasoning_content", finals[0])
    self.assertTrue(finals[0].get("tool_calls"))


class MessageRecordReasoningTests(unittest.TestCase):
  def test_openai_messages_to_records_preserves_reasoning_meta(self) -> None:
    records = _openai_messages_to_message_records(
      [
        {
          "role": "assistant",
          "content": "",
          "reasoning_content": "hidden",
          "tool_calls": [{"id": "c1"}],
        }
      ]
    )
    self.assertEqual(records[0].meta.get("reasoning_content"), "hidden")


if __name__ == "__main__":
  unittest.main()
