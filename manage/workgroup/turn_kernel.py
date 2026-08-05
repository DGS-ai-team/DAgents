"""Manage Turn Kernel：Leader LLM loop + Assign / Projector / HITL 门禁。"""

from __future__ import annotations

from typing import Any

from manage.llm.store import LLMConfigStore
from manage.workgroup.errors import WorkgroupError
from manage.workgroup.history import (
    RunHistoryMessage,
    ToolCall,
    ToolCallFunction,
    can_invoke_llm_after_tools,
    open_tool_call_ids,
)
from manage.workgroup.llm_chat import ChatResult, ChatToolCall, LLMChatClient, resolve_chat_client
from manage.workgroup.models import (
    ActorRun,
    ActorRunCreateRequest,
    Assign,
    AssignCreateRequest,
)
from manage.workgroup.native_tools import AssignCompleter, NativeToolDispatcher, leader_native_tools
from manage.workgroup.projector import project_actor_context
from manage.workgroup.protocol_names import protocol_name_for_actor
from manage.workgroup.store import WorkGroupStore

_DEFAULT_MAX_TOOL_LOOPS = 16
_LEADER_SYSTEM = (
    "You are the Workgroup Leader (Supervisor). "
    "You only orchestrate via manage-native tools; you never execute shell/fs/browser yourself. "
    "Use assign_workgroup_task to delegate work to a member. "
    "When the task is done, reply with a concise final answer for the group."
)


