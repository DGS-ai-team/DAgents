"""ContextProjector / RunHistory 契约 fixture golden。"""

from __future__ import annotations

import json
import sys
import unittest
from pathlib import Path

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.workgroup.history import (  # noqa: E402
    can_invoke_llm_after_tools,
    to_provider_messages,
)
from manage.workgroup.projector import (  # noqa: E402
    count_assign_summary_mentions,
    project_actor_context,
    project_while_tools_open,
)

_FIX = _ROOT / "docs" / "design" / "fixtures" / "workgroup-d05" / "projection"


def _load(name: str) -> dict:
    return json.loads((_FIX / name).read_text(encoding="utf-8"))


class ProjectorFixtureTests(unittest.TestCase):
    def test_openai_member_sees_leader(self) -> None:
        fix = _load("openai_member_sees_leader.json")
        given = fix["given"]
        out = project_actor_context(
            actor_id=given["actor_id"],
            timeline_events=given["timeline_others"],
            own_run_history=given["own_run_history"],
        )
        self.assertEqual(out["messages"], fix["then"]["messages"])

    def test_tool_name_not_overridden(self) -> None:
        fix = _load("tool_name_not_overridden.json")
        msgs = to_provider_messages(fix["given"]["input_run_messages"])
        tool_msg = next(m for m in msgs if m["role"] == "tool")
        self.assertEqual(tool_msg["name"], fix["then"]["tool_message_name"])
        for forbidden in fix["then"]["forbidden_tool_message_names"]:
            self.assertNotEqual(tool_msg["name"], forbidden)

    def test_parallel_tool_calls_must_pair_all(self) -> None:
        fix = _load("parallel_tool_calls_must_pair_all.json")
        given = fix["given"]
        ok, wait = can_invoke_llm_after_tools(given["assistant_tool_calls"], given["results_so_far"])
        self.assertEqual(ok, fix["then"]["llm_invoked_again"])
        self.assertEqual(wait, fix["then"]["wait_for"])

    def test_open_tool_call_buffers_timeline(self) -> None:
        fix = _load("open_tool_call_buffers_timeline.json")
        given = fix["given"]
        out = project_while_tools_open(
            actor_id=given["actor_id"],
            open_tool_calls=given["open_tool_calls"],
            incoming_timeline=given["incoming_timeline"],
        )
        self.assertEqual(out["inject_into_llm_context"], fix["then"]["inject_into_llm_context"])
        self.assertEqual(out["buffered"], fix["then"]["buffered"])

    def test_assign_result_deduped_vs_timeline(self) -> None:
        fix = _load("assign_result_deduped_vs_timeline.json")
        given = fix["given"]
        aid = given["assign_id"]
        history = [
            {
                "role": "assistant",
                "name": "leader",
                "content": "",
                "tool_calls": [
                    {
                        "id": given["leader_tool_result"]["tool_call_id"],
                        "type": "function",
                        "function": {"name": "assign_workgroup_task", "arguments": "{}"},
                    }
                ],
            },
            {
                "role": "tool",
                "tool_call_id": given["leader_tool_result"]["tool_call_id"],
                "name": "assign_workgroup_task",
                "content": given["leader_tool_result"]["content"],
            },
        ]
        timeline = [
            {
                "seq": 1,
                "type": "actor_final_text",
                "actor_id": "mb_01h00000000000000000000002",
                "assign_id": aid,
                "content_text": given["timeline_event"]["content_text"],
                "protocol_name": "member_mb_01h00000000000000000000002",
            }
        ]
        out = project_actor_context(
            actor_id="leader",
            own_run_history=history,
            timeline_events=timeline,
        )
        count, kept = count_assign_summary_mentions(out["messages"], aid)
        self.assertEqual(count, fix["then"]["messages_with_assign_summary"])
        self.assertEqual(kept, fix["then"]["kept"])

    def test_deepseek_close_assign_tool_pairing(self) -> None:
        fix = _load("deepseek_leader_sees_member_final.json")
        given = fix["given"]
        from manage.workgroup.history import RunHistoryMessage, build_assign_tool_result_content
        from manage.workgroup.history import ToolCall, ToolCallFunction

        own = [RunHistoryMessage.model_validate(m) for m in given["own_run_history"]]
        final = given["member_final_timeline"]
        content = build_assign_tool_result_content(
            assign_id=final["assign_id"],
            status="succeeded",
            summary=final["content_text"],
        )
        tool_msg = RunHistoryMessage(
            role="tool",
            tool_call_id="call_as1",
            name="assign_workgroup_task",
            content=content,
        )
        messages = to_provider_messages(own + [tool_msg])
        # strip empty content key differences: fixture assistant has no content field
        normalized = []
        for m in messages:
            item = dict(m)
            if item.get("role") == "assistant" and item.get("content") == "":
                # fixture omits content on assistant with tool_calls — allow either
                pass
            normalized.append(item)
        expected = fix["then"]["leader_messages"]
        self.assertEqual(normalized[0]["tool_calls"], expected[0]["tool_calls"])
        self.assertEqual(normalized[1], expected[1])
        self.assertFalse(fix["then"]["timeline_user_insert_for_same_assign_id"])
        # 投影时同 assign_id 不再插入 user
        out = project_actor_context(
            actor_id="leader",
            own_run_history=own + [tool_msg],
            timeline_events=[final],
        )
        user_inserts = [m for m in out["messages"] if m.get("role") == "user" and final["assign_id"] in str(m.get("content"))]
        self.assertEqual(user_inserts, [])


if __name__ == "__main__":
    unittest.main()
