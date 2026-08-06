"""workgroup-d05 Manage 侧 store / protocol / ACL / HITL golden（读 fixture JSON）。

投影类已在 test_manage_workgroup_projector；Node/WS/tool_command 仍由专项/Go 覆盖。
本文件对接可在纯 Python 闭合的 messaging / identity / hitl-CAS / fencing(ACL) / security(timeline shape)。
"""

from __future__ import annotations

import json
import sys
import threading
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.storage.sqlite import SQLiteDatabase  # noqa: E402
from manage.workgroup.d3_models import HumanPostRequest, TimelineEvent  # noqa: E402
from manage.workgroup.errors import WorkgroupError  # noqa: E402
from manage.workgroup.history import RunHistoryMessage  # noqa: E402
from manage.workgroup.models import (  # noqa: E402
    ACLPatchRequest,
    MemberCreateRequest,
    WorkGroupCreateRequest,
)
from manage.workgroup.protocol_names import (  # noqa: E402
    is_reserved_protocol_name,
    protocol_name_for_actor,
)
from manage.workgroup.store import WorkGroupStore  # noqa: E402
from manage.workgroup.vertical import VerticalLoop  # noqa: E402

_FIX = _ROOT / "docs" / "design" / "fixtures" / "workgroup-d05"

# INDEX 中本文件负责执行的相对路径（供 INDEX harness 交叉检查）
STORE_GOLDEN_FILES = (
    "messaging/human_client_message_id_dedupe.json",
    "messaging/concurrent_human_total_order_by_seq.json",
    "identity/reject_display_name_as_protocol_name.json",
    "identity/reserved_name_spoof.json",
    "hitl/double_resolve_cas.json",
    "fencing/acl_revoke_stops_timeline_read.json",
    "security/timeline_excludes_raw_tool_payload.json",
)


def _load(rel: str) -> dict:
    return json.loads((_FIX / rel).read_text(encoding="utf-8"))


def _ready_store(
    tmp: str,
    *,
    owners: list[str] | None = None,
    collaborators: list[str] | None = None,
    with_member: bool = True,
) -> tuple[WorkGroupStore, str]:
    store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
    owner = (owners or ["node_a"])[0]
    group, _ = store.create_workgroup(
        WorkGroupCreateRequest(
            display_name="Fixture",
            created_by_node_id=owner,
            llm_profile_id="mock",
            llm_profile_revision="1",
        )
    )
    wid = group.workgroup_id
    collabs = list(collaborators if collaborators is not None else ["node_b"])
    # create_workgroup 已把 owner 写入 ACL revision=1
    store.patch_acl(wid, ACLPatchRequest(collaborators=collabs, expected_revision=1))
    if with_member and collabs:
        home = collabs[0]
        member, _ = store.create_member(
            wid,
            MemberCreateRequest(
                display_name="Worker",
                home_node_id=home,
                llm_profile_id="mock",
                llm_profile_revision="1",
                allow_tool_names=["read_file"],
            ),
        )
        store.mark_member_status(
            member.member_id,
            "ready",
            workgroup_id=wid,
            workspace_path=str(Path(tmp) / "ws"),
            tool_catalog_revision="rev_test",
        )
    store.publish_workgroup(wid)
    return store, wid


