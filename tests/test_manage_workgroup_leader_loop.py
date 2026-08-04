"""Manage Leader LLM loop（Mock）单元测试。"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.storage.sqlite import SQLiteDatabase  # noqa: E402
from manage.workgroup.llm_chat import MockLLMClient  # noqa: E402
from manage.workgroup.models import (  # noqa: E402
    ACLPatchRequest,
    GrantInviteRequest,
    MemberCreateRequest,
    WorkGroupCreateRequest,
)
from manage.workgroup.store import WorkGroupStore  # noqa: E402
from manage.workgroup.turn_kernel import (  # noqa: E402
    TurnKernel,
    mock_leader_script_assign_then_answer,
)


class LeaderLoopTests(unittest.TestCase):
    def _ready_group(self, store: WorkGroupStore) -> tuple[str, str]:
        group, _ = store.create_workgroup(
            WorkGroupCreateRequest(
                display_name="Loop",
                created_by_node_id="node-a",
                llm_profile_id="mock",
                llm_profile_revision="1",
            )
        )
        store.patch_acl(
            group.workgroup_id,
            ACLPatchRequest(collaborators=["node-b"], expected_revision=1),
        )
        member, spec = store.create_member(
            group.workgroup_id,
            MemberCreateRequest(
                home_node_id="node-b",
                display_name="worker",
                allow_tool_names=["read_file"],
            ),
        )
        grant = store.invite_grant(
            group.workgroup_id,
            GrantInviteRequest(member_id=member.member_id, tool_allow_names=["read_file"]),
        )
        store.accept_grant(grant.grant_id, home_node_id="node-b", member_spec_digest=spec.digest)
        store.mark_member_status(member.member_id, "ready", workgroup_id=group.workgroup_id)
        return group.workgroup_id, member.member_id

    def test_human_message_drives_assign_then_final(self) -> None:
        with TemporaryDirectory() as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            wid, mid = self._ready_group(store)
            script = mock_leader_script_assign_then_answer(
                member_id=mid,
                instruction="读 README",
                final_text="README 已读完",
            )
            kernel = TurnKernel(store, chat_client=MockLLMClient(script), mock_llm=True)
            result = kernel.handle_human_message(
                wid,
                text="请安排成员读 README",
                from_node_id="node-a",
            )
            self.assertEqual(result["loop"]["status"], "succeeded")
            self.assertEqual(result["loop"]["final_text"], "README 已读完")
            self.assertEqual(result["leader_run"].status, "succeeded")

            timeline = store.list_timeline(wid)
            types = [e.type for e in timeline]
            self.assertEqual(types[0], "human_message")
            self.assertIn("actor_final_text", types)
            # member scripted final + leader final
            actor_finals = [e for e in timeline if e.type == "actor_final_text"]
            self.assertGreaterEqual(len(actor_finals), 2)

            hist = store.get_run_history(result["leader_run"].run_id)
            assert hist is not None
            roles = [m.role for m in hist.messages]
            self.assertEqual(roles.count("assistant"), 2)
            self.assertEqual(roles.count("tool"), 1)
            tool = next(m for m in hist.messages if m.role == "tool")
            self.assertEqual(tool.name, "assign_workgroup_task")
            self.assertIn("[scripted] 读 README", tool.content or "")
            self.assertIn("\"status\": \"succeeded\"", tool.content or "")

    def test_single_active_assign_enforced(self) -> None:
        with TemporaryDirectory() as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            wid, mid = self._ready_group(store)
            from manage.workgroup.models import AssignCreateRequest
            from manage.workgroup.errors import WorkgroupError

            store.create_assign(wid, AssignCreateRequest(member_id=mid, instruction="a"))
            with self.assertRaises(WorkgroupError) as ctx:
                store.create_assign(wid, AssignCreateRequest(member_id=mid, instruction="b"))
            self.assertEqual(ctx.exception.code, "conflict")


if __name__ == "__main__":
    unittest.main()
