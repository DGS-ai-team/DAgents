"""Workgroup D1 store / digest / ACL 单元测试。"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.storage.sqlite import SQLiteDatabase  # noqa: E402
from manage.workgroup.errors import WorkgroupError  # noqa: E402
from manage.workgroup.models import (  # noqa: E402
    AssignCreateRequest,
    ACLPatchRequest,
    ActorRunCreateRequest,
    MemberCreateRequest,
    MemberPatchRequest,
    WorkGroupCreateRequest,
)
from manage.workgroup.store import WorkGroupStore  # noqa: E402
from manage.workgroup.turn_kernel import TurnKernel  # noqa: E402


class WorkgroupStoreTests(unittest.TestCase):
    def _store(self, tmp: str) -> WorkGroupStore:
        root = Path(tmp)
        db = SQLiteDatabase(root / "manage.db")
        return WorkGroupStore(db=db)

    def test_create_group_acl_member_assign(self) -> None:
        with TemporaryDirectory() as tmp:
            store = self._store(tmp)
            group, acl = store.create_workgroup(
                WorkGroupCreateRequest(
                    display_name="Demo",
                    created_by_node_id="node-a",
                    llm_profile_id="default",
                    llm_profile_revision="1",
                )
            )
            self.assertEqual(group.status, "configuring")
            self.assertEqual(acl.owners, ["node-a"])
            self.assertEqual(acl.revision, 1)

            # 先把 home node 加进 ACL
            acl = store.patch_acl(
                group.workgroup_id,
                ACLPatchRequest(collaborators=["node-b"], expected_revision=1),
            )
            self.assertEqual(acl.revision, 2)
            self.assertIn("node-b", acl.collaborators)

            member = store.create_member(
                group.workgroup_id,
                MemberCreateRequest(
                    agent_id="agent-b",
                    home_node_id="node-b",
                    display_name="worker",
                ),
            )
            self.assertEqual(member.status, "provisioning")
            ctx = store.member_execution_context(member.member_id)
            self.assertEqual(ctx["home_node_id"], "node-b")

            # 未发布不可派发
            with self.assertRaises(WorkgroupError) as ctx_err:
                store.create_assign(
                    group.workgroup_id,
                    AssignCreateRequest(member_id=member.member_id, instruction="read x"),
                )
            self.assertEqual(ctx_err.exception.code, "workgroup_not_published")

            store.publish_workgroup(group.workgroup_id)
            self.assertEqual(store.get_workgroup(group.workgroup_id).status, "active")

            # 创建成员后即可派发（无需 Grant）
            assign = store.create_assign(
                group.workgroup_id,
                AssignCreateRequest(member_id=member.member_id, instruction="read x"),
            )
            self.assertEqual(assign.status, "queued")

    def test_agent_ref_member_is_session_bound(self) -> None:
        with TemporaryDirectory() as tmp:
            store = self._store(tmp)
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(display_name="AgentRef", created_by_node_id="node-a")
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
                    display_name="Existing Agent",
                ),
            )
            self.assertEqual(member.agent_id, "agent-b")
            self.assertTrue(member.session_id)
            ctx = store.member_execution_context(member.member_id)
            self.assertEqual(ctx["agent_id"], "agent-b")
            self.assertEqual(ctx["session_id"], member.session_id)

    def test_acl_revision_cas(self) -> None:
        with TemporaryDirectory() as tmp:
            store = self._store(tmp)
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(display_name="G", created_by_node_id="node-a")
            )
            with self.assertRaises(WorkgroupError) as ctx:
                store.patch_acl(
                    group.workgroup_id,
                    ACLPatchRequest(collaborators=["node-b"], expected_revision=9),
                )
            self.assertEqual(ctx.exception.code, "conflict")

    def test_persist_reload(self) -> None:
        with TemporaryDirectory() as tmp:
            root = Path(tmp)
            path = root / "manage.db"
            store = WorkGroupStore(db=SQLiteDatabase(path))
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(display_name="Persist", created_by_node_id="node-a")
            )
            wid = group.workgroup_id
            store2 = WorkGroupStore(db=SQLiteDatabase(path))
            loaded = store2.get_workgroup(wid)
            self.assertIsNotNone(loaded)
            assert loaded is not None
            self.assertEqual(loaded.display_name, "Persist")

    def test_bound_pending_hitl_survives_restart_and_marks_run_awaiting(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            root = Path(tmp)
            path = root / "manage.db"
            store = WorkGroupStore(db=SQLiteDatabase(path))
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(display_name="HITL restart", created_by_node_id="node-a")
            )
            store.publish_workgroup(group.workgroup_id)
            run = store.create_actor_run(
                group.workgroup_id,
                ActorRunCreateRequest(actor_id="leader"),
            )
            hitl = store.create_hitl(
                group.workgroup_id,
                prompt="confirm",
                run_id=run.run_id,
                tool_call_id="call_hitl_restart",
            )

            restarted = WorkGroupStore(db=SQLiteDatabase(path))
            loaded = restarted.get_hitl(hitl.hitl_id)
            self.assertIsNotNone(loaded)
            assert loaded is not None
            self.assertEqual(loaded.run_id, run.run_id)
            self.assertEqual(loaded.tool_call_id, "call_hitl_restart")
            restarted.reconcile_inflight_runs()
            recovered_run = restarted.get_actor_run(run.run_id)
            self.assertIsNotNone(recovered_run)
            assert recovered_run is not None
            self.assertEqual(recovered_run.status, "awaiting_hitl")

            resolved = restarted.resolve_hitl_cas(
                group.workgroup_id,
                hitl.hitl_id,
                resolution={"answer": "yes"},
            )
            self.assertEqual(resolved.status, "resolved")
            restarted_again = WorkGroupStore(db=SQLiteDatabase(path))
            restarted_again.reconcile_inflight_runs()
            crash_window_run = restarted_again.get_actor_run(run.run_id)
            self.assertIsNotNone(crash_window_run)
            assert crash_window_run is not None
            self.assertEqual(crash_window_run.status, "awaiting_hitl")

    def test_projector_empty_run(self) -> None:
        with TemporaryDirectory() as tmp:
            store = self._store(tmp)
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(display_name="G", created_by_node_id="node-a")
            )
            store.publish_workgroup(group.workgroup_id)
            kernel = TurnKernel(store)
            proj = kernel.project(actor_id="leader")
            self.assertEqual(proj["actor_id"], "leader")
            self.assertIn("messages", proj)

    def test_update_member_updates_display_fields(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = self._store(tmp)
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(display_name="U", created_by_node_id="node-a")
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
            updated = store.update_member(
                group.workgroup_id,
                member.member_id,
                MemberPatchRequest(
                    display_name="writer",
                ),
            )
            self.assertEqual(updated.display_name, "writer")
            self.assertEqual(updated.status, "provisioning")


if __name__ == "__main__":
    unittest.main()
