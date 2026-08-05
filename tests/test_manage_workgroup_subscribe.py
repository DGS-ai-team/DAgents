"""D4：工作组持久订阅（ACL 门禁 + 创建者自动订阅）。"""

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
from manage.workgroup.models import ACLPatchRequest, WorkGroupCreateRequest  # noqa: E402
from manage.workgroup.store import WorkGroupStore  # noqa: E402


class WorkgroupSubscribeTests(unittest.TestCase):
    def test_creator_auto_subscribe_and_acl_gate(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(display_name="Demo", created_by_node_id="node-a")
            )
            wid = group.workgroup_id
            self.assertTrue(store.is_subscribed(wid, "node-a"))
            listed = store.list_workgroups(subscribed_by="node-a")
            self.assertEqual(len(listed), 1)

            with self.assertRaises(WorkgroupError) as ctx:
                store.subscribe(wid, "node-b")
            self.assertEqual(ctx.exception.code, "not_authorized")

            store.patch_acl(wid, ACLPatchRequest(collaborators=["node-b"], expected_revision=1))
            sub = store.subscribe(wid, "node-b")
            self.assertEqual(sub.node_id, "node-b")
            self.assertEqual(len(store.list_subscribers(wid)), 2)

            store.unsubscribe(wid, "node-b")
            self.assertFalse(store.is_subscribed(wid, "node-b"))
            self.assertEqual(store.list_workgroups(subscribed_by="node-b"), [])

            # ACL 内但未订阅 → acl_member 仍可见
            acl_list = store.list_workgroups(acl_member="node-b")
            self.assertEqual(len(acl_list), 1)
            self.assertEqual(acl_list[0].workgroup_id, wid)

    def test_list_excludes_archived_by_default(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            active, _ = store.create_workgroup(
                WorkGroupCreateRequest(display_name="Live", created_by_node_id="node-a")
            )
            gone, _ = store.create_workgroup(
                WorkGroupCreateRequest(display_name="Gone", created_by_node_id="node-a")
            )
            store.begin_archive(gone.workgroup_id)
            if store.get_workgroup(gone.workgroup_id).status != "archived":
                store.begin_archive(gone.workgroup_id)

            listed = store.list_workgroups()
            self.assertEqual([g.workgroup_id for g in listed], [active.workgroup_id])

            all_groups = store.list_workgroups(include_archived=True)
            ids = {g.workgroup_id for g in all_groups}
            self.assertIn(active.workgroup_id, ids)
            self.assertIn(gone.workgroup_id, ids)


if __name__ == "__main__":
    unittest.main()

