"""D3 WS：gap-fill、dup connection fence、cursor_too_old、Manage 重启 outbox。"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.storage.sqlite import SQLiteDatabase  # noqa: E402
from manage.workgroup.d3_models import OutboxFrame  # noqa: E402
from manage.workgroup.models import (  # noqa: E402
    ACLPatchRequest,
    AssignCreateRequest,
    MemberCreateRequest,
    WorkGroupCreateRequest,
)
from manage.workgroup.store import WorkGroupStore  # noqa: E402
from manage.workgroup.ws_hub import WorkgroupWSHub  # noqa: E402


class WorkgroupWSHubTests(unittest.TestCase):
    def test_gap_fill_and_dup_connection_fence(self) -> None:
        """对齐 fixtures/workgroup-d05/ws/gap_fill_and_dup_connection_fence.json"""
        with TemporaryDirectory() as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            hub = WorkgroupWSHub(store=store)
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(
                    display_name="ws",
                    created_by_node_id="node_a",
                )
            )
            wid = group.workgroup_id
            store._outbox[wid] = [  # noqa: SLF001
                OutboxFrame(
                    delivery_seq=41,
                    workgroup_id=wid,
                    type="tool.command",
                    payload={"command_id": "cmd_a"},
                    created_at="2026-07-31T00:00:00Z",
                ),
                OutboxFrame(
                    delivery_seq=42,
                    workgroup_id=wid,
                    type="tool.command",
                    payload={"command_id": "cmd_b"},
                    created_at="2026-07-31T00:00:01Z",
                ),
            ]

            # conn1 @ gen 5
            hub._max_generation["node_a"] = 5  # noqa: SLF001
            sent: list[dict] = []
            # 断线后以 gen 6 重连并 resume from 40
            hub._conns.clear()  # noqa: SLF001
            welcome = hub.hello("node_a", last_ack_delivery_seq=40, send=sent.append)
            self.assertEqual(welcome["payload"]["connection_generation"], 6)

            result = hub.resume_offer("node_a", workgroup_id=wid, last_ack_delivery_seq=40)
            self.assertEqual(result["complete"]["payload"]["replayed"], [41, 42])
            self.assertEqual([e["delivery_seq"] for e in result["envelopes"]], [41, 42])

            # 旧 conn1 gen=5 迟到帧
            late = hub.handle_late_frame("node_a", connection_generation=5)
            self.assertEqual(late["code"], "fencing_rejected")

    def test_cursor_too_old_resync(self) -> None:
        store = WorkGroupStore()
        hub = WorkgroupWSHub(store=store, outbox_retention_from=100)
        group, _ = store.create_workgroup(
            WorkGroupCreateRequest(display_name="ws", created_by_node_id="node_a")
        )
        hub.hello("node_a", last_ack_delivery_seq=1, send=lambda _m: None)
        err = hub.resume_offer(
            "node_a",
            workgroup_id=group.workgroup_id,
            last_ack_delivery_seq=1,
        )
        self.assertEqual(err["type"], "resume.error")
        self.assertEqual(err["payload"]["code"], "cursor_too_old")
        self.assertEqual(err["payload"]["action"], "resync_snapshot_then_resume")

    def test_manage_restart_pending_assign(self) -> None:
        with TemporaryDirectory() as tmp:
            db_path = Path(tmp) / "m.db"
            store = WorkGroupStore(db=SQLiteDatabase(db_path))
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(display_name="ws", created_by_node_id="node_a")
            )
            wid = group.workgroup_id
            store.patch_acl(wid, ACLPatchRequest(collaborators=["node_b"], expected_revision=1))
            member, spec = store.create_member(
                wid,
                MemberCreateRequest(
                    home_node_id="node_b",
                    display_name="reader",
                    allow_tool_names=["read_file"],
                ),
            )
            _ = spec
            store.mark_member_status(member.member_id, "ready", workgroup_id=wid)
            store.publish_workgroup(wid)
            assign = store.create_assign(
                wid, AssignCreateRequest(member_id=member.member_id, instruction="read")
            )
            store.set_assign_status(assign.assign_id, "running")
            frame = store.enqueue_outbox(
                wid,
                type="tool.command",
                payload={"command_id": "cmd_01h00000000000000000000008"},
            )
            self.assertFalse(frame.acked)

            store2 = WorkGroupStore(db=SQLiteDatabase(db_path))
            hub2 = WorkgroupWSHub(store=store2)
            reloaded = store2.get_assign(assign.assign_id)
            self.assertIsNotNone(reloaded)
            assert reloaded is not None
            self.assertEqual(reloaded.status, "running")
            unacked = hub2.reconcile_unacked(wid)
            self.assertEqual(len(unacked), 1)
            self.assertEqual(unacked[0].payload["command_id"], "cmd_01h00000000000000000000008")
            assigns = [a for a in store2._assigns.values() if a.workgroup_id == wid]  # noqa: SLF001
            self.assertEqual(len(assigns), 1)


if __name__ == "__main__":
    unittest.main()
