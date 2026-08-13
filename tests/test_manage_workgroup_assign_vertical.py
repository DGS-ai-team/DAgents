"""对话路径 assign → VerticalLoop 真工具 + WS inbound 闭环。"""

from __future__ import annotations

import importlib.util
import sys
import threading
import time
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

from fastapi.testclient import TestClient

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.config import ManageSettings  # noqa: E402
from manage.manage_app import create_app  # noqa: E402
from manage.storage.sqlite import SQLiteDatabase  # noqa: E402
from manage.workgroup.llm_chat import MockLLMClient  # noqa: E402
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
from manage.workgroup.vertical import VerticalLoop, path_from_instruction  # noqa: E402


def _load_fake_bridge():
    path = _ROOT / "tests" / "test_manage_workgroup_vertical.py"
    spec = importlib.util.spec_from_file_location("test_manage_workgroup_vertical", path)
    assert spec and spec.loader
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod.FakeNodeBridge


FakeNodeBridge = _load_fake_bridge()


class PathFromInstructionTests(unittest.TestCase):
    def test_defaults_and_tokens(self) -> None:
        self.assertEqual(path_from_instruction(""), "README")
        self.assertEqual(path_from_instruction("读 README"), "README")
        self.assertEqual(path_from_instruction('请读 "notes/a.md"'), "notes/a.md")
        self.assertEqual(path_from_instruction("打开 foo/bar.txt 并摘要"), "foo/bar.txt")

    def test_rejects_host_absolute_paths(self) -> None:
        from manage.workgroup.errors import WorkgroupError
        from manage.workgroup.vertical import validate_member_read_path

        with self.assertRaises(WorkgroupError) as ctx:
            validate_member_read_path(r"D:\Program Files\DAgents\config.yaml")
        self.assertEqual(ctx.exception.code, "not_authorized")
        self.assertIn("relative", ctx.exception.message.lower())

        with self.assertRaises(WorkgroupError) as ctx2:
            validate_member_read_path("/etc/passwd")
        self.assertEqual(ctx2.exception.code, "not_authorized")

        self.assertEqual(validate_member_read_path("README"), "README")
        self.assertEqual(validate_member_read_path("notes/a.md"), "notes/a.md")