class TurnKernel:
    """Manage 侧 turn 编排。

    D6：Leader LLM loop（Mock/真实 LLM）+ Manage-native 工具；
    Assign 默认同步 scripted completer（Member LLM 后续接入）。
    """

    def __init__(
        self,
        store: WorkGroupStore,
        *,
        llm_store: LLMConfigStore | None = None,
        chat_client: LLMChatClient | None = None,
        assign_completer: AssignCompleter | None = None,
        max_tool_loops: int = _DEFAULT_MAX_TOOL_LOOPS,
        mock_llm: bool = False,
    ) -> None:
        self._store = store
        self._llm_store = llm_store
        self._chat_client = chat_client
        self._assign_completer = assign_completer
        self._max_tool_loops = max(1, max_tool_loops)
        self._mock_llm = mock_llm
        self._hitl_resolutions: dict[str, dict[str, Any]] = {}

    def start_leader_run(self, workgroup_id: str, *, llm_profile_revision: str | None = None) -> ActorRun:
        return self._store.create_actor_run(
            workgroup_id,
            ActorRunCreateRequest(actor_id="leader", llm_profile_revision=llm_profile_revision),
        )

    def assign_member(self, workgroup_id: str, req: AssignCreateRequest) -> Assign:
        return self._store.create_assign(workgroup_id, req)

    def project(self, *, actor_id: str, run_id: str | None = None, member_id: str | None = None) -> dict[str, Any]:
        run = self._store.get_actor_run(run_id) if run_id else None
        member = self._store.get_member(member_id) if member_id else None
        hist = self._store.get_run_history(run_id) if run_id else None
        timeline = self._store.list_timeline(run.workgroup_id) if run else []
        return project_actor_context(
            actor_id=actor_id,
            run=run,
            member=member,
            timeline_events=timeline,
            own_run_history=hist.messages if hist else [],
        )

    def resolve_hitl_cas(
        self,
        hitl_id: str,
        *,
        expected_status: str = "pending",
        resolution: dict[str, Any],
    ) -> dict[str, Any]:
        """HITL 乐观 CAS 占位：同 id 二次决议 → already_resolved。"""
        existing = self._hitl_resolutions.get(hitl_id)
        if existing is not None:
            raise WorkgroupError(
                "already_resolved",
                "HITL already resolved",
                http_status=409,
                details={"hitl_id": hitl_id, "existing": existing},
            )
        if expected_status != "pending":
            raise WorkgroupError("conflict", f"unexpected HITL status={expected_status}", http_status=409)
        stored = {"hitl_id": hitl_id, "status": "resolved", "resolution": resolution}
        self._hitl_resolutions[hitl_id] = stored
        return stored

    def handle_human_message(
        self,
        workgroup_id: str,
        *,
        text: str,
        from_node_id: str,
        client_message_id: str | None = None,
        disable_tools: bool = False,
    ) -> dict[str, Any]:
        """写入 Timeline 并驱动 Leader loop 至空闲。"""
        final: dict[str, Any] | None = None
        for ev in self.handle_human_message_events(
            workgroup_id,
            text=text,
            from_node_id=from_node_id,
            client_message_id=client_message_id,
            disable_tools=disable_tools,
        ):
            if ev.get("event") == "final":
                final = ev.get("data") or {}
        if final is None:
            raise WorkgroupError("conflict", "leader loop produced no final event", http_status=500)
        return final

    def handle_human_message_events(
        self,
        workgroup_id: str,
        *,
        text: str,
        from_node_id: str,
        client_message_id: str | None = None,
        disable_tools: bool = False,
    ):
        """写入 Timeline 并驱动 Leader loop，产出 SSE 友好事件。"""
        self._store.assert_acl_member(workgroup_id, from_node_id)
        event = self._store.append_timeline(
            workgroup_id,
            type="human_message",
            actor_id=from_node_id,
            text=text,
            client_message_id=client_message_id,
            protocol_name=protocol_name_for_actor(from_node_id),
        )
        yield {
            "event": "human",
            "data": {"timeline_event": event.model_dump(mode="json")},
        }
        run = self._store.find_running_leader_run(workgroup_id) or self.start_leader_run(workgroup_id)
        self._store.ensure_run_history(run)
        loop_result: dict[str, Any] | None = None
        for ev in self.run_leader_until_idle_events(workgroup_id, run.run_id, disable_tools=disable_tools):
            if ev.get("event") == "loop_final":
                loop_result = ev.get("data") or {}
                continue
            yield ev
        if loop_result is None:
            raise WorkgroupError("conflict", "leader loop produced no result", http_status=500)
        yield {
            "event": "final",
            "data": {
                "timeline_event": event,
                "leader_run": loop_result["run"],
                "loop": {
                    "steps": loop_result.get("steps"),
                    "status": loop_result.get("status"),
                    "final_text": loop_result.get("final_text"),
                },
            },
        }

    def run_leader_until_idle(self, workgroup_id: str, run_id: str, *, disable_tools: bool = False) -> dict[str, Any]:
        for ev in self.run_leader_until_idle_events(workgroup_id, run_id, disable_tools=disable_tools):
            if ev.get("event") == "loop_final":
                return ev["data"]
        raise WorkgroupError("conflict", "leader loop produced no result", http_status=500)

    def run_leader_until_idle_events(self, workgroup_id: str, run_id: str, *, disable_tools: bool = False):
        run = self._store.get_actor_run(run_id)
        if run is None or run.workgroup_id != workgroup_id:
            raise WorkgroupError("not_found", "actor run not found", http_status=404)
        if run.actor_id != "leader":
            raise WorkgroupError("invalid_request", "run is not a leader run")
        if run.status not in {"running", "awaiting_hitl"}:
            yield {
                "event": "loop_final",
                "data": {"run": run, "steps": 0, "status": run.status},
            }
            return

        group = self._store.require_active(workgroup_id)
        client = self._chat_client or resolve_chat_client(
            self._llm_store,
            profile_id=group.llm_profile_id,
            mock=self._mock_llm,
        )
        dispatcher = NativeToolDispatcher(
            self._store,
            leader_run_id=run_id,
            assign_completer=self._assign_completer,
        )
        tools = [] if disable_tools else leader_native_tools()
        steps = 0
        tool_loops = 0

        while True:
            hist = self._store.ensure_run_history(run)
            open_ids = open_tool_call_ids(hist.messages)
            if open_ids:
                raise WorkgroupError(
                    "conflict",
                    "open tool_calls must be paired before continuing",
                    http_status=409,
                    details={"open_tool_calls": open_ids},
                )

            projected = project_actor_context(
                actor_id="leader",
                run=run,
                timeline_events=self._store.list_timeline(workgroup_id),
                own_run_history=hist.messages,
            )
            messages = [{"role": "system", "content": _LEADER_SYSTEM}] + list(projected["messages"])
            yield {"event": "status", "data": {"phase": "thinking"}}
            result = None
            stream = getattr(client, "stream_chat", None)
            if callable(stream):
                for piece in stream(messages, tools=tools or None):
                    if piece.delta:
                        yield {"event": "delta", "data": {"text": piece.delta}}
                    if piece.result is not None:
                        result = piece.result
            else:
                result = client.chat(messages, tools=tools or None)
            if result is None:
                raise WorkgroupError("conflict", "llm stream produced no result", http_status=502)

            steps += 1
            tool_loops += 1
            if tool_loops > self._max_tool_loops:
                run = self._store.update_actor_run(run_id, status="failed")
                raise WorkgroupError(
                    "conflict",
                    "max_tool_loops exceeded",
                    http_status=409,
                    details={"max_tool_loops": self._max_tool_loops},
                )

            assistant = self._assistant_message(result)
            wm = max((e.seq for e in self._store.list_timeline(workgroup_id)), default=0)
            self._store.append_run_history(run_id, [assistant], timeline_watermark_seq=wm)
            run = self._store.get_actor_run(run_id) or run

            if not result.tool_calls:
                final_text = (result.content or "").strip() or "(empty)"
                final_event = self._store.append_timeline(
                    workgroup_id,
                    type="actor_final_text",
                    actor_id="leader",
                    text=final_text,
                    protocol_name="leader",
                )
                run = self._store.update_actor_run(run_id, status="succeeded", timeline_watermark_seq=wm)
                yield {
                    "event": "assistant_final",
                    "data": {
                        "text": final_text,
                        "timeline_event": final_event.model_dump(mode="json"),
                    },
                }
                yield {
                    "event": "loop_final",
                    "data": {
                        "run": run,
                        "steps": steps,
                        "status": "succeeded",
                        "final_text": final_text,
                    },
                }
                return

            if disable_tools:
                raise WorkgroupError(
                    "invalid_request",
                    "leader tools are disabled for this message, but model returned tool_calls",
                    http_status=409,
                )
            tool_msgs: list[RunHistoryMessage] = []
            for tc in result.tool_calls:
                yield {
                    "event": "status",
                    "data": {"phase": "tool", "tool": tc.name, "tool_call_id": tc.id},
                }
                content = dispatcher.dispatch(
                    workgroup_id=workgroup_id,
                    tool_name=tc.name,
                    tool_call_id=tc.id,
                    arguments_json=tc.arguments,
                )
                tool_msgs.append(
                    RunHistoryMessage(
                        role="tool",
                        tool_call_id=tc.id,
                        name=tc.name,
                        content=content,
                    )
                )
            ok, wait = can_invoke_llm_after_tools(
                [{"id": tc.id, "name": tc.name} for tc in result.tool_calls],
                [{"tool_call_id": m.tool_call_id} for m in tool_msgs],
            )
            if not ok:
                raise WorkgroupError(
                    "conflict",
                    "parallel tool_calls incompletely paired",
                    details={"wait_for": wait},
                )
            wm = max((e.seq for e in self._store.list_timeline(workgroup_id)), default=wm)
            self._store.append_run_history(run_id, tool_msgs, timeline_watermark_seq=wm)
            run = self._store.get_actor_run(run_id) or run

    @staticmethod
    def _assistant_message(result: ChatResult) -> RunHistoryMessage:
        tool_calls = None
        if result.tool_calls:
            tool_calls = [
                ToolCall(
                    id=tc.id,
                    function=ToolCallFunction(name=tc.name, arguments=tc.arguments or "{}"),
                )
                for tc in result.tool_calls
            ]
        return RunHistoryMessage(
            role="assistant",
            name="leader",
            content=result.content or ("" if tool_calls else ""),
            tool_calls=tool_calls,
        )


def mock_leader_script_assign_then_answer(
    *,
    member_id: str,
    instruction: str = "读 README",
    final_text: str = "已完成",
) -> list[ChatResult]:
    """测试用：第一步 assign，第二步终态文本。"""
    import json

    args = json.dumps({"member_id": member_id, "instruction": instruction}, ensure_ascii=False)
    return [
        ChatResult(
            content="",
            tool_calls=[
                ChatToolCall(
                    id="call_as1",
                    name="assign_workgroup_task",
                    arguments=args,
                )
            ],
            finish_reason="tool_calls",
        ),
        ChatResult(content=final_text, finish_reason="stop"),
    ]
