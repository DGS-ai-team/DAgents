"""D3 纵向闭环：human → provision → assign → read_file → timeline + HITL/archive。"""

from __future__ import annotations

import json
import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from typing import Any

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.storage.sqlite import SQLiteDatabase  # noqa: E402
from manage.workgroup.d3_models import (  # noqa: E402
	HITLCreateRequest,
	HITLResolveRequest,
	HumanPostRequest,
	MemberFinalRequest,
	ProvisionCompleteRequest,
)
from manage.workgroup.errors import WorkgroupError  # noqa: E402
from manage.workgroup.models import (  # noqa: E402
    ACLPatchRequest,
    MemberCreateRequest,
    WorkGroupCreateRequest,
)
from manage.workgroup.store import WorkGroupStore  # noqa: E402
from manage.workgroup.vertical import VerticalLoop  # noqa: E402


class FakeNodeBridge:
    """模拟 Node Worker：provision + read_file + tombstone fencing。"""

    def __init__(self, root: Path, node_id: str = "node-b") -> None:
        self.root = root
        self.node_id = node_id
        self.bindings: dict[str, dict[str, Any]] = {}
        self.tombstones: dict[str, dict[str, Any]] = {}
        self.executions: dict[str, int] = {}
        self.journal: dict[str, dict[str, Any]] = {}

    def provision(self, payload: dict[str, Any]) -> dict[str, Any]:
        mid = payload["member_id"]
        ws = self.root / payload["workgroup_id"] / mid
        ws.mkdir(parents=True, exist_ok=True)
        (ws / "README").write_text("# Demo\n标题是 Demo\n", encoding="utf-8")
        catalog = "rev_fake_read_file"
        self.bindings[mid] = {
            "workspace_path": str(ws),
            "tool_catalog_revision": catalog,
            "lease_epoch": int(payload["lease_epoch"]),
            "member_spec_digest": payload["member_spec_digest"],
            "tool_allow_names": list(payload.get("tool_allow_names") or []),
            "status": "ready",
        }
        return {
            "ok": True,
            "workspace_path": str(ws),
            "tool_catalog_revision": catalog,
        }

    def execute_command(self, payload: dict[str, Any]) -> dict[str, Any]:
        cmd_id = payload["command_id"]
        wg = payload["workgroup_id"]
        if wg in self.tombstones:
            tomb = self.tombstones[wg]
            if int(payload.get("lease_epoch") or 0) < int(tomb["lease_epoch_at_archive"]):
                return {
                    "status": "rejected",
                    "error_code": "fencing_rejected",
                    "result_text": "",
                }
        existing = self.journal.get(cmd_id)
        if existing is not None:
            return {
                "status": existing["status"],
                "result_text": existing.get("result_text", ""),
                "error_code": existing.get("error_code"),
                "reexec": False,
            }
        mid = payload["member_id"]
        binding = self.bindings.get(mid)
        if binding is None:
            return {"status": "failed", "error_code": "not_found", "result_text": ""}
        allow = {str(n) for n in (binding.get("tool_allow_names") or [])}
        tool_name = str(payload.get("tool_name") or "")
        if allow and tool_name not in allow:
            return {"status": "failed", "error_code": "not_authorized", "result_text": ""}
        args = json.loads(payload["arguments_json"])
        ws = Path(binding["workspace_path"])
        self.executions[cmd_id] = self.executions.get(cmd_id, 0) + 1
        try:
            if tool_name == "read_file":
                path = ws / str(args["path"])
                text = path.read_text(encoding="utf-8")
                result = {"status": "succeeded", "result_text": text, "error_code": None}
            elif tool_name == "write_file":
                path = ws / str(args["path"])
                path.parent.mkdir(parents=True, exist_ok=True)
                content = str(args.get("content") or "")
                path.write_text(content, encoding="utf-8")
                result = {
                    "status": "succeeded",
                    "result_text": f"wrote {len(content.encode('utf-8'))} bytes to {args['path']}",
                    "error_code": None,
                }
            elif tool_name == "glob_files":
                directory = str(args.get("directory") or ".")
                pattern = str(args.get("glob_pattern") or "*")
                base = ws if directory in {".", "./"} else ws / directory
                matches = sorted(str(p.relative_to(ws)).replace("\\", "/") for p in base.glob(pattern) if p.is_file())
                result = {
                    "status": "succeeded",
                    "result_text": json.dumps({"paths": matches}, ensure_ascii=False),
                    "error_code": None,
                }
            else:
                result = {"status": "failed", "error_code": "conflict", "result_text": f"unsupported {tool_name}"}
        except FileNotFoundError:
            result = {"status": "failed", "error_code": "not_found", "result_text": ""}
        except OSError as exc:
            result = {"status": "failed", "error_code": "conflict", "result_text": str(exc)}
        self.journal[cmd_id] = result
        return result

    def apply_tombstone(self, payload: dict[str, Any]) -> None:
        self.tombstones[payload["workgroup_id"]] = payload
        for b in self.bindings.values():
            b["status"] = "archived"
            b["lease_epoch"] = int(payload["lease_epoch_at_archive"])

    def local_agent_ids(self) -> list[str]:
        return []