class AssignVerticalLoopTests(unittest.TestCase):
    def _ready(self, tmp: str, *, with_bridge: bool = True):
        store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "manage.db"))
        bridge = FakeNodeBridge(Path(tmp) / "node-ws", node_id="node-b") if with_bridge else None
        loop = VerticalLoop(store, bridge=bridge, command_timeout_s=2.0)
        group, _ = store.create_workgroup(
            WorkGroupCreateRequest(
                display_name="Vert",
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
                home_node_id="node-b",
                display_name="reader",
                allow_tool_names=["read_file"],
                prompt={"soul_md": "reader"},
            ),
        )
        if with_bridge:
            loop.enqueue_provision(group.workgroup_id, member.member_id)
        else:
            store.mark_member_status(
                member.member_id,
                "ready",
                workgroup_id=group.workgroup_id,
                workspace_path=str(Path(tmp) / "ws"),
                tool_catalog_revision="rev_test",
            )
        store.publish_workgroup(group.workgroup_id)
        return store, loop, bridge, group.workgroup_id, member.member_id

    def test_leader_assign_uses_bridge_read_file(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, loop, bridge, wid, mid = self._ready(tmp, with_bridge=True)
            assert bridge is not None
            script = mock_leader_script_assign_then_answer(
                member_id=mid,
                instruction="读 README",
                final_text="已根据文件内容回复",
            )
            member_script = mock_member_script_read_file_then_answer(
                path="README",
                final_text="标题是 Demo",
            )
            kernel = TurnKernel(
                store,
                chat_client=MockLLMClient(script),
                member_chat_client=MockLLMClient(member_script),
            )
            kernel.set_assign_completer(loop.make_assign_completer(kernel))
            result = kernel.handle_human_message(
                wid,
                text="请安排成员读 README",
                from_node_id="node-a",
            )
            self.assertEqual(result["loop"]["status"], "succeeded")
            self.assertEqual(result["loop"]["final_text"], "已根据文件内容回复")

            hist = store.get_run_history(result["leader_run"].run_id)
            assert hist is not None
            tool = next(m for m in hist.messages if m.role == "tool")
            self.assertIn("Demo", tool.content or "")
            self.assertNotIn("[scripted]", tool.content or "")
            self.assertEqual(sum(bridge.executions.values()), 1)

            timeline = store.list_timeline(wid)
            member_finals = [
                e for e in timeline if e.type == "actor_final_text" and e.actor_id == mid
            ]
            self.assertEqual(len(member_finals), 1)
            self.assertIn("Demo", member_finals[0].text)

    def test_member_loop_glob_write_and_allowlist(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, loop, bridge, wid, mid = self._ready(tmp, with_bridge=True)
            assert bridge is not None
            # expand allowlist to include glob + write
            from manage.workgroup.models import MemberCreateRequest

            # recreate member with full tools via patching spec allow list in store internals
            spec = store.get_spec(mid)
            assert spec is not None
            tools = spec.tools.model_copy(
                update={"allow_names": ["read_file", "glob_files", "write_file"]}
            )
            store._specs[mid] = spec.model_copy(update={"tools": tools})  # noqa: SLF001
            # refresh binding allow via re-provision
            loop.enqueue_provision(wid, mid)

            import json

            from manage.workgroup.llm_chat import ChatResult, ChatToolCall, MockLLMClient
            from manage.workgroup.turn_kernel import mock_leader_script_assign_then_answer

            leader_script = mock_leader_script_assign_then_answer(
                member_id=mid,
                instruction="列出文件并写 out.txt",
                final_text="组长确认完成",
            )
            member_script = [
                ChatResult(
                    content="",
                    tool_calls=[
                        ChatToolCall(
                            id="call_g1",
                            name="glob_files",
                            arguments=json.dumps(
                                {"directory": ".", "glob_pattern": "*"}, ensure_ascii=False
                            ),
                        )
                    ],
                    finish_reason="tool_calls",
                ),
                ChatResult(
                    content="",
                    tool_calls=[
                        ChatToolCall(
                            id="call_w1",
                            name="write_file",
                            arguments=json.dumps(
                                {"path": "out.txt", "content": "ok"}, ensure_ascii=False
                            ),
                        )
                    ],
                    finish_reason="tool_calls",
                ),
                ChatResult(content="已写入 out.txt", finish_reason="stop"),
            ]
            kernel = TurnKernel(
                store,
                chat_client=MockLLMClient(leader_script),
                member_chat_client=MockLLMClient(member_script),
            )
            kernel.set_assign_completer(loop.make_assign_completer(kernel))
            result = kernel.handle_human_message(
                wid, text="请成员列目录并写文件", from_node_id="node-a"
            )
            self.assertEqual(result["loop"]["final_text"], "组长确认完成")
            self.assertEqual(sum(bridge.executions.values()), 2)

            # unknown tool rejected in member loop without Node call
            member_script2 = [
                ChatResult(
                    content="",
                    tool_calls=[
                        ChatToolCall(
                            id="call_bad",
                            name="bash_run",
                            arguments="{}",
                        )
                    ],
                    finish_reason="tool_calls",
                ),
                ChatResult(content="无法使用 bash", finish_reason="stop"),
            ]
            # need another assign — previous succeeded so ok
            leader2 = mock_leader_script_assign_then_answer(
                member_id=mid,
                instruction="试 bash",
                final_text="已拒绝",
            )
            kernel2 = TurnKernel(
                store,
                chat_client=MockLLMClient(leader2),
                member_chat_client=MockLLMClient(member_script2),
            )
            kernel2.set_assign_completer(loop.make_assign_completer(kernel2))
            before = sum(bridge.executions.values())
            result2 = kernel2.handle_human_message(wid, text="再试", from_node_id="node-a")
            self.assertEqual(result2["loop"]["final_text"], "已拒绝")
            self.assertEqual(sum(bridge.executions.values()), before)

            runs = [r for r in store._runs.values() if r.actor_id == mid]  # noqa: SLF001
            self.assertEqual(len(runs), 1)
            mhist = store.get_run_history(runs[0].run_id)
            assert mhist is not None
            tool_msg = next(
                m
                for m in reversed(mhist.messages)
                if m.role == "tool"
            )
            self.assertIn("allowlist", (tool_msg.content or "").lower())

    def test_member_session_reuses_complete_history_across_assigns(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, loop, bridge, wid, mid = self._ready(tmp, with_bridge=True)
            assert bridge is not None
            import json

            from manage.workgroup.llm_chat import ChatResult, ChatToolCall, MockLLMClient
            from manage.workgroup.turn_kernel import mock_leader_script_assign_then_answer

            first_instruction = "读取 README"
            second_instruction = "再次确认 README"
            leader_client = MockLLMClient(
                mock_leader_script_assign_then_answer(
                    member_id=mid,
                    instruction=first_instruction,
                    final_text="第一项完成",
                )
                + mock_leader_script_assign_then_answer(
                    member_id=mid,
                    instruction=second_instruction,
                    final_text="第二项完成",
                )
            )
            member_client = MockLLMClient(
                [
                    ChatResult(
                        tool_calls=[
                            ChatToolCall(
                                id="call_member_read_1",
                                name="read_file",
                                arguments=json.dumps({"path": "README"}),
                            )
                        ],
                        finish_reason="tool_calls",
                    ),
                    ChatResult(content="第一次读取完成", finish_reason="stop"),
                    ChatResult(content="第二次读取完成", finish_reason="stop"),
                ]
            )
            kernel = TurnKernel(
                store,
                chat_client=leader_client,
                member_chat_client=member_client,
            )
            kernel.set_assign_completer(loop.make_assign_completer(kernel))

            first = kernel.handle_human_message(wid, text="安排第一次读取", from_node_id="node-a")
            second = kernel.handle_human_message(wid, text="安排第二次读取", from_node_id="node-a")

            self.assertEqual(first["loop"]["status"], "succeeded")
            self.assertEqual(second["loop"]["status"], "succeeded")
            member_runs = [r for r in store._runs.values() if r.actor_id == mid]  # noqa: SLF001
            self.assertEqual(len(member_runs), 1)
            history = store.get_run_history(member_runs[0].run_id)
            assert history is not None
            user_texts = [m.content for m in history.messages if m.role == "user"]
            self.assertIn(first_instruction, user_texts)
            self.assertIn(second_instruction, user_texts)
            self.assertGreaterEqual(sum(m.role == "assistant" for m in history.messages), 3)
            self.assertGreaterEqual(sum(m.role == "tool" for m in history.messages), 1)

            second_member_messages = member_client.calls[2]["messages"]
            second_member_contents = [m.get("content") for m in second_member_messages]
            self.assertIn(first_instruction, second_member_contents)
            self.assertIn(second_instruction, second_member_contents)
            self.assertEqual(
                [m.get("name") for m in second_member_messages if m.get("role") == "tool"],
                ["read_file"],
            )

    def test_ws_inbound_tool_result_wakes_waiter(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, loop, _, wid, mid = self._ready(tmp, with_bridge=False)
            from manage.workgroup.models import AssignCreateRequest

            assign = store.create_assign(
                wid,
                AssignCreateRequest(member_id=mid, instruction="读 README"),
            )
            dispatched = loop.dispatch_read_file_for_assign(
                wid,
                assign_id=assign.assign_id,
                member_id=mid,
                tool_call_id="call_ws_1",
                path="README",
            )
            cmd = dispatched["command"]
            self.assertIsNone(dispatched.get("tool_result"))

            def later() -> None:
                time.sleep(0.05)
                loop.handle_inbound(
                    "node-b",
                    "tool.result",
                    {
                        "workgroup_id": wid,
                        "command_id": cmd["command_id"],
                        "assign_id": assign.assign_id,
                        "member_id": mid,
                        "status": "succeeded",
                        "result_text": "hello from node",
                    },
                )

            threading.Thread(target=later, daemon=True).start()
            got = loop.wait_command_result(cmd["command_id"], timeout_s=2.0)
            self.assertEqual(got["status"], "succeeded")
            self.assertEqual(got["result_text"], "hello from node")

    def test_create_app_ws_inbound_completes_provision(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            store = app.state.workgroup_store
            loop = app.state.workgroup_loop
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(display_name="ws-in", created_by_node_id="node-b")
            )
            wid = group.workgroup_id
            member, _ = store.create_member(
                wid,
                MemberCreateRequest(
                    home_node_id="node-b",
                    display_name="w",
                    allow_tool_names=["read_file"],
                ),
            )
            mid = member.member_id
            self.assertEqual(store.get_member(mid).status, "provisioning")
            frame = loop.enqueue_provision(wid, mid)

            with TestClient(app) as client:
                with client.websocket_connect(
                    "/v1/workgroups/ws",
                    headers={"x-dagents-agent-id": "node-b"},
                ) as ws:
                    ws.send_json(
                        {
                            "type": "session.hello",
                            "payload": {"node_id": "node-b", "last_ack_delivery_seq": 0},
                        }
                    )
                    welcome = ws.receive_json()
                    gen = welcome["payload"]["connection_generation"]
                    ws.send_json(
                        {
                            "type": "member.provision_result",
                            "payload": {
                                "workgroup_id": wid,
                                "member_id": mid,
                                "provision_id": frame.payload["provision_id"],
                                "workspace_path": str(Path(tmp) / "ws"),
                                "tool_catalog_revision": "rev_ws",
                                "status": "ready",
                                "delivery_seq": frame.delivery_seq,
                                "connection_generation": gen,
                            },
                        }
                    )
                    acked = ws.receive_json()
                    self.assertEqual(acked["type"], "delivery.acked", acked)

            member2 = store.get_member(mid)
            assert member2 is not None
            self.assertEqual(member2.status, "ready")

    def test_cancel_wakes_wait_command_result(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, loop, _, wid, mid = self._ready(tmp, with_bridge=False)
            from manage.workgroup.models import AssignCreateRequest

            assign = store.create_assign(
                wid,
                AssignCreateRequest(member_id=mid, instruction="读 README"),
            )
            store.set_assign_status(assign.assign_id, "running")
            dispatched = loop.dispatch_read_file_for_assign(
                wid,
                assign_id=assign.assign_id,
                member_id=mid,
                tool_call_id="call_cancel_wait",
                path="README",
            )
            cmd_id = dispatched["command"]["command_id"]
            box: dict = {}

            def waiter() -> None:
                box["r"] = loop.wait_command_result(cmd_id, timeout_s=3.0)

            t = threading.Thread(target=waiter, daemon=True)
            t.start()
            time.sleep(0.05)
            woke = loop.cancel_pending_commands(wid)
            self.assertIn(cmd_id, woke)
            t.join(timeout=2)
            self.assertFalse(t.is_alive())
            self.assertEqual(box["r"]["status"], "canceled")
            self.assertEqual(box["r"]["error_code"], "canceled")
            cancel_frames = [
                f for f in store.list_outbox(wid) if f.type == "tool.cancel"
            ]
            self.assertTrue(cancel_frames)
            self.assertEqual(cancel_frames[-1].payload.get("command_id"), cmd_id)

    def test_late_tool_result_does_not_revive_failed_assign(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, loop, _, wid, mid = self._ready(tmp, with_bridge=False)
            from manage.workgroup.d3_models import ToolResultApplyRequest
            from manage.workgroup.models import AssignCreateRequest

            assign = store.create_assign(
                wid,
                AssignCreateRequest(member_id=mid, instruction="读 README"),
            )
            store.set_assign_status(assign.assign_id, "running")
            dispatched = loop.dispatch_read_file_for_assign(
                wid,
                assign_id=assign.assign_id,
                member_id=mid,
                tool_call_id="call_late",
                path="README",
            )
            cmd_id = dispatched["command"]["command_id"]
            store.fail_active_assigns(wid, reason="cancelled by user", error_code="canceled")
            failed = store.get_assign(assign.assign_id)
            assert failed is not None
            self.assertEqual(failed.status, "failed")

            out = loop.apply_tool_result(
                wid,
                ToolResultApplyRequest(
                    command_id=cmd_id,
                    assign_id=assign.assign_id,
                    member_id=mid,
                    status="succeeded",
                    result_text="too late",
                ),
            )
            self.assertTrue(out.get("ignored_assign_update"))
            again = store.get_assign(assign.assign_id)
            assert again is not None
            self.assertEqual(again.status, "failed")

    def test_cancel_turn_hook_wakes_pending_command(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store, loop, _, wid, mid = self._ready(tmp, with_bridge=False)
            from manage.workgroup.models import AssignCreateRequest

            assign = store.create_assign(
                wid,
                AssignCreateRequest(member_id=mid, instruction="慢工具"),
            )
            store.set_assign_status(assign.assign_id, "running")
            dispatched = loop.dispatch_read_file_for_assign(
                wid,
                assign_id=assign.assign_id,
                member_id=mid,
                tool_call_id="call_hook",
                path="README",
            )
            cmd_id = dispatched["command"]["command_id"]
            kernel = TurnKernel(store, mock_llm=True)
            kernel.set_command_cancel_hook(loop.cancel_pending_commands)
            kernel._begin_turn(wid, mode="leader", leader_run_id="lr1")
            box: dict = {}

            def waiter() -> None:
                box["r"] = loop.wait_command_result(cmd_id, timeout_s=3.0)

            t = threading.Thread(target=waiter, daemon=True)
            t.start()
            time.sleep(0.05)
            out = kernel.cancel_turn(wid)
            self.assertTrue(out["cancelled"])
            t.join(timeout=2)
            self.assertFalse(t.is_alive())
            self.assertEqual(box["r"]["status"], "canceled")
            self.assertEqual(store.get_assign(assign.assign_id).status, "failed")

    def test_wait_timeout_mentions_dialer_offline(self) -> None:
        from manage.workgroup.errors import WorkgroupError
        from manage.workgroup.ws_hub import WorkgroupWSHub

        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "manage.db"))
            hub = WorkgroupWSHub(store)
            loop = VerticalLoop(store, hub=hub, command_timeout_s=0.3)
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(
                    display_name="TimeoutHint",
                    created_by_node_id="node-a",
                    llm_profile_id="mock",
                    llm_profile_revision="1",
                )
            )
            wid = group.workgroup_id
            store.patch_acl(
                wid,
                ACLPatchRequest(collaborators=["node-b"], expected_revision=1),
            )
            member, _ = store.create_member(
                wid,
                MemberCreateRequest(
                    home_node_id="node-b",
                    display_name="m",
                    allow_tool_names=["read_file"],
                ),
            )
            mid = member.member_id
            store.mark_member_status(
                mid,
                "ready",
                workgroup_id=wid,
                workspace_path=str(Path(tmp) / "ws"),
                tool_catalog_revision="rev_to",
            )
            store.publish_workgroup(wid)
            from manage.workgroup.models import AssignCreateRequest

            assign = store.create_assign(
                wid,
                AssignCreateRequest(member_id=mid, instruction="读 README"),
            )
            dispatched = loop.dispatch_read_file_for_assign(
                wid,
                assign_id=assign.assign_id,
                member_id=mid,
                tool_call_id="call_timeout_hint",
                path="README",
            )
            cmd_id = dispatched["command"]["command_id"]
            with self.assertRaises(WorkgroupError) as ctx:
                loop.wait_command_result(cmd_id, timeout_s=0.3)
            self.assertIn("dialer not connected", ctx.exception.message)
            self.assertIn("not tool-approval", ctx.exception.message)


if __name__ == "__main__":
    unittest.main()
