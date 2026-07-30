"""Workgroup D1 store / digest / ACL≠Grant 单元测试。"""

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
    GrantInviteRequest,
    MemberCreateRequest,
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
        db = SQLiteDatabase(Path(tmp) / "manage.db")
        return WorkGroupStore(db=db)

    def test_create_group_acl_member_grant_assign(self) -> None:
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
            self.assertEqual(group.status, "active")
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
            self.assertEqual(member.status, "requested")
            self.assertEqual(spec.digest, member.member_spec_digest)
            self.assertEqual(spec.skills, "disabled")
            self.assertFalse(spec.memory.remember_enabled)

            # ACL alone cannot assign
            with self.assertRaises(WorkgroupError) as ctx:
                store.create_assign(
                    group.workgroup_id,
                    AssignCreateRequest(member_id=member.member_id, instruction="read x"),
                )
            self.assertEqual(ctx.exception.code, "not_authorized")

            grant = store.invite_grant(
                group.workgroup_id,
                GrantInviteRequest(member_id=member.member_id, tool_allow_names=["read_file"]),
            )
            self.assertEqual(grant.status, "invited")
            self.assertEqual(grant.tool_allow_names, ["read_file"])

            accepted = store.accept_grant(
                grant.grant_id,
                home_node_id="node-b",
                member_spec_digest=spec.digest,
            )
            self.assertEqual(accepted.status, "accepted")
            refreshed = store.get_member(member.member_id)
            assert refreshed is not None
            self.assertEqual(refreshed.status, "provisioning")

            assign = store.create_assign(
                group.workgroup_id,
                AssignCreateRequest(member_id=member.member_id, instruction="read x"),
            )
            self.assertEqual(assign.status, "queued")

    def test_grant_tool_subset_enforced(self) -> None:
        with TemporaryDirectory() as tmp:
            store = self._store(tmp)
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(display_name="G", created_by_node_id="node-a")
            )
            store.patch_acl(
                group.workgroup_id,
                ACLPatchRequest(collaborators=["node-b"], expected_revision=1),
            )
            member, _ = store.create_member(
                group.workgroup_id,
                MemberCreateRequest(
                    home_node_id="node-b",
                    display_name="w",
                    allow_tool_names=["read_file"],
                ),
            )
            with self.assertRaises(WorkgroupError) as ctx:
                store.invite_grant(
                    group.workgroup_id,
                    GrantInviteRequest(member_id=member.member_id, tool_allow_names=["bash"]),
                )
            self.assertEqual(ctx.exception.code, "digest_mismatch")

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
            path = Path(tmp) / "manage.db"
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

    def test_hitl_cas_skeleton(self) -> None:
        with TemporaryDirectory() as tmp:
            store = self._store(tmp)
            kernel = TurnKernel(store)
            first = kernel.resolve_hitl_cas("ht_test", resolution={"ok": True})
            self.assertEqual(first["status"], "resolved")
            with self.assertRaises(WorkgroupError) as ctx:
                kernel.resolve_hitl_cas("ht_test", resolution={"ok": False})
            self.assertEqual(ctx.exception.code, "already_resolved")


if __name__ == "__main__":
    unittest.main()
