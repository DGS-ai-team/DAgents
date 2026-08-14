"""工作组 @直连、提及剥离、turn cancel。"""

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
from manage.workgroup.errors import WorkgroupError  # noqa: E402
from manage.workgroup.mentions import resolve_direct_member, strip_member_mention  # noqa: E402
from manage.workgroup.models import (  # noqa: E402
    ACLPatchRequest,
    ActorRunCreateRequest,
    MemberCreateRequest,
    WorkGroupCreateRequest,
)
from manage.workgroup.history import RunHistoryMessage  # noqa: E402
from manage.workgroup import ids  # noqa: E402
from manage.workgroup.llm_chat import ChatResult, MockLLMClient  # noqa: E402
from manage.workgroup.native_tools import scripted_assign_completer  # noqa: E402
from manage.workgroup.store import WorkGroupStore  # noqa: E402
from manage.workgroup.turn_kernel import TurnKernel  # noqa: E402


class MentionStripTests(unittest.TestCase):
    def test_strip_exact_token(self) -> None:
        self.assertEqual(
            strip_member_mention("@Alice 请读 README", display_name="Alice"),
            "请读 README",
        )

    def test_strip_keeps_other_at(self) -> None:
        # 非精确 display_name 的 @ 不剥（避免误伤）
        self.assertIn(
            "@Bob",
            strip_member_mention("@Alice 抄送 @Bob", display_name="Alice"),
        )

