"""AgentRef assign 入口的最小契约测试。

跨节点执行由 Node Agent 的 session/turn 协议负责；Manage 不再在测试中
伪造第二套成员工具执行器。
"""

from __future__ import annotations

import sys
import threading
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.storage.sqlite import SQLiteDatabase  # noqa: E402
from manage.workgroup.models import (  # noqa: E402
    ACLPatchRequest,
    AssignCreateRequest,
    MemberCreateRequest,
    WorkGroupCreateRequest,
)
from manage.workgroup.store import WorkGroupStore  # noqa: E402
from manage.workgroup.vertical import VerticalLoop  # noqa: E402


class AgentRefAssignTests(unittest.TestCase):
    def _ready(self, tmp: str) -> tuple[WorkGroupStore, VerticalLoop, str, str]:
        store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "manage.db"))
        group, _ = store.create_workgroup(
            WorkGroupCreateRequest(display_name="Assign", created_by_node_id="node-a")
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
            ),
        )
        store.publish_workgroup(group.workgroup_id)
        store.mark_member_status(member.member_id, "ready", workgroup_id=group.workgroup_id)
        return store, VerticalLoop(store, turn_timeout_s=1.0), group.workgroup_id, member.member_id

    def test_run_agent_ref_assign_accepts_member_busy_for_same_assign(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, loop, wid, mid = self._ready(tmp)
            assign = store.create_assign(
                wid,
                AssignCreateRequest(member_id=mid, instruction="读取 README"),
            )
            loop.enqueue_agent_turn_start = lambda *_args, **_kwargs: None  # type: ignore[method-assign]
            loop.wait_agent_turn = lambda *_args, **_kwargs: {  # type: ignore[method-assign]
                "status": "succeeded",
                "final_text": "已完成",
            }
            self.assertEqual(
                loop.run_agent_ref_assign(wid, assign.assign_id, mid, assign.instruction),
                "已完成",
            )

    def test_cancel_pending_assign_sends_agent_turn_cancel_and_wakes_waiter(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, loop, wid, mid = self._ready(tmp)
            assign = store.create_assign(
                wid,
                AssignCreateRequest(member_id=mid, instruction="执行慢任务"),
            )
            with loop._lock:  # noqa: SLF001 - verify the waiter boundary
                loop._agent_waiters[assign.assign_id] = threading.Event()  # noqa: SLF001
            self.assertEqual(loop.cancel_pending_agent_turns(wid), [assign.assign_id])
            result = loop.wait_agent_turn(assign.assign_id, timeout_s=1.0)
            self.assertEqual(result["status"], "canceled")
            self.assertEqual(store.list_outbox(wid)[-1].type, "agent.turn.cancel")


if __name__ == "__main__":
    unittest.main()
