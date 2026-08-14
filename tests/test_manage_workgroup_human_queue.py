"""工作组 human 消息 FIFO 入队 / 单飞消费。"""

from __future__ import annotations

import sys
import threading
import time
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.storage.sqlite import SQLiteDatabase  # noqa: E402
from manage.workgroup.llm_chat import ChatResult, MockLLMClient  # noqa: E402
from manage.workgroup.models import (  # noqa: E402
    ACLPatchRequest,
    AssignCreateRequest,
    MemberCreateRequest,
    WorkGroupCreateRequest,
)
from manage.workgroup.store import WorkGroupStore  # noqa: E402
from manage.workgroup.turn_kernel import TurnKernel  # noqa: E402


class HumanQueueTests(unittest.TestCase):
    def _ready(self, store: WorkGroupStore) -> str:
        group, _ = store.create_workgroup(
            WorkGroupCreateRequest(
                display_name="Q",
                created_by_node_id="node-a",
                llm_profile_id="mock",
                llm_profile_revision="1",
            )
        )
        store.patch_acl(
            group.workgroup_id,
            ACLPatchRequest(collaborators=["node-b"], expected_revision=1),
        )
        store.publish_workgroup(group.workgroup_id)
        return group.workgroup_id

    def test_second_message_queues_while_busy(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            wid = self._ready(store)
            gate = threading.Event()

            class SlowLLM(MockLLMClient):
                def chat(self, *args, **kwargs):  # type: ignore[no-untyped-def]
                    gate.wait(timeout=5)
                    return ChatResult(content="done-1", finish_reason="stop")

            kernel = TurnKernel(store, chat_client=SlowLLM([]), mock_llm=True)
            events1: list[dict] = []
            events2: list[dict] = []

            def run_first() -> None:
                for ev in kernel.handle_human_message_events(
                    wid, text="first", from_node_id="node-a", disable_tools=True
                ):
                    events1.append(ev)

            t = threading.Thread(target=run_first, daemon=True)
            t.start()
            # wait until first turn claimed
            for _ in range(50):
                if kernel._is_turn_busy(wid):
                    break
                time.sleep(0.02)
            self.assertTrue(kernel._is_turn_busy(wid))

            for ev in kernel.handle_human_message_events(
                wid, text="second", from_node_id="node-b", disable_tools=True
            ):
                events2.append(ev)
            self.assertEqual(events2[0]["event"], "queued")
            self.assertEqual(events2[0]["data"]["position"], 1)
            self.assertEqual(events2[0]["data"]["text"], "second")
            q = kernel.list_human_queue(wid)
            self.assertEqual(q["depth"], 1)
            self.assertTrue(q["busy"])

            # edit + cancel path
            qid = events2[0]["data"]["queue_id"]
            patched = kernel.patch_human_queue_item(wid, qid, text="second-edited")
            self.assertEqual(patched["text"], "second-edited")
            self.assertEqual(patched["position"], 1)

            gate.set()
            t.join(timeout=5)
            self.assertTrue(any(e.get("event") == "final" for e in events1))

            # queued item should be consumed after first finishes
            for _ in range(100):
                if kernel.list_human_queue(wid)["depth"] == 0 and not kernel._is_turn_busy(wid):
                    break
                time.sleep(0.05)
            self.assertEqual(kernel.list_human_queue(wid)["depth"], 0)
            humans = [e for e in store.list_timeline(wid) if e.type == "human_message"]
            self.assertEqual(len(humans), 2)
            self.assertEqual(humans[0].text, "first")
            self.assertEqual(humans[1].text, "second-edited")

    def test_cancel_queued_message(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            wid = self._ready(store)
            gate = threading.Event()

            class SlowLLM(MockLLMClient):
                def chat(self, *args, **kwargs):  # type: ignore[no-untyped-def]
                    gate.wait(timeout=5)
                    return ChatResult(content="ok", finish_reason="stop")

            kernel = TurnKernel(store, chat_client=SlowLLM([]), mock_llm=True)

            def run_first() -> None:
                for _ in kernel.handle_human_message_events(
                    wid, text="first", from_node_id="node-a", disable_tools=True
                ):
                    pass

            t = threading.Thread(target=run_first, daemon=True)
            t.start()
            for _ in range(50):
                if kernel._is_turn_busy(wid):
                    break
                time.sleep(0.02)

            queued = None
            for ev in kernel.handle_human_message_events(
                wid, text="drop-me", from_node_id="node-a", disable_tools=True
            ):
                if ev.get("event") == "queued":
                    queued = ev["data"]
            assert queued is not None
            out = kernel.cancel_human_queue_item(wid, queued["queue_id"])
            self.assertTrue(out["cancelled"])
            self.assertEqual(kernel.list_human_queue(wid)["depth"], 0)

            gate.set()
            t.join(timeout=5)
            for _ in range(50):
                if not kernel._is_turn_busy(wid):
                    break
                time.sleep(0.05)
            humans = [e for e in store.list_timeline(wid) if e.type == "human_message"]
            self.assertEqual([h.text for h in humans], ["first"])

    def test_send_now_recovers_orphaned_direct_turn(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            wid = self._ready(store)
            member, _ = store.create_member(
                wid,
                MemberCreateRequest(
                    home_node_id="node-a",
                    display_name="member-a",
                    description="worker",
                ),
            )
            store.mark_member_status(
                member.member_id,
                "ready",
                workgroup_id=wid,
                workspace_path=str(Path(tmp) / "ws"),
                tool_catalog_revision="rev_test",
            )
            kernel = TurnKernel(
                store,
                mock_llm=True,
                assign_completer=lambda *_args: "done",
            )

            # Stop consuming immediately after final, reproducing a client
            # disconnect before the generator's finally block is resumed.
            first = kernel.handle_human_message_events(
                wid,
                text="@member-a first",
                from_node_id="node-a",
                direct_member_id=member.member_id,
            )
            for event in first:
                if event.get("event") == "final":
                    break
            self.assertEqual(kernel.list_human_queue(wid)["active_mode"], "direct")
            kernel._active_turn[wid]["turn_started_at"] = time.monotonic() - 2

            queued = next(
                event
                for event in kernel.handle_human_message_events(
                    wid,
                    text="@member-a second",
                    from_node_id="node-a",
                    direct_member_id=member.member_id,
                )
                if event.get("event") == "queued"
            )["data"]
            out = kernel.send_human_queue_item_now(wid, queued["queue_id"])
            self.assertTrue(out["sent_now"])

            for _ in range(100):
                state = kernel.list_human_queue(wid)
                if state["depth"] == 0 and not state["busy"]:
                    break
                time.sleep(0.05)
            self.assertEqual(kernel.list_human_queue(wid)["depth"], 0)
            self.assertFalse(kernel.list_human_queue(wid)["busy"])
            humans = [e.text for e in store.list_timeline(wid) if e.type == "human_message"]
            self.assertEqual(humans, ["@member-a first", "@member-a second"])
            first.close()

    def test_queue_changes_broadcast_complete_snapshot(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            wid = self._ready(store)
            kernel = TurnKernel(store, mock_llm=True)
            kernel._begin_turn(wid, mode="leader", client_message_id="active")
            realtime: list[tuple[str, dict]] = []
            kernel.set_realtime_event_listener(
                lambda _wid, event_type, data, _client_id: realtime.append((event_type, data))
            )

            queued = next(
                event
                for event in kernel.handle_human_message_events(
                    wid, text="queued", from_node_id="node-a", disable_tools=True
                )
                if event.get("event") == "queued"
            )["data"]
            self.assertEqual(realtime[-1][0], "queued")
            self.assertEqual(realtime[-1][1]["queue"]["items"][0]["text"], "queued")

            kernel.patch_human_queue_item(wid, queued["queue_id"], text="edited")
            self.assertEqual(realtime[-1][0], "queue")
            self.assertEqual(realtime[-1][1]["queue"]["items"][0]["text"], "edited")

            kernel.cancel_human_queue_item(wid, queued["queue_id"])
            self.assertEqual(realtime[-1][0], "queue")
            self.assertEqual(realtime[-1][1]["queue"]["items"], [])

    def test_queued_message_survives_kernel_reload(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            path = Path(tmp) / "m.db"
            store = WorkGroupStore(db=SQLiteDatabase(path))
            wid = self._ready(store)
            kernel = TurnKernel(store, mock_llm=True)
            kernel._begin_turn(wid, mode="leader", client_message_id="first")
            action, item, position = kernel._claim_or_enqueue(
                wid,
                text="survives restart",
                from_node_id="node-a",
                client_message_id="queued-1",
                disable_tools=True,
                direct_member_id=None,
            )
            self.assertEqual(action, "queued")
            self.assertEqual(position, 1)

            reloaded_store = WorkGroupStore(db=SQLiteDatabase(path))
            reloaded_kernel = TurnKernel(reloaded_store, mock_llm=True)
            queue = reloaded_kernel.list_human_queue(wid)
            self.assertEqual(queue["depth"], 1)
            self.assertEqual(queue["items"][0]["text"], "survives restart")
            self.assertTrue(reloaded_store.get_turn_checkpoint(wid))

    def test_restart_fences_active_runs_and_releases_member(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            path = Path(tmp) / "m.db"
            store = WorkGroupStore(db=SQLiteDatabase(path))
            wid = self._ready(store)
            member, _ = store.create_member(
                wid,
                MemberCreateRequest(
                    home_node_id="node-a",
                    display_name="member-a",
                    description="worker",
                ),
            )
            assign = store.create_assign(
                wid,
                AssignCreateRequest(member_id=member.member_id, instruction="work"),
            )
            store.set_assign_status(assign.assign_id, "running")
            store.mark_member_status(member.member_id, "busy", workgroup_id=wid)
            run = store.get_actor_run(assign.leader_run_id)
            self.assertIsNotNone(run)
            result = store.reconcile_inflight_runs()
            self.assertIn(assign.assign_id, result["assign_ids"])
            self.assertEqual(store.get_assign(assign.assign_id).status, "indeterminate")
            self.assertEqual(store.get_actor_run(assign.leader_run_id).status, "indeterminate")
            self.assertEqual(store.get_member(member.member_id).status, "ready")


if __name__ == "__main__":
    unittest.main()