class StoreGoldenTests(unittest.TestCase):
    def test_human_client_message_id_dedupe(self) -> None:
        fix = _load("messaging/human_client_message_id_dedupe.json")
        self.assertEqual(fix["when"]["op"], "human.post")
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, wid = _ready_store(tmp)
            loop = VerticalLoop(store)
            cid = str(fix["when"]["client_message_id"])
            node = str(fix["when"]["node"])
            text = str(fix["when"]["text"])
            e1 = loop.post_human(
                wid,
                HumanPostRequest(from_node_id=node, text=text, client_message_id=cid),
            )
            e2 = loop.post_human(
                wid,
                HumanPostRequest(from_node_id=node, text=text, client_message_id=cid),
            )
            self.assertEqual(e1.event_id, e2.event_id)
            self.assertEqual(e1.seq, e2.seq)
            self.assertEqual(len(store.list_timeline(wid)), 1)

    def test_concurrent_human_total_order_by_seq(self) -> None:
        fix = _load("messaging/concurrent_human_total_order_by_seq.json")
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, wid = _ready_store(tmp, collaborators=["node_b", "node_c"])
            loop = VerticalLoop(store)
            events = []
            for step in fix["when"]:
                events.append(
                    loop.post_human(
                        wid,
                        HumanPostRequest(
                            from_node_id=str(step["node_id"]),
                            text=str(step["text"]),
                            client_message_id=str(step["client_message_id"]),
                        ),
                    )
                )
            then = fix["then"]
            self.assertEqual(len(events), then["events"])
            seqs = [e.seq for e in events]
            if then.get("seq_distinct"):
                self.assertEqual(len(seqs), len(set(seqs)))
            if then.get("order") == "by_seq_asc":
                self.assertEqual(seqs, sorted(seqs))
            listed = store.list_timeline(wid)
            self.assertEqual([e.seq for e in listed], sorted(e.seq for e in listed))

    def test_protocol_name_rejects_unicode_display(self) -> None:
        fix = _load("identity/reject_display_name_as_protocol_name.json")
        mid = str(fix["given"]["member_id"])
        got = protocol_name_for_actor(mid)
        self.assertEqual(got, fix["then"]["protocol_name"])
        for forbidden in fix["then"]["forbidden"]:
            self.assertNotEqual(got, forbidden)
            self.assertNotIn(forbidden, got)

    def test_reserved_name_spoof_rejected(self) -> None:
        fix = _load("identity/reserved_name_spoof.json")
        self.assertTrue(is_reserved_protocol_name(fix["given"]["client_claimed_name"]))
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, wid = _ready_store(tmp)
            loop = VerticalLoop(store)
            node = str(fix["given"]["authenticated_node_id"])
            ev = loop.post_human(
                wid,
                HumanPostRequest(
                    from_node_id=node,
                    text=str(fix["when"]["text"]),
                    client_message_id=str(fix["when"]["client_message_id"]),
                ),
            )
            # 客户端声称 leader 不得写入；由 actor_id 推导 human_* 
            self.assertNotEqual(ev.protocol_name, fix["then"]["never_protocol_name"])
            self.assertNotEqual(ev.protocol_name, "leader")
            self.assertTrue(str(ev.protocol_name or "").startswith("human_"))
            # fixture 示例用 hu_ ULID；实现为 human_<node_id>，钉实际 stamp
            self.assertEqual(ev.protocol_name, protocol_name_for_actor(node))

    def test_hitl_double_resolve_cas(self) -> None:
        fix = _load("hitl/double_resolve_cas.json")
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, wid = _ready_store(tmp, collaborators=["node_b", "node_c"])
            hitl = store.create_hitl(wid, prompt="confirm?")
            results: list[str] = []
            errors: list[str] = []
            barrier = threading.Barrier(2)

            def resolve(answer: str) -> None:
                barrier.wait(timeout=2)
                try:
                    store.resolve_hitl_cas(wid, hitl.hitl_id, resolution={"answer": answer})
                    results.append(answer)
                except WorkgroupError as exc:
                    errors.append(exc.code)

            t1 = threading.Thread(target=resolve, args=("yes",), daemon=True)
            t2 = threading.Thread(target=resolve, args=("no",), daemon=True)
            t1.start()
            t2.start()
            t1.join(timeout=3)
            t2.join(timeout=3)
            self.assertEqual(len(results), fix["then"]["resolved_count"])
            self.assertIn(fix["then"]["loser_code"], errors)
            # tool_results_written / resume_enqueued 需 turn_kernel 全链路，本档只钉 CAS

    def test_acl_revoke_stops_timeline_read(self) -> None:
        fix = _load("fencing/acl_revoke_stops_timeline_read.json")
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, wid = _ready_store(tmp, collaborators=["node_b", "node_c"])
            reader = str(fix["given"]["reader_node"])
            store.assert_acl_member(wid, reader)
            acl = store.get_acl(wid)
            assert acl is not None
            remaining = [n for n in acl.collaborators if n != reader]
            store.patch_acl(
                wid,
                ACLPatchRequest(collaborators=remaining, expected_revision=acl.revision),
            )
            with self.assertRaises(WorkgroupError) as ctx:
                store.assert_acl_member(wid, reader)
            self.assertEqual(ctx.exception.code, fix["then"]["code"])
            self.assertEqual(ctx.exception.http_status, fix["then"]["history_read_http_status"])

    def test_timeline_excludes_raw_tool_payload(self) -> None:
        fix = _load("security/timeline_excludes_raw_tool_payload.json")
        fields = set(TimelineEvent.model_fields.keys())
        for forbidden in fix["then"]["timeline_fields_forbidden"]:
            self.assertNotIn(forbidden, fields)
        if fix["then"].get("run_history_may_contain_tool_body"):
            secret = fix["given"]["tool_result_in_run_history"]["content"]
            msg = RunHistoryMessage(role="tool", content=secret, tool_call_id="call_1", name="read_file")
            self.assertIn(secret, msg.content or "")


if __name__ == "__main__":
    unittest.main()