class DirectMemberRouteTests(unittest.TestCase):
    def _ready_group(self, tmp: str):
        store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
        group, _ = store.create_workgroup(
            WorkGroupCreateRequest(
                display_name="Direct",
                created_by_node_id="node-a",
                llm_profile_id="mock",
                llm_profile_revision="1",
            )
        )
        store.patch_acl(
            group.workgroup_id,
            ACLPatchRequest(collaborators=["node-b"], expected_revision=1),
        )
        member, _ = store.create_member(
            group.workgroup_id,
            MemberCreateRequest(
                display_name="Alice",
                home_node_id="node-b",
                llm_profile_id="mock",
                llm_profile_revision="1",
            ),
        )
        store.mark_member_status(
            member.member_id,
            "ready",
            workgroup_id=group.workgroup_id,
            workspace_path=str(Path(tmp) / "ws"),
            tool_catalog_revision="rev_test",
        )
        store.publish_workgroup(group.workgroup_id)
        return store, group.workgroup_id, member

    def test_resolve_requires_ready(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, wid, member = self._ready_group(tmp)
            store.mark_member_status(
                member.member_id,
                "error",
                workgroup_id=wid,
                workspace_path="",
                tool_catalog_revision="",
            )
            with self.assertRaises(WorkgroupError):
                resolve_direct_member(
                    store,
                    wid,
                    direct_member_id=member.member_id,
                    timeline_text="@Alice hi",
                )

    def test_direct_skips_leader_llm(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, wid, member = self._ready_group(tmp)
            kernel = TurnKernel(store, mock_llm=True, assign_completer=scripted_assign_completer)
            result = kernel.handle_human_message(
                wid,
                text="@Alice 请读 README",
                from_node_id="node-a",
                direct_member_id=member.member_id,
            )
            self.assertEqual(result.get("mode"), "direct")
            self.assertEqual(result["loop"]["status"], "succeeded")
            types = [e.type for e in store.list_timeline(wid)]
            self.assertIn("human_message", types)
            self.assertIn("assign_started", types)
            human = next(e for e in store.list_timeline(wid) if e.type == "human_message")
            self.assertIn("@Alice", human.text)
            self.assertEqual(human.direct_member_id, member.member_id)
            started = next(e for e in store.list_timeline(wid) if e.type == "assign_started")
            self.assertTrue(started.text.startswith("直达"))
            self.assertEqual(started.actor_id, member.member_id)
            # 直连 Timeline 不应出现 Supervisor（leader）分派事件
            self.assertFalse(
                any(
                    e.type in {"assign_started", "assign_finished", "system_notice"}
                    and e.actor_id == "leader"
                    for e in store.list_timeline(wid)
                )
            )

    def test_direct_mention_reuses_supervisor_session_and_records_result(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, wid, member = self._ready_group(tmp)
            client = MockLLMClient([ChatResult(content="follow-up", finish_reason="stop")])
            kernel = TurnKernel(
                store,
                chat_client=client,
                mock_llm=True,
                assign_completer=scripted_assign_completer,
            )

            direct = kernel.handle_human_message(
                wid,
                text="@Alice 检查 README",
                from_node_id="node-a",
                direct_member_id=member.member_id,
            )
            leader_run_id = direct["leader_run"].run_id
            leader_runs = store.list_actor_runs(wid, actor_id="leader")
            self.assertEqual([run.run_id for run in leader_runs], [leader_run_id])

            history = store.get_run_history(leader_run_id)
            assert history is not None
            self.assertIn("@Alice 检查 README", [m.content for m in history.messages])
            self.assertIn(
                "[scripted] 检查 README",
                [m.content for m in history.messages],
            )

            follow_up = kernel.handle_human_message(
                wid,
                text="总结刚才的结果",
                from_node_id="node-a",
                disable_tools=True,
            )
            self.assertEqual(follow_up["leader_run"].run_id, leader_run_id)
            self.assertEqual(len(store.list_actor_runs(wid, actor_id="leader")), 1)
            self.assertIn(
                "@Alice 检查 README",
                [m.get("content") for m in client.calls[0]["messages"]],
            )
            self.assertIn(
                "[scripted] 检查 README",
                [m.get("content") for m in client.calls[0]["messages"]],
            )

    def test_legacy_duplicate_supervisor_runs_are_consolidated(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, wid, _member = self._ready_group(tmp)
            first = store.create_actor_run(
                wid,
                ActorRunCreateRequest(actor_id="leader"),
            )
            second = store.create_actor_run(
                wid,
                ActorRunCreateRequest(actor_id="leader"),
            )
            store.append_run_history(
                first.run_id,
                [RunHistoryMessage(role="assistant", content="first session")],
                timeline_watermark_seq=1,
            )
            store.append_run_history(
                second.run_id,
                [RunHistoryMessage(role="assistant", content="second session")],
                timeline_watermark_seq=3,
            )
            store.append_timeline(
                wid,
                type="human_message",
                actor_id="node-a",
                text="@Alice legacy request",
                protocol_name="human_node-a",
            )
            assign_id = ids.assign_id()
            store.append_timeline(
                wid,
                type="actor_final_text",
                actor_id="legacy-member",
                text="legacy member result",
                protocol_name="member_legacy-member",
                assign_id=assign_id,
            )

            listed = store.list_actor_runs(wid, actor_id="leader")
            self.assertEqual(len(listed), 1)
            merged = store.get_run_history(listed[0].run_id)
            assert merged is not None
            contents = [m.content for m in merged.messages]
            self.assertIn("first session", contents)
            self.assertIn("second session", contents)
            self.assertIn("@Alice legacy request", contents)
            self.assertIn("legacy member result", contents)
            all_listed = store.list_actor_runs(wid)
            self.assertEqual(sum(run.actor_id == "leader" for run in all_listed), 1)

    def test_manual_leading_mention_stays_with_leader(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, wid, _member = self._ready_group(tmp)
            kernel = TurnKernel(store, mock_llm=True, assign_completer=scripted_assign_completer)
            result = kernel.handle_human_message(
                wid,
                text="@Alice 请读 README",
                from_node_id="node-a",
            )
            self.assertEqual(result.get("mode"), "leader")
            self.assertEqual(result["loop"]["status"], "succeeded")
            human = next(e for e in store.list_timeline(wid) if e.type == "human_message")
            self.assertIsNone(human.direct_member_id)

    def test_cancel_idle_orphan(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, wid, _member = self._ready_group(tmp)
            kernel = TurnKernel(store, mock_llm=True)
            out = kernel.cancel_turn(wid)
            self.assertFalse(out["cancelled"])
            self.assertEqual(out["mode"], "idle")

    def test_cancel_during_direct_completer(self) -> None:
        """直连执行中 cancel：置位 flag、fail assign、heal；序列收口为已中断。"""
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, wid, member = self._ready_group(tmp)
            barrier = threading.Event()
            release = threading.Event()

            def blocking_completer(workgroup_id, assign_id, member_id, instruction, tool_call_id=""):
                _ = (workgroup_id, assign_id, member_id, instruction, tool_call_id)
                barrier.set()
                if not release.wait(timeout=5):
                    raise TimeoutError("completer not released")
                # cancel 后 kernel 会在 completer 返回后再次检查
                return "should-not-matter"

            kernel = TurnKernel(store, mock_llm=True, assign_completer=blocking_completer)
            result_box: dict = {}
            err_box: dict = {}

            def run():
                try:
                    result_box["r"] = kernel.handle_human_message(
                        wid,
                        text="@Alice 长任务",
                        from_node_id="node-a",
                        direct_member_id=member.member_id,
                    )
                except Exception as exc:  # noqa: BLE001
                    err_box["e"] = exc

            t = threading.Thread(target=run, daemon=True)
            t.start()
            self.assertTrue(barrier.wait(timeout=5))
            out = kernel.cancel_turn(wid)
            self.assertTrue(out["cancelled"])
            self.assertEqual(out["mode"], "direct")
            release.set()
            t.join(timeout=5)
            self.assertFalse(t.is_alive())
            # 直连路径对 canceled 收口为 final，不抛给同步 handle
            if "r" in result_box:
                self.assertEqual(result_box["r"]["loop"]["status"], "canceled")
            finished = [e for e in store.list_timeline(wid) if e.type == "assign_finished"]
            self.assertTrue(finished)
            self.assertTrue(any("中断" in (e.text or "") for e in finished))
            # 同一 assign 不应重复写多条 finished（cancel + path 去重）
            aids = [e.assign_id for e in finished]
            self.assertEqual(len(aids), len(set(aids)))


if __name__ == "__main__":
    unittest.main()