class WorkgroupVerticalTests(unittest.TestCase):
    def _setup(self, tmp: str) -> tuple[VerticalLoop, FakeNodeBridge, str, str]:
        store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "manage.db"))
        bridge = FakeNodeBridge(Path(tmp) / "node-ws", node_id="node-b")
        loop = VerticalLoop(store, bridge=bridge)
        group, _ = store.create_workgroup(
            WorkGroupCreateRequest(
                display_name="Demo",
                created_by_node_id="node-a",
                llm_profile_id="default",
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
                display_name="reader",
                allow_tool_names=["read_file"],
                prompt={"soul_md": "reader"},
            ),
        )
        _ = spec
        loop.enqueue_provision(group.workgroup_id, member.member_id)
        store.publish_workgroup(group.workgroup_id)
        return loop, bridge, group.workgroup_id, member.member_id

    def test_two_node_read_file_happy_path(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            loop, bridge, wid, mid = self._setup(tmp)
            self.assertNotIn(mid, bridge.local_agent_ids())

            human = loop.post_human(
                wid,
                HumanPostRequest(
                    text="读 README",
                    client_message_id="01h0000000000000000000000d",
                    from_node_id="node-a",
                ),
            )
            # client_message_id 去重
            human2 = loop.post_human(
                wid,
                HumanPostRequest(
                    text="读 README again",
                    client_message_id="01h0000000000000000000000d",
                    from_node_id="node-a",
                ),
            )
            self.assertEqual(human.event_id, human2.event_id)

            dispatched = loop.assign_and_dispatch_read_file(
                wid, member_id=mid, instruction="读 README", path="README"
            )
            self.assertEqual(dispatched["tool_result"]["status"], "succeeded")
            self.assertIn("Demo", dispatched["tool_result"]["result_text"])
            self.assertTrue(dispatched.get("tool_result") is not None)

            final = loop.member_final(
                wid,
                MemberFinalRequest(
                    assign_id=dispatched["assign"].assign_id,
                    member_id=mid,
                    text="标题是 Demo",
                ),
            )
            self.assertEqual(final["assign"].status, "succeeded")
            self.assertIsNone(final["assign"].error_code)
            self.assertEqual(final["assign"].result_summary, "标题是 Demo")

            timeline = loop.store.list_timeline(wid)
            types = [e.type for e in timeline]
            self.assertEqual(types, ["human_message", "actor_final_text"])
            for ev in timeline:
                dumped = ev.model_dump()
                for forbidden in ("tool_arguments", "tool_result", "raw_tool_payload"):
                    self.assertNotIn(forbidden, dumped)
            # leader 侧 tool 配对成功且未写入 Timeline（仅 human + final）
            self.assertEqual(bridge.executions[dispatched["command"]["command_id"]], 1)

    def test_info_hitl_cas_once(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            loop, _, wid, _ = self._setup(tmp)
            hitl = loop.create_info_hitl(wid, HITLCreateRequest(prompt="确认继续？"))
            ok = loop.resolve_info_hitl(
                wid, hitl.hitl_id, HITLResolveRequest(resolution={"answer": "yes"})
            )
            self.assertEqual(ok.status, "resolved")
            with self.assertRaises(WorkgroupError) as ctx:
                loop.resolve_info_hitl(
                    wid, hitl.hitl_id, HITLResolveRequest(resolution={"answer": "no"})
                )
            self.assertEqual(ctx.exception.code, "already_resolved")

    def test_archive_fencing_and_indeterminate_reconcile(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            loop, bridge, wid, mid = self._setup(tmp)
            dispatched = loop.assign_and_dispatch_read_file(
                wid, member_id=mid, instruction="读 README", path="README"
            )
            archived = loop.archive_with_tombstone(wid)
            self.assertEqual(archived["workgroup"].status, "archived")
            # 旧 lease 命令被拒
            stale = dict(dispatched["command"])
            stale["command_id"] = "cmd_" + "0" * 26
            stale["lease_epoch"] = 1
            rejected = bridge.execute_command(stale)
            self.assertEqual(rejected["status"], "rejected")

            # journal 丢失 + 副作用已开始 → indeterminate，不自动重执行
            recon = loop.reconcile_missing_journal(
                wid,
                assign_id=dispatched["assign"].assign_id,
                command_id=dispatched["command"]["command_id"],
                member_id=mid,
                side_effect_started=True,
            )
            self.assertEqual(recon["status"], "indeterminate")
            self.assertFalse(recon["auto_reexec"])

    def test_archived_member_ignores_late_provision_result(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "manage.db"))
            loop = VerticalLoop(store)
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(
                    display_name="Archive replay",
                    created_by_node_id="node-a",
                    llm_profile_id="default",
                    llm_profile_revision="1",
                )
            )
            first, _ = store.create_member(
                group.workgroup_id,
                MemberCreateRequest(home_node_id="node-a", display_name="first"),
            )
            second, _ = store.create_member(
                group.workgroup_id,
                MemberCreateRequest(home_node_id="node-a", display_name="second"),
            )
            provision = loop.enqueue_provision(group.workgroup_id, first.member_id)

            archived = store.archive_member(group.workgroup_id, first.member_id)
            tombstone = loop.enqueue_member_tombstone(group.workgroup_id, first.member_id)
            self.assertEqual(archived.status, "archived")
            self.assertEqual(tombstone.payload["member_id"], first.member_id)

            late = loop.complete_provision(
                group.workgroup_id,
                ProvisionCompleteRequest(
                    member_id=first.member_id,
                    provision_id=provision.payload["provision_id"],
                    status="ready",
                ),
            )
            self.assertEqual(late["member"].status, "archived")
            self.assertEqual(store.get_member(first.member_id).status, "archived")
            self.assertEqual([m.member_id for m in store.list_members(group.workgroup_id)], [second.member_id])


if __name__ == "__main__":
    unittest.main()
