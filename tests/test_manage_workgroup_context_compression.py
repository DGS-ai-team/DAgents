from __future__ import annotations

import sys
import threading
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

ROOT = Path(__file__).resolve().parents[1]
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

from manage.storage.sqlite import SQLiteDatabase  # noqa: E402
from manage.workgroup.context_compression import (  # noqa: E402
    ActorContextSnapshot,
    build_compression_plan,
    context_messages,
    make_snapshot,
)
from manage.workgroup.history import RunHistoryMessage, ToolCall, ToolCallFunction  # noqa: E402
from manage.workgroup.models import (  # noqa: E402
    ActorRunCreateRequest,
    AssignCreateRequest,
    MemberCreateRequest,
    WorkGroupCreateRequest,
)
from manage.workgroup.store import WorkGroupStore  # noqa: E402
from manage.workgroup.turn_kernel import TurnKernel  # noqa: E402
from manage.workgroup.llm_chat import ChatResult  # noqa: E402


def _history(count: int = 7) -> list[RunHistoryMessage]:
    return [
        RunHistoryMessage(role="user", content=f"old request {i} " + ("x" * 1800))
        for i in range(count)
    ]


class ContextCompressionTests(unittest.TestCase):
    class _SummaryAwareClient:
        def __init__(self) -> None:
            self.summary_calls: list[list[dict[str, object]]] = []
            self.primary_calls: list[list[dict[str, object]]] = []

        def chat(self, messages, *, tools=None, tool_choice=None):  # noqa: ANN001
            if any(message.get("name") == "workgroup_context_summary" for message in messages):
                self.summary_calls.append(messages)
                return ChatResult(content="压缩后的事实摘要", finish_reason="stop")
            self.primary_calls.append(messages)
            return ChatResult(content="最终答复", finish_reason="stop")

        def stream_chat(self, messages, *, tools=None, tool_choice=None):  # noqa: ANN001
            yield type("Piece", (), {"delta": "最终答复", "result": ChatResult(content="最终答复")})()

    def test_plan_keeps_recent_message_and_respects_tool_boundary(self) -> None:
        history = [
            RunHistoryMessage(role="user", content="first " + "a" * 1200),
            RunHistoryMessage(
                role="assistant",
                content="",
                tool_calls=[
                    ToolCall(
                        id="call_1",
                        function=ToolCallFunction(name="bash_run", arguments="{}"),
                    )
                ],
            ),
            RunHistoryMessage(role="tool", tool_call_id="call_1", name="bash_run", content="done"),
            RunHistoryMessage(role="user", content="latest " + "b" * 1200),
        ]
        plan = build_compression_plan(
            history,
            snapshot=None,
            trigger_tokens=100,
            keep_tokens=100,
        )
        self.assertIsNotNone(plan)
        assert plan is not None
        self.assertLess(plan.end, len(history))
        self.assertEqual(context_messages(history, None)[-1]["content"], history[-1].content)

        open_history = history[:2]
        self.assertIsNone(
            build_compression_plan(
                open_history,
                snapshot=None,
                trigger_tokens=1,
                keep_tokens=0,
            )
        )

    def test_snapshot_projection_preserves_full_history_and_rejects_stale_source(self) -> None:
        history = _history()
        plan = build_compression_plan(
            history,
            snapshot=None,
            trigger_tokens=100,
            keep_tokens=100,
        )
        self.assertIsNotNone(plan)
        assert plan is not None
        snapshot = make_snapshot(
            run_id="rn_00000000000000000000000001",
            workgroup_id="wg_00000000000000000000000001",
            actor_id="leader",
            history=history,
            plan=plan,
            summary="facts from the old prefix",
            previous=None,
            timeline_seq=9,
        )
        projected = context_messages(history, snapshot)
        self.assertEqual(projected[0]["name"], "workgroup_context_summary")
        self.assertIn("facts from the old prefix", projected[0]["content"])
        self.assertEqual(projected[-1]["content"], history[-1].content)

        changed = list(history)
        changed[0] = changed[0].model_copy(update={"content": "changed"})
        fallback = context_messages(changed, snapshot)
        self.assertNotEqual(fallback[0].get("name"), "workgroup_context_summary")
        self.assertEqual(len(fallback), len(changed))

    def test_snapshot_persists_separately_from_run_history(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            db_path = Path(tmp) / "manage.db"
            db = SQLiteDatabase(db_path)
            store = WorkGroupStore(db=db)
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(
                    display_name="snapshot",
                    created_by_node_id="node-a",
                    llm_profile_id="mock",
                    llm_profile_revision="1",
                )
            )
            store.publish_workgroup(group.workgroup_id)
            run = store.create_actor_run(
                group.workgroup_id,
                ActorRunCreateRequest(actor_id="leader"),
            )
            history = _history()
            store.append_run_history(run.run_id, history)
            plan = build_compression_plan(
                history,
                snapshot=None,
                trigger_tokens=100,
                keep_tokens=100,
            )
            assert plan is not None
            snapshot = make_snapshot(
                run_id=run.run_id,
                workgroup_id=group.workgroup_id,
                actor_id="leader",
                history=history,
                plan=plan,
                summary="persisted",
                previous=None,
                timeline_seq=4,
            )
            store.save_context_snapshot(snapshot)
            self.assertEqual(len(store.get_run_history(run.run_id).messages), len(history))  # type: ignore[union-attr]

            reloaded = WorkGroupStore(db=SQLiteDatabase(db_path))
            self.assertEqual(reloaded.get_context_snapshot(run.run_id).summary_content, "persisted")  # type: ignore[union-attr]
            self.assertEqual(len(reloaded.get_run_history(run.run_id).messages), len(history))  # type: ignore[union-attr]

    def test_turn_kernel_does_not_compress_pending_hitl_or_active_assign(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(
                    display_name="guards",
                    created_by_node_id="node-a",
                    llm_profile_id="mock",
                    llm_profile_revision="1",
                )
            )
            store.publish_workgroup(group.workgroup_id)
            kernel = TurnKernel(store, mock_llm=True)
            run = kernel.start_leader_run(group.workgroup_id)
            hitl = store.create_hitl(group.workgroup_id, prompt="wait", run_id=run.run_id)
            self.assertTrue(
                kernel._context_compression_blocked(  # noqa: SLF001
                    run,
                    _history(),
                )
            )
            store.resolve_hitl_cas(group.workgroup_id, hitl.hitl_id, resolution={"text": "ok"})
            member_snapshot = ActorContextSnapshot(
                run_id=run.run_id,
                workgroup_id=group.workgroup_id,
                actor_id="leader",
                summary_content="old",
                summary_source_hash="sha256:" + "0" * 64,
                compressed_until_ordinal=1,
                updated_at="2026-01-01T00:00:00Z",
            )
            self.assertFalse(kernel._context_compression_blocked(run, _history()))  # noqa: SLF001
            self.assertEqual(member_snapshot.actor_id, "leader")
            member = store.create_member(
                group.workgroup_id,
                MemberCreateRequest(
                    agent_id="agent-a",
                    home_node_id="node-a",
                    display_name="worker",
                ),
            )
            store.mark_member_status(member.member_id, "ready", workgroup_id=group.workgroup_id)
            store.create_assign(
                group.workgroup_id,
                AssignCreateRequest(member_id=member.member_id, instruction="still running"),
            )
            self.assertTrue(kernel._context_compression_blocked(run, _history()))  # noqa: SLF001

    def test_primary_request_uses_snapshot_without_mutating_run_history(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(
                    display_name="primary",
                    created_by_node_id="node-a",
                    llm_profile_id="mock",
                    llm_profile_revision="1",
                )
            )
            store.publish_workgroup(group.workgroup_id)
            client = self._SummaryAwareClient()
            kernel = TurnKernel(
                store,
                chat_client=client,
                mock_llm=True,
                context_silent_trigger_tokens=100,
                context_blocking_trigger_tokens=100,
                context_keep_tokens=100,
            )
            run = kernel.start_leader_run(group.workgroup_id)
            history = _history(50)
            store.append_run_history(run.run_id, history)
            snapshot = kernel._context_snapshot_for_request(  # noqa: SLF001
                run=run,
                history=history,
                client=client,
                actor_label="Supervisor",
            )
            self.assertIsNotNone(snapshot)
            assert snapshot is not None
            projected = kernel.project(actor_id="leader", run_id=run.run_id)
            self.assertEqual(projected["messages"][0]["name"], "workgroup_context_summary")
            self.assertEqual(projected["messages"][-1]["content"], history[-1].content)
            self.assertEqual(len(store.get_run_history(run.run_id).messages), len(history))  # type: ignore[union-attr]
            self.assertEqual(len(client.summary_calls), 1)

    def test_silent_task_does_not_block_but_blocking_tier_waits(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(
                    display_name="tiers",
                    created_by_node_id="node-a",
                    llm_profile_id="mock",
                    llm_profile_revision="1",
                )
            )
            store.publish_workgroup(group.workgroup_id)

            class BlockingClient(self._SummaryAwareClient):
                def __init__(self) -> None:
                    super().__init__()
                    self.started = threading.Event()
                    self.release = threading.Event()

                def chat(self, messages, *, tools=None, tool_choice=None):  # noqa: ANN001
                    if any(message.get("name") == "workgroup_context_summary" for message in messages):
                        self.started.set()
                        self.release.wait(timeout=2)
                    return super().chat(messages, tools=tools, tool_choice=tool_choice)

            client = BlockingClient()
            kernel = TurnKernel(
                store,
                chat_client=client,
                mock_llm=True,
                context_silent_trigger_tokens=100,
                context_blocking_trigger_tokens=10000,
                context_keep_tokens=50,
            )
            run = kernel.start_leader_run(group.workgroup_id)
            history = _history(7)
            store.append_run_history(run.run_id, history)
            snapshot = kernel._context_snapshot_for_request(  # noqa: SLF001
                run=run,
                history=history,
                client=client,
                actor_label="Supervisor",
            )
            self.assertIsNone(snapshot)
            self.assertTrue(client.started.wait(timeout=1))
            client.release.set()
            with kernel._context_task_lock:  # noqa: SLF001
                task = kernel._context_tasks[run.run_id]  # noqa: SLF001
            task.result(timeout=2)
            kernel._harvest_context_task(run)  # noqa: SLF001
            self.assertIsNotNone(store.get_context_snapshot(run.run_id))
            store.append_run_history(run.run_id, _history(30))
            current = store.get_run_history(run.run_id)
            assert current is not None
            before = len(client.summary_calls)
            # The current history is above the blocking threshold.  The call
            # must wait for and apply a fresh task, not return the old epoch.
            refreshed = kernel._context_snapshot_for_request(  # noqa: SLF001
                run=run,
                history=current.messages,
                client=client,
                actor_label="Supervisor",
            )
            self.assertIsNotNone(refreshed)
            self.assertGreaterEqual(len(client.summary_calls), before + 1)


if __name__ == "__main__":
    unittest.main()
