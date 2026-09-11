"""Manage Leader LLM loop（Mock）单元测试。"""

from __future__ import annotations

import json
import threading
import sys
import time
import unittest
from datetime import datetime
from pathlib import Path
from tempfile import TemporaryDirectory

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.storage.sqlite import SQLiteDatabase  # noqa: E402
from manage.workgroup.llm_chat import ChatResult, MockLLMClient  # noqa: E402
from manage.workgroup.models import (  # noqa: E402
    ACLPatchRequest,
    ActorRunCreateRequest,
    MemberCreateRequest,
    WorkGroupCreateRequest,
)
from manage.workgroup.store import WorkGroupStore  # noqa: E402
from manage.workgroup.turn_kernel import (  # noqa: E402
    TurnKernel,
    mock_leader_script_assign_then_answer,
)
from manage.workgroup.native_tools import NativeToolDispatcher  # noqa: E402


def scripted_assign_completer(
    _workgroup_id: str,
    _assign_id: str,
    _member_id: str,
    instruction: str,
    _tool_call_id: str = "",
) -> str:
    return f"[scripted] {instruction.strip()[:500]}"


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

    def test_supervisor_member_listing_does_not_expose_tool_allowlist(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            wid, _ = self._ready_group(store)
            payload = json.loads(
                NativeToolDispatcher(store, leader_run_id="rn_test").dispatch(
                    workgroup_id=wid,
                    tool_name="list_workgroup_members",
                    tool_call_id="call_list",
                    arguments_json="{}",
                )
            )
            self.assertTrue(payload["members"])
            self.assertNotIn("tool_allow_names", payload["members"][0])

    def test_first_human_message_survives_ahead_watermark(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(
                    display_name="Stale watermark",
                    created_by_node_id="node-a",
                    llm_profile_id="mock",
                    llm_profile_revision="1",
                )
            )
            store.publish_workgroup(group.workgroup_id)
            client = MockLLMClient()
            kernel = TurnKernel(store, chat_client=client, mock_llm=True)
            run = kernel.start_leader_run(group.workgroup_id)
            # Simulate a reused interrupted run whose watermark is ahead of Timeline.
            store.update_actor_run(run.run_id, timeline_watermark_seq=99)

            text = "message must reach supervisor"
            result = kernel.handle_human_message(
                group.workgroup_id,
                text=text,
                from_node_id="node-a",
                disable_tools=True,
            )
            self.assertEqual(result["loop"]["status"], "succeeded")
            first_request = client.calls[0]["messages"]
            self.assertEqual(
                [m.get("content") for m in first_request if m.get("role") == "user"],
                [f"当天日期为：{datetime.now().strftime('%Y%m%d')}", text],
            )

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
        member = store.create_member(
            group.workgroup_id,
            MemberCreateRequest(
                agent_id="agent-b",
                home_node_id="node-b",
                display_name="worker",
            ),
        )
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
            kernel = TurnKernel(
                store,
                chat_client=MockLLMClient(script),
                assign_completer=scripted_assign_completer,
                mock_llm=True,
            )
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

    def test_tool_call_content_is_persisted_before_assign_timeline_event(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            wid, mid = self._ready_group(store)
            from manage.workgroup.llm_chat import ChatResult, ChatToolCall

            args = json.dumps({"member_id": mid, "instruction": "read README"})
            client = MockLLMClient(
                [
                    ChatResult(
                        content="Supervisor pre-tool content",
                        tool_calls=[
                            ChatToolCall(
                                id="call_content_assign",
                                name="assign_workgroup_task",
                                arguments=args,
                            )
                        ],
                        finish_reason="tool_calls",
                    ),
                    ChatResult(content="done", finish_reason="stop"),
                ]
            )
            kernel = TurnKernel(
                store,
                chat_client=client,
                assign_completer=scripted_assign_completer,
                mock_llm=True,
            )
            result = kernel.handle_human_message(wid, text="assign", from_node_id="node-a")

            self.assertEqual(result["loop"]["status"], "succeeded")
            timeline = store.list_timeline(wid)
            pre_tool = next(e for e in timeline if e.type == "assistant_content")
            assign_started = next(e for e in timeline if e.type == "assign_started")
            self.assertEqual(pre_tool.text, "Supervisor pre-tool content")
            self.assertLess(pre_tool.seq, assign_started.seq)
            history = store.get_run_history(result["leader_run"].run_id)
            assert history is not None
            assistant = next(m for m in history.messages if m.tool_calls)
            self.assertEqual(assistant.content, "Supervisor pre-tool content")

    def test_supervisor_non_assign_tool_is_published_as_safe_timeline_notice(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(
                    display_name="Visible supervisor tool",
                    created_by_node_id="node-a",
                    llm_profile_id="mock",
                    llm_profile_revision="1",
                )
            )
            store.publish_workgroup(group.workgroup_id)
            from manage.workgroup.llm_chat import ChatToolCall

            client = MockLLMClient(
                [
                    ChatResult(
                        content="Supervisor pre-tool content",
                        tool_calls=[
                            ChatToolCall(
                                id="call_visible_members",
                                name="list_workgroup_members",
                                arguments=json.dumps(
                                    {
                                        "purpose": "查看成员",
                                        "secret": "must stay private",
                                    },
                                    ensure_ascii=False,
                                ),
                            )
                        ],
                        finish_reason="tool_calls",
                    ),
                    ChatResult(content="成员列表已确认", finish_reason="stop"),
                ]
            )
            kernel = TurnKernel(
                store,
                chat_client=client,
                assign_completer=scripted_assign_completer,
                mock_llm=True,
            )
            result = kernel.handle_human_message(
                group.workgroup_id,
                text="查看当前成员",
                from_node_id="node-a",
            )

            self.assertEqual(result["loop"]["status"], "succeeded")
            timeline = store.list_timeline(group.workgroup_id)
            pre_tool = next(e for e in timeline if e.type == "assistant_content")
            notice = next(
                e
                for e in timeline
                if e.type == "system_notice" and e.actor_id == "leader"
            )
            final = next(e for e in timeline if e.type == "actor_final_text")
            self.assertEqual(notice.text, "查看成员")
            self.assertLess(pre_tool.seq, notice.seq)
            self.assertLess(notice.seq, final.seq)
            self.assertNotIn("list_workgroup_members", notice.text)
            self.assertNotIn("must stay private", notice.text)

    def test_supervisor_session_reuses_complete_history_across_turns(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            wid, mid = self._ready_group(store)
            from manage.workgroup.llm_chat import ChatResult, ChatToolCall

            first_text = "先查看当前成员列表"
            second_text = "请@worker读取 README"
            client = MockLLMClient(
                [
                    ChatResult(
                        tool_calls=[
                            ChatToolCall(
                                id="call_list_history",
                                name="list_workgroup_members",
                                arguments="{}",
                            )
                        ],
                        finish_reason="tool_calls",
                    ),
                    ChatResult(content="成员列表已确认", finish_reason="stop"),
                    ChatResult(
                        tool_calls=[
                            ChatToolCall(
                                id="call_assign_history",
                                name="assign_workgroup_task",
                                arguments=json.dumps(
                                    {"member_id": mid, "instruction": "读取 README"},
                                    ensure_ascii=False,
                                ),
                            )
                        ],
                        finish_reason="tool_calls",
                    ),
                    ChatResult(content="任务已完成", finish_reason="stop"),
                ]
            )
            kernel = TurnKernel(
                store,
                chat_client=client,
                assign_completer=scripted_assign_completer,
                mock_llm=True,
            )

            first = kernel.handle_human_message(wid, text=first_text, from_node_id="node-a")
            second = kernel.handle_human_message(wid, text=second_text, from_node_id="node-a")

            self.assertEqual(first["loop"]["status"], "succeeded")
            self.assertEqual(second["loop"]["status"], "succeeded")
            leader_runs = [r for r in store._runs.values() if r.actor_id == "leader"]  # noqa: SLF001
            self.assertEqual(len(leader_runs), 1)
            history = store.get_run_history(leader_runs[0].run_id)
            assert history is not None
            user_texts = [m.content for m in history.messages if m.role == "user"]
            self.assertIn(first_text, user_texts)
            self.assertIn(second_text, user_texts)
            self.assertEqual(
                [m.role for m in history.messages],
                ["user", "user", "assistant", "tool", "assistant", "user", "assistant", "tool", "assistant"],
            )

            second_turn_messages = client.calls[2]["messages"]
            second_turn_contents = [m.get("content") for m in second_turn_messages]
            self.assertIn(first_text, second_turn_contents)
            self.assertIn(second_text, second_turn_contents)
            self.assertEqual(
                [m.get("name") for m in second_turn_messages if m.get("role") == "tool"],
                ["list_workgroup_members"],
            )

    def test_supervisor_keeps_current_input_through_tool_loop(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            wid, mid = self._ready_group(store)
            from manage.workgroup.llm_chat import ChatResult, ChatToolCall

            human_text = "先看成员列表，再@worker读取 README"
            client = MockLLMClient(
                [
                    ChatResult(
                        tool_calls=[
                            ChatToolCall(
                                id="call_list_same_turn",
                                name="list_workgroup_members",
                                arguments="{}",
                            )
                        ],
                        finish_reason="tool_calls",
                    ),
                    ChatResult(
                        tool_calls=[
                            ChatToolCall(
                                id="call_assign_same_turn",
                                name="assign_workgroup_task",
                                arguments=json.dumps(
                                    {"member_id": mid, "instruction": "读取 README"},
                                    ensure_ascii=False,
                                ),
                            )
                        ],
                        finish_reason="tool_calls",
                    ),
                    ChatResult(content="任务已完成", finish_reason="stop"),
                ]
            )
            kernel = TurnKernel(
                store,
                chat_client=client,
                assign_completer=scripted_assign_completer,
                mock_llm=True,
            )
            result = kernel.handle_human_message(wid, text=human_text, from_node_id="node-a")

            self.assertEqual(result["loop"]["status"], "succeeded")
            for call in client.calls:
                self.assertIn(human_text, [m.get("content") for m in call["messages"]])
            history = store.get_run_history(result["leader_run"].run_id)
            assert history is not None
            self.assertEqual(
                [m.role for m in history.messages],
                ["user", "user", "assistant", "tool", "assistant", "tool", "assistant"],
            )

    def test_supervisor_session_history_survives_store_reload(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            db_path = Path(tmp) / "m.db"
            store1 = WorkGroupStore(db=SQLiteDatabase(db_path))
            group, _ = store1.create_workgroup(
                WorkGroupCreateRequest(
                    display_name="Reload",
                    created_by_node_id="node-a",
                    llm_profile_id="mock",
                    llm_profile_revision="1",
                )
            )
            wid = group.workgroup_id
            store1.publish_workgroup(wid)
            first_client = MockLLMClient([ChatResult(content="第一轮", finish_reason="stop")])
            first = TurnKernel(store1, chat_client=first_client, mock_llm=True).handle_human_message(
                wid,
                text="第一条消息",
                from_node_id="node-a",
                disable_tools=True,
            )
            first_run_id = first["leader_run"].run_id

            store2 = WorkGroupStore(db=SQLiteDatabase(db_path))
            second_client = MockLLMClient([ChatResult(content="第二轮", finish_reason="stop")])
            second = TurnKernel(store2, chat_client=second_client, mock_llm=True).handle_human_message(
                wid,
                text="第二条消息",
                from_node_id="node-a",
                disable_tools=True,
            )

            self.assertEqual(second["leader_run"].run_id, first_run_id)
            history = store2.get_run_history(first_run_id)
            assert history is not None
            user_texts = [m.content for m in history.messages if m.role == "user"]
            self.assertIn("第一条消息", user_texts)
            self.assertIn("第二条消息", user_texts)
            self.assertIn("第一条消息", [m.get("content") for m in second_client.calls[0]["messages"]])

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

    def test_resolved_hitl_replays_tool_result_and_resumes_leader(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            root = Path(tmp)
            store = WorkGroupStore(db=SQLiteDatabase(root / "m.db"))
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(
                    display_name="HITL resume",
                    created_by_node_id="node-a",
                    llm_profile_id="mock",
                    llm_profile_revision="1",
                )
            )
            store.publish_workgroup(group.workgroup_id)
            from manage.workgroup.history import RunHistoryMessage, ToolCall, ToolCallFunction

            run = store.create_actor_run(
                group.workgroup_id,
                ActorRunCreateRequest(actor_id="leader"),
            )
            store.append_run_history(
                run.run_id,
                [
                    RunHistoryMessage(
                        role="assistant",
                        name="leader",
                        content="",
                        tool_calls=[
                            ToolCall(
                                id="call_hitl_resume",
                                function=ToolCallFunction(
                                    name="ask_workgroup_user",
                                    arguments='{"prompt":"confirm"}',
                                ),
                            )
                        ],
                    )
                ],
            )
            hitl = store.create_hitl(
                group.workgroup_id,
                prompt="confirm",
                run_id=run.run_id,
                tool_call_id="call_hitl_resume",
            )
            store.resolve_hitl_cas(
                group.workgroup_id,
                hitl.hitl_id,
                resolution={"answer": "yes"},
            )
            restarted = WorkGroupStore(db=SQLiteDatabase(root / "m.db"))
            restarted.reconcile_inflight_runs()
            client = MockLLMClient([ChatResult(content="continued", finish_reason="stop")])
            kernel = TurnKernel(restarted, chat_client=client, mock_llm=True)

            scheduled = kernel.resume_persisted_hitls()
            self.assertEqual(len(scheduled), 1)
            self.assertTrue(scheduled[0]["scheduled"])
            for _ in range(100):
                current = restarted.get_actor_run(run.run_id)
                if current is not None and current.status == "succeeded":
                    break
                time.sleep(0.01)
            current = restarted.get_actor_run(run.run_id)
            self.assertIsNotNone(current)
            assert current is not None
            self.assertEqual(current.status, "succeeded")
            history = restarted.get_run_history(run.run_id)
            assert history is not None
            tool_results = [
                m for m in history.messages if m.role == "tool" and m.tool_call_id == "call_hitl_resume"
            ]
            self.assertEqual(len(tool_results), 1)
            self.assertIn('"answer": "yes"', tool_results[0].content or "")
            self.assertEqual(history.messages[-1].content, "continued")

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
                max_steps=1,
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

    def test_new_human_message_does_not_cancel_active_assign(self) -> None:
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
            # A new human message is serialized after the current turn; it is
            # not an implicit cancellation signal for an unrelated Assign.
            self.assertEqual(store.get_assign(stuck.assign_id).status, "running")

    def test_same_member_active_assign_enforced(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            wid, mid = self._ready_group(store)
            from manage.workgroup.models import AssignCreateRequest
            from manage.workgroup.errors import WorkgroupError

            store.create_assign(wid, AssignCreateRequest(member_id=mid, instruction="a"))
            with self.assertRaises(WorkgroupError) as ctx:
                store.create_assign(wid, AssignCreateRequest(member_id=mid, instruction="b"))
            self.assertEqual(ctx.exception.code, "conflict")

    def test_different_members_can_run_assigns_in_parallel(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            wid, first_mid = self._ready_group(store)
            store.patch_acl(
                wid,
                ACLPatchRequest(collaborators=["node-c"], expected_revision=2),
            )
            second = store.create_member(
                wid,
                MemberCreateRequest(
                    agent_id="agent-c",
                    home_node_id="node-c",
                    display_name="worker-c",
                ),
            )
            store.mark_member_status(second.member_id, "ready", workgroup_id=wid)

            from manage.workgroup.llm_chat import ChatToolCall

            calls = [
                ChatToolCall(
                    id="call_parallel_a",
                    name="assign_workgroup_task",
                    arguments=json.dumps(
                        {"member_id": first_mid, "instruction": "task-a"},
                        ensure_ascii=False,
                    ),
                ),
                ChatToolCall(
                    id="call_parallel_b",
                    name="assign_workgroup_task",
                    arguments=json.dumps(
                        {"member_id": second.member_id, "instruction": "task-b"},
                        ensure_ascii=False,
                    ),
                ),
            ]
            entered: set[str] = set()
            lock = threading.Lock()
            both_entered = threading.Event()

            def completer(_wid: str, assign_id: str, member_id: str, instruction: str, _call_id: str = "") -> str:
                with lock:
                    entered.add(member_id)
                    if len(entered) == 2:
                        both_entered.set()
                self.assertTrue(both_entered.wait(2), "assigns did not overlap")
                return f"done:{instruction}"

            client = MockLLMClient(
                [
                    ChatResult(tool_calls=calls, finish_reason="tool_calls"),
                    ChatResult(content="both done", finish_reason="stop"),
                ]
            )
            kernel = TurnKernel(
                store,
                chat_client=client,
                assign_completer=completer,
                mock_llm=True,
            )
            result = kernel.handle_human_message(
                wid,
                text="run both workers",
                from_node_id="node-a",
            )
            self.assertEqual(result["loop"]["status"], "succeeded")
            history = store.get_run_history(result["leader_run"].run_id)
            assert history is not None
            tool_messages = [m for m in history.messages if m.role == "tool"]
            self.assertEqual(
                [m.tool_call_id for m in tool_messages],
                ["call_parallel_a", "call_parallel_b"],
            )
            self.assertIn("done:task-a", tool_messages[0].content or "")
            self.assertIn("done:task-b", tool_messages[1].content or "")
            self.assertEqual(store.get_member(first_mid).status, "ready")
            self.assertEqual(store.get_member(second.member_id).status, "ready")

    def test_same_member_cannot_have_parallel_assigns(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            wid, mid = self._ready_group(store)
            from manage.workgroup.models import AssignCreateRequest
            from manage.workgroup.errors import WorkgroupError

            first = store.create_assign(
                wid,
                AssignCreateRequest(member_id=mid, instruction="first"),
            )
            self.assertEqual(store.get_member(mid).status, "busy")
            with self.assertRaises(WorkgroupError) as ctx:
                store.create_assign(
                    wid,
                    AssignCreateRequest(member_id=mid, instruction="second"),
                )
            self.assertEqual(ctx.exception.code, "conflict")
            store.set_assign_status(first.assign_id, "canceled", error_code="canceled")
            self.assertEqual(store.get_member(mid).status, "ready")

    def test_cancel_turn_cancels_all_parallel_member_runs(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            wid, first_mid = self._ready_group(store)
            store.patch_acl(
                wid,
                ACLPatchRequest(collaborators=["node-c"], expected_revision=2),
            )
            second = store.create_member(
                wid,
                MemberCreateRequest(
                    agent_id="agent-c",
                    home_node_id="node-c",
                    display_name="worker-c",
                ),
            )
            store.mark_member_status(second.member_id, "ready", workgroup_id=wid)
            from manage.workgroup.models import ActorRunCreateRequest, AssignCreateRequest

            first_assign = store.create_assign(
                wid,
                AssignCreateRequest(member_id=first_mid, instruction="first"),
            )
            second_assign = store.create_assign(
                wid,
                AssignCreateRequest(member_id=second.member_id, instruction="second"),
            )
            first_run = store.create_actor_run(
                wid,
                ActorRunCreateRequest(actor_id=first_mid, assign_id=first_assign.assign_id),
            )
            second_run = store.create_actor_run(
                wid,
                ActorRunCreateRequest(actor_id=second.member_id, assign_id=second_assign.assign_id),
            )
            kernel = TurnKernel(store, mock_llm=True)
            leader_run = kernel.start_leader_run(wid)
            kernel._begin_turn(
                wid,
                mode="leader",
                leader_run_id=leader_run.run_id,
                member_run_ids=[first_run.run_id, second_run.run_id],
            )
            result = kernel.cancel_turn(wid)
            self.assertEqual(
                set(result["canceled_assign_ids"]),
                {first_assign.assign_id, second_assign.assign_id},
            )
            self.assertEqual(store.get_assign(first_assign.assign_id).status, "canceled")
            self.assertEqual(store.get_assign(second_assign.assign_id).status, "canceled")
            finished = [
                event
                for event in store.list_timeline(wid)
                if event.type == "assign_finished" and event.assign_id
            ]
            self.assertEqual(
                {event.assign_id for event in finished},
                {first_assign.assign_id, second_assign.assign_id},
            )
            self.assertEqual(
                {store.get_actor_run(first_run.run_id).status, store.get_actor_run(second_run.run_id).status},
                {"canceled"},
            )
            self.assertEqual(store.get_member(first_mid).status, "ready")
            self.assertEqual(store.get_member(second.member_id).status, "ready")


if __name__ == "__main__":
    unittest.main()
