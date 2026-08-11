"""Manage Leader LLM loop（Mock）单元测试。"""

from __future__ import annotations

import sys
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
    MemberCreateRequest,
    WorkGroupCreateRequest,
)
from manage.workgroup.store import WorkGroupStore  # noqa: E402
from manage.workgroup.turn_kernel import (  # noqa: E402
    TurnKernel,
    mock_leader_script_assign_then_answer,
    mock_member_script_read_file_then_answer,
)


class LeaderLoopTests(unittest.TestCase):
    def test_supervisor_chat_without_tools_and_member(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(
                    display_name="Solo",
                    created_by_node_id="node-a",
                    llm_profile_id="mock",
                    llm_profile_revision="1",
                )
            )
            store.publish_workgroup(group.workgroup_id)
            kernel = TurnKernel(store, mock_llm=True)
            result = kernel.handle_human_message(
                group.workgroup_id,
                text="你好，请直接回复，不要调用任何工具",
                from_node_id="node-a",
                disable_tools=True,
            )
            self.assertEqual(result["loop"]["status"], "succeeded")
            self.assertTrue(result["loop"]["final_text"])
            hist = store.get_run_history(result["leader_run"].run_id)
            assert hist is not None
            # 无工具路径：不应写入 tool 消息
            self.assertEqual([m.role for m in hist.messages].count("tool"), 0)

    def _ready_group(self, store: WorkGroupStore) -> tuple[str, str]:
        group, _ = store.create_workgroup(
            WorkGroupCreateRequest(
                display_name="Loop",
                created_by_node_id="node-a",
                llm_profile_id="mock",
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
                display_name="worker",
                allow_tool_names=["read_file"],
            ),
        )
        _ = spec
        store.mark_member_status(member.member_id, "ready", workgroup_id=group.workgroup_id)
        store.publish_workgroup(group.workgroup_id)
        return group.workgroup_id, member.member_id

    def test_human_message_drives_assign_then_final(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            wid, mid = self._ready_group(store)
            script = mock_leader_script_assign_then_answer(
                member_id=mid,
                instruction="读 README",
                final_text="README 已读完",
            )
            kernel = TurnKernel(store, chat_client=MockLLMClient(script), mock_llm=True)
            result = kernel.handle_human_message(
                wid,
                text="请安排成员读 README",
                from_node_id="node-a",
            )
            self.assertEqual(result["loop"]["status"], "succeeded")
            self.assertEqual(result["loop"]["final_text"], "README 已读完")
            self.assertEqual(result["leader_run"].status, "succeeded")

            timeline = store.list_timeline(wid)
            types = [e.type for e in timeline]
            self.assertEqual(types[0], "human_message")
            self.assertIn("actor_final_text", types)
            # member scripted final + leader final
            actor_finals = [e for e in timeline if e.type == "actor_final_text"]
            self.assertGreaterEqual(len(actor_finals), 2)

            hist = store.get_run_history(result["leader_run"].run_id)
            assert hist is not None
            roles = [m.role for m in hist.messages]
            self.assertEqual(roles.count("assistant"), 2)
            self.assertEqual(roles.count("tool"), 1)
            tool = next(m for m in hist.messages if m.role == "tool")
            self.assertEqual(tool.name, "assign_workgroup_task")
            self.assertIn("[scripted] 读 README", tool.content or "")
            self.assertIn("\"status\": \"succeeded\"", tool.content or "")

    def test_human_message_events_stream_deltas(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(
                    display_name="Stream",
                    created_by_node_id="node-a",
                    llm_profile_id="mock",
                    llm_profile_revision="1",
                )
            )
            store.publish_workgroup(group.workgroup_id)
            script = [ChatResult(content="你好世界，这是流式回复", finish_reason="stop")]
            kernel = TurnKernel(store, chat_client=MockLLMClient(script), mock_llm=True)
            events = list(
                kernel.handle_human_message_events(
                    group.workgroup_id,
                    text="hi",
                    from_node_id="node-a",
                    disable_tools=True,
                )
            )
            names = [e["event"] for e in events]
            self.assertEqual(names[0], "human")
            self.assertIn("status", names)
            self.assertIn("delta", names)
            self.assertIn("assistant_final", names)
            self.assertEqual(names[-1], "final")
            deltas = "".join(e["data"]["text"] for e in events if e["event"] == "delta")
            self.assertEqual(deltas, "你好世界，这是流式回复")

    def test_member_llm_loop_read_file_then_final(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            wid, mid = self._ready_group(store)
            import importlib.util

            from manage.workgroup.llm_chat import MockLLMClient
            from manage.workgroup.vertical import VerticalLoop

            path = Path(__file__).resolve().parent / "test_manage_workgroup_vertical.py"
            spec = importlib.util.spec_from_file_location("test_manage_workgroup_vertical", path)
            assert spec and spec.loader
            mod = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(mod)
            FakeNodeBridge = mod.FakeNodeBridge

            bridge = FakeNodeBridge(Path(tmp) / "node", node_id="node-b")
            loop = VerticalLoop(store, bridge=bridge, command_timeout_s=2.0)
            loop.enqueue_provision(wid, mid)

            leader_script = mock_leader_script_assign_then_answer(
                member_id=mid,
                instruction="读 README 并摘要",
                final_text="组长已汇总",
            )
            member_script = mock_member_script_read_file_then_answer(
                path="README",
                final_text="标题是 Demo",
            )
            kernel = TurnKernel(
                store,
                chat_client=MockLLMClient(leader_script),
                member_chat_client=MockLLMClient(member_script),
            )
            live_events = []
            kernel.set_realtime_event_listener(
                lambda _wid, event_type, data, client_id: live_events.append(
                    (event_type, data, client_id)
                )
            )
            kernel.set_assign_completer(loop.make_assign_completer(kernel))
            result = kernel.handle_human_message(
                wid,
                text="请成员读 README",
                from_node_id="node-a",
            )
            self.assertEqual(result["loop"]["final_text"], "组长已汇总")
            timeline = store.list_timeline(wid)
            member_finals = [
                e for e in timeline if e.type == "actor_final_text" and e.actor_id == mid
            ]
            self.assertEqual(len(member_finals), 1)
            self.assertEqual(member_finals[0].text, "标题是 Demo")
            self.assertEqual(sum(bridge.executions.values()), 1)
            member_live = [
                (event_type, data)
                for event_type, data, _client_id in live_events
                if data.get("member_id") == mid
            ]
            self.assertTrue(any(event_type == "status" for event_type, _data in member_live))
            self.assertTrue(any(event_type == "delta" for event_type, _data in member_live))

            runs = [r for r in store._runs.values() if r.actor_id == mid]  # noqa: SLF001
            self.assertEqual(len(runs), 1)
            mhist = store.get_run_history(runs[0].run_id)
            assert mhist is not None
            roles = [m.role for m in mhist.messages]
            self.assertIn("user", roles)
            self.assertEqual(roles.count("tool"), 1)
            self.assertGreaterEqual(roles.count("assistant"), 2)

    def test_heals_interrupted_open_tool_calls(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(
                    display_name="Heal",
                    created_by_node_id="node-a",
                    llm_profile_id="mock",
                    llm_profile_revision="1",
                )
            )
            store.publish_workgroup(group.workgroup_id)
            from manage.workgroup.history import RunHistoryMessage, ToolCall, ToolCallFunction
            from manage.workgroup.models import ActorRunCreateRequest

            run = store.create_actor_run(
                group.workgroup_id,
                ActorRunCreateRequest(actor_id="leader"),
            )
            store.ensure_run_history(run)
            store.append_run_history(
                run.run_id,
                [
                    RunHistoryMessage(
                        role="assistant",
                        name="leader",
                        content="",
                        tool_calls=[
                            ToolCall(
                                id="call_orphan",
                                function=ToolCallFunction(
                                    name="list_workgroup_members",
                                    arguments="{}",
                                ),
                            )
                        ],
                    )
                ],
            )
            kernel = TurnKernel(
                store,
                chat_client=MockLLMClient(
                    [ChatResult(content="已恢复并继续", finish_reason="stop")]
                ),
                mock_llm=True,
            )
            result = kernel.handle_human_message(
                group.workgroup_id,
                text="继续",
                from_node_id="node-a",
                disable_tools=True,
            )
            self.assertEqual(result["loop"]["status"], "succeeded")
            self.assertEqual(result["loop"]["final_text"], "已恢复并继续")
            hist = store.get_run_history(run.run_id)
            assert hist is not None
            tool = next(m for m in hist.messages if m.role == "tool")
            self.assertEqual(tool.tool_call_id, "call_orphan")
            self.assertIn("interrupted", tool.content or "")

    def test_leader_tool_loop_soft_limit_returns_tool_result(self) -> None:
        """超限后若模型仍发起 tool_calls，写入 soft tool_result 并允许收尾结论。"""
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            wid, mid = self._ready_group(store)
            from manage.workgroup.llm_chat import ChatToolCall

            # 第 1 步：预算内执行工具；第 2 步：超额仍 tool_calls → soft；第 3 步：终态文本
            # 注意：每步必须用独立 ChatResult，避免脚本条目共享同一对象被复用污染。
            script = [
                ChatResult(
                    content="",
                    tool_calls=[
                        ChatToolCall(
                            id="call_loop_1",
                            name="list_workgroup_members",
                            arguments="{}",
                        )
                    ],
                    finish_reason="tool_calls",
                ),
                ChatResult(
                    content="",
                    tool_calls=[
                        ChatToolCall(
                            id="call_loop_2",
                            name="list_workgroup_members",
                            arguments="{}",
                        )
                    ],
                    finish_reason="tool_calls",
                ),
                ChatResult(content="已达上限，当前进度：已列出成员。是否继续？", finish_reason="stop"),
            ]
            kernel = TurnKernel(
                store,
                chat_client=MockLLMClient(script),
                mock_llm=True,
                max_tool_loops=1,
            )
            result = kernel.handle_human_message(
                wid,
                text="列出成员并继续工具",
                from_node_id="node-a",
            )
            self.assertEqual(result["loop"]["status"], "succeeded")
            self.assertIn("是否继续", result["loop"]["final_text"])
            hist = store.get_run_history(result["leader_run"].run_id)
            assert hist is not None
            soft = [
                m
                for m in hist.messages
                if m.role == "tool" and "已超过单轮工具调用次数" in (m.content or "")
            ]
            self.assertEqual(len(soft), 1, hist.messages)
            _ = mid

    def test_new_human_message_releases_stuck_active_assign(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            wid, mid = self._ready_group(store)
            from manage.workgroup.models import AssignCreateRequest

            stuck = store.create_assign(wid, AssignCreateRequest(member_id=mid, instruction="stuck"))
            store.set_assign_status(stuck.assign_id, "running")
            kernel = TurnKernel(
                store,
                chat_client=MockLLMClient(
                    [ChatResult(content="锁已释放，可以继续", finish_reason="stop")]
                ),
                mock_llm=True,
            )
            result = kernel.handle_human_message(
                wid,
                text="继续",
                from_node_id="node-a",
                disable_tools=True,
            )
            self.assertEqual(result["loop"]["status"], "succeeded")
            self.assertEqual(store.get_assign(stuck.assign_id).status, "failed")

    def test_single_active_assign_enforced(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            wid, mid = self._ready_group(store)
            from manage.workgroup.models import AssignCreateRequest
            from manage.workgroup.errors import WorkgroupError

            store.create_assign(wid, AssignCreateRequest(member_id=mid, instruction="a"))
            with self.assertRaises(WorkgroupError) as ctx:
                store.create_assign(wid, AssignCreateRequest(member_id=mid, instruction="b"))
            self.assertEqual(ctx.exception.code, "conflict")


if __name__ == "__main__":
    unittest.main()
