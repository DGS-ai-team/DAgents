"""AgentRef 纵向编排测试：session / turn / HITL / tool cancel。"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.storage.sqlite import SQLiteDatabase  # noqa: E402
from manage.workgroup.d3_models import HITLResolveRequest, HumanPostRequest  # noqa: E402
from manage.workgroup.models import (  # noqa: E402
    ACLPatchRequest,
    AssignCreateRequest,
    MemberCreateRequest,
    WorkGroupCreateRequest,
)
from manage.workgroup.store import WorkGroupStore  # noqa: E402
from manage.workgroup.vertical import VerticalLoop  # noqa: E402


class WorkgroupVerticalTests(unittest.TestCase):
    def _setup(self, tmp: str) -> tuple[WorkGroupStore, VerticalLoop, str, str]:
        store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "manage.db"))
        group, _ = store.create_workgroup(
            WorkGroupCreateRequest(display_name="Demo", created_by_node_id="node-a")
        )
        store.patch_acl(
            group.workgroup_id,
            ACLPatchRequest(collaborators=["node-b"], expected_revision=1),
        )
        member = store.create_member(
            group.workgroup_id,
            MemberCreateRequest(
                agent_id="agent-b",
                home_node_id="node-b",
                display_name="reader",
                description="读取资料",
            ),
        )
        store.publish_workgroup(group.workgroup_id)
        store.mark_member_status(member.member_id, "ready", workgroup_id=group.workgroup_id)
        return store, VerticalLoop(store), group.workgroup_id, member.member_id

    def test_session_open_and_turn_start_use_agent_ref_identity(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, loop, wid, mid = self._setup(tmp)
            opened = loop.enqueue_agent_session_open(wid, mid)
            self.assertEqual(opened.type, "agent.session.open")
            self.assertEqual(opened.payload["agent_id"], "agent-b")
            self.assertEqual(opened.payload["session_id"], f"wg:{wid}:member:{mid}")

            assign = store.create_assign(
                wid,
                AssignCreateRequest(member_id=mid, instruction="读 README"),
            )
            started = loop.enqueue_agent_turn_start(wid, assign.assign_id)
            self.assertEqual(started.type, "agent.turn.start")
            self.assertEqual(started.payload["assign_id"], assign.assign_id)
            self.assertEqual(started.payload["user_message"], "读 README")

    def test_human_message_is_deduplicated_and_agent_result_is_persisted(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, loop, wid, mid = self._setup(tmp)
            first = loop.post_human(
                wid,
                HumanPostRequest(
                    text="请读取 README",
                    client_message_id="cm_01",
                    from_node_id="node-a",
                ),
            )
            second = loop.post_human(
                wid,
                HumanPostRequest(
                    text="重复发送",
                    client_message_id="cm_01",
                    from_node_id="node-a",
                ),
            )
            self.assertEqual(first.event_id, second.event_id)

            assign = store.create_assign(
                wid,
                AssignCreateRequest(member_id=mid, instruction="读取 README"),
            )
            loop.handle_inbound(
                "node-b",
                "agent.turn.result",
                {
                    "workgroup_id": wid,
                    "member_id": mid,
                    "agent_id": "agent-b",
                    "session_id": f"wg:{wid}:member:{mid}",
                    "assign_id": assign.assign_id,
                    "status": "succeeded",
                    "final_text": "README 已读取",
                },
            )
            events = store.list_timeline(wid)
            finals = [event for event in events if event.type == "actor_final_text"]
            self.assertEqual(len(finals), 1)
            self.assertEqual(finals[0].text, "README 已读取")

    def test_agent_tool_cancel_requires_a_running_tool_event(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, loop, wid, mid = self._setup(tmp)
            assign = store.create_assign(
                wid,
                AssignCreateRequest(member_id=mid, instruction="执行命令"),
            )
            store.set_assign_status(assign.assign_id, "running")
            store.append_timeline(
                wid,
                type="tool_started",
                actor_id=mid,
                assign_id=assign.assign_id,
                tool_call_id="call-1",
                tool_name="bash",
                status="running",
            )
            frame = loop.enqueue_agent_tool_cancel(wid, assign.assign_id, "call-1")
            self.assertEqual(frame.type, "agent.tool.cancel")
            self.assertEqual(frame.payload["tool_name"], "bash")
            self.assertEqual(frame.payload["tool_call_id"], "call-1")

    def test_assign_identity_separates_direct_parent_and_child_turns(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, loop, wid, mid = self._setup(tmp)
            assign = store.create_assign(
                wid,
                AssignCreateRequest(
                    member_id=mid,
                    source="direct_member",
                    parent_turn_id="tr_parent",
                    instruction="直接执行任务",
                ),
            )
            self.assertIsNone(assign.leader_tool_call_id)
            self.assertEqual(assign.source, "direct_member")
            self.assertEqual(assign.parent_turn_id, "tr_parent")
            self.assertNotEqual(assign.child_turn_id, assign.assign_id)
            self.assertTrue(assign.attempt_id)
            frame = loop.enqueue_agent_turn_start(wid, assign.assign_id)
            self.assertEqual(frame.payload["child_turn_id"], assign.child_turn_id)
            self.assertEqual(frame.payload["attempt_id"], assign.attempt_id)
            self.assertNotIn("turn_id", frame.payload)

    def test_member_events_are_durable_without_hub_and_duplicate_free(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, loop, wid, mid = self._setup(tmp)
            assign = store.create_assign(
                wid,
                AssignCreateRequest(member_id=mid, instruction="执行工具"),
            )
            payload = {
                "workgroup_id": wid,
                "member_id": mid,
                "agent_id": "agent-b",
                "session_id": f"wg:{wid}:member:{mid}",
                "assign_id": assign.assign_id,
                "child_turn_id": assign.child_turn_id,
                "attempt_id": assign.attempt_id,
                "event_type": "tool_call",
                "event_seq": 7,
                "data": {
                    "tool_calls": [
                        {"id": "call-1", "function": {"name": "bash_run"}},
                    ]
                },
            }
            loop.handle_inbound("node-b", "agent.turn.event", payload)
            loop.handle_inbound("node-b", "agent.turn.event", payload)
            starts = [event for event in store.list_timeline(wid) if event.type == "tool_started"]
            self.assertEqual(len(starts), 1)
            self.assertEqual(store.get_assign(assign.assign_id).last_event_seq, 7)

    def test_member_approval_is_distinct_from_user_question(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, loop, wid, mid = self._setup(tmp)
            assign = store.create_assign(
                wid,
                AssignCreateRequest(member_id=mid, instruction="需要审批的任务"),
            )
            payload = {
                "workgroup_id": wid,
                "member_id": mid,
                "agent_id": "agent-b",
                "session_id": f"wg:{wid}:member:{mid}",
                "assign_id": assign.assign_id,
                "child_turn_id": assign.child_turn_id,
                "attempt_id": assign.attempt_id,
                "event_type": "hitl_required",
                "event_seq": 9,
                "data": {
                    "hitl_id": "node-hitl-1",
                    "message": "确认执行命令",
                    "items": [{"id": "call-1", "name": "bash_run"}],
                },
            }
            loop.handle_inbound("node-b", "agent.turn.event", payload)
            hitl = store.list_hitl(wid, pending_only=True)[0]
            self.assertEqual(hitl.kind, "tool_approval")
            self.assertEqual(store.get_assign(assign.assign_id).status, "awaiting_hitl")

    def test_agent_ref_hitl_resolution_emits_resume_frame(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, loop, wid, mid = self._setup(tmp)
            assign = store.create_assign(
                wid,
                AssignCreateRequest(member_id=mid, instruction="成员审批任务"),
            )
            hitl = store.create_hitl(
                wid,
                prompt="成员请求确认",
                metadata={
                    "source": "agent_ref",
                    "node_hitl_id": "node-hitl-1",
                    "member_id": mid,
                    "agent_id": "agent-b",
                    "session_id": f"wg:{wid}:member:{mid}",
                    "assign_id": assign.assign_id,
                    "home_node_id": "node-b",
                    "items": [
                        {
                            "hitl_type": "user_information",
                            "id": "call-question",
                            "name": "ask_user_information",
                            "content": "部署到哪个环境？",
                        }
                    ],
                },
            )
            loop.resolve_info_hitl(
                wid,
                hitl.hitl_id,
                HITLResolveRequest(
                    resolution={
                        "type": "user_information",
                        "tool_call_id": "call-question",
                        "answer": "production",
                    }
                ),
            )
            frames = [item for item in store.list_outbox(wid) if item.type == "agent.turn.resume"]
            self.assertEqual(len(frames), 1)
            self.assertEqual(frames[0].payload["resume_value"]["type"], "user_information")
            self.assertEqual(frames[0].payload["resume_value"]["answer"], "production")

    def test_archive_closes_agent_session(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, loop, wid, mid = self._setup(tmp)
            archived = store.archive_member(wid, mid)
            self.assertEqual(archived.status, "archived")
            frame = loop.enqueue_agent_session_close(wid, mid)
            self.assertEqual(frame.type, "agent.session.close")
            self.assertEqual(frame.payload["agent_id"], "agent-b")


if __name__ == "__main__":
    unittest.main()
