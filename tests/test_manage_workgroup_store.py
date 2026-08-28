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
from manage.workgroup.digest import sha256_digest  # noqa: E402
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


class WorkgroupDigestTests(unittest.TestCase):
    def test_digest_excludes_self_and_is_stable(self) -> None:
        payload = {
            "b": 2,
            "a": {"z": 1, "y": [3, 2]},
            "digest": "sha256:deadbeef",
        }
        d1 = sha256_digest(payload)
        d2 = sha256_digest({**payload, "digest": "sha256:other"})
        self.assertEqual(d1, d2)
        self.assertTrue(d1.startswith("sha256:"))
        self.assertEqual(len(d1), len("sha256:") + 64)


class WorkgroupStoreTests(unittest.TestCase):
    def _store(self, tmp: str) -> WorkGroupStore:
        root = Path(tmp)
        db = SQLiteDatabase(root / "manage.db")
        return WorkGroupStore(db=db, workspaces_dir=root / "workgroup-workspaces")

    def test_create_group_materializes_workspace(self) -> None:
        with TemporaryDirectory() as tmp:
            store = self._store(tmp)
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(
                    display_name="WS",
                    created_by_node_id="node-a",
                )
            )
            self.assertEqual(group.workspace.root_kind, "workgroup_workspace")
            self.assertTrue(group.workspace.path)
            ws = Path(group.workspace.path)
            self.assertTrue(ws.is_dir())
            self.assertTrue((ws / "data").is_dir())
            self.assertTrue((ws / "README.md").is_file())
            runtime = store.workgroup_runtime(group.workgroup_id)
            self.assertEqual(runtime.get("workspace_path"), group.workspace.path)

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

            member, spec = store.create_member(
                group.workgroup_id,
                MemberCreateRequest(
                    home_node_id="node-b",
                    display_name="worker",
                    allow_tool_names=["read_file", "bash"],
                ),
            )
            self.assertEqual(member.status, "provisioning")
            self.assertEqual(spec.digest, member.member_spec_digest)
            self.assertEqual(spec.skills, "disabled")
            self.assertFalse(spec.memory.remember_enabled)
            ctx = store.member_execution_context(member.member_id)
            self.assertEqual(ctx["home_node_id"], "node-b")
            self.assertEqual(ctx["tool_allow_names"], ["read_file", "bash"])
            self.assertTrue(ctx["lease_id"])
            self.assertEqual(ctx["lease_epoch"], 1)

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
            member, spec = store.create_member(
                group.workgroup_id,
                MemberCreateRequest(
                    agent_id="agent-b",
                    home_node_id="node-b",
                    display_name="Existing Agent",
                ),
            )
            self.assertEqual(member.execution_mode, "agent_ref")
            self.assertEqual(member.agent_id, "agent-b")
            self.assertTrue(member.session_id)
            self.assertEqual(spec.agent_id, "agent-b")
            ctx = store.member_execution_context(member.member_id)
            self.assertEqual(ctx["execution_mode"], "agent_ref")
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
            ws_dir = root / "workgroup-workspaces"
            store = WorkGroupStore(db=SQLiteDatabase(path), workspaces_dir=ws_dir)
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(display_name="Persist", created_by_node_id="node-a")
            )
            wid = group.workgroup_id
            ws_path = group.workspace.path
            self.assertTrue(ws_path)
            store2 = WorkGroupStore(db=SQLiteDatabase(path), workspaces_dir=ws_dir)
            loaded = store2.get_workgroup(wid)
            self.assertIsNotNone(loaded)
            assert loaded is not None
            self.assertEqual(loaded.display_name, "Persist")
            self.assertEqual(loaded.workspace.path, ws_path)
            self.assertEqual(
                store2.workgroup_runtime(wid).get("workspace_path"),
                ws_path,
            )

    def test_bound_pending_hitl_survives_restart_and_marks_run_awaiting(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            root = Path(tmp)
            path = root / "manage.db"
            ws_dir = root / "workgroup-workspaces"
            store = WorkGroupStore(db=SQLiteDatabase(path), workspaces_dir=ws_dir)
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

            restarted = WorkGroupStore(db=SQLiteDatabase(path), workspaces_dir=ws_dir)
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
            restarted_again = WorkGroupStore(db=SQLiteDatabase(path), workspaces_dir=ws_dir)
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

    def test_update_member_bumps_generation(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = self._store(tmp)
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(display_name="U", created_by_node_id="node-a")
            )
            store.patch_acl(
                group.workgroup_id,
                ACLPatchRequest(collaborators=["node-b"], expected_revision=1),
            )
            member, spec = store.create_member(
                group.workgroup_id,
                MemberCreateRequest(
                    home_node_id="node-b",
                    display_name="reader",
                    allow_tool_names=["read_file"],
                ),
            )
            self.assertEqual(member.member_generation, 1)
            updated, new_spec = store.update_member(
                group.workgroup_id,
                member.member_id,
                MemberPatchRequest(
                    display_name="writer",
                    allow_tool_names=["read_file", "write_file"],
                ),
            )
            self.assertEqual(updated.display_name, "writer")
            self.assertEqual(updated.member_generation, 2)
            self.assertEqual(updated.status, "provisioning")
            self.assertNotEqual(new_spec.digest, spec.digest)
            self.assertEqual(new_spec.tools.allow_names, ["read_file", "write_file"])


if __name__ == "__main__":
    unittest.main()
