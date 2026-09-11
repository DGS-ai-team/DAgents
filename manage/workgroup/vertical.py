"""AgentRef 纵向编排：human → assign → Node turn → Timeline。"""

from __future__ import annotations

import threading
import time
from typing import TYPE_CHECKING, Any

from manage.workgroup.d3_models import (
    HITLCreateRequest,
    HITLRequest,
    HITLResolveRequest,
    HumanPostRequest,
    OutboxFrame,
    TimelineEvent,
)
from manage.workgroup.assignment_service import AssignmentService
from manage.workgroup.errors import WorkgroupError
from manage.workgroup.protocol_names import protocol_name_for_actor
from manage.workgroup.store import WorkGroupStore

if TYPE_CHECKING:
    from manage.workgroup.turn_kernel import TurnKernel

# A member AgentRef turn remains suspended while its Node-side Agent waits for
# a human tool approval. One minute is too short for a real operator to
# inspect the request; keep the wait aligned with the ten-minute UI contract.
_DEFAULT_AGENT_TURN_TIMEOUT_S = 10 * 60.0
_READ_FILE_TOOL = "read_file"


class VerticalLoop:
    """Manage-side AgentRef orchestration loop."""

    def __init__(
        self,
        store: WorkGroupStore,
        hub: Any | None = None,
        *,
        turn_timeout_s: float = _DEFAULT_AGENT_TURN_TIMEOUT_S,
    ) -> None:
        self.store = store
        self.hub = hub  # WorkgroupWSHub；有连接时优先推送 outbox
        self.turn_timeout_s = max(0.1, float(turn_timeout_s))
        self.assignments = AssignmentService(store)
        self._lock = threading.Lock()
        # AgentRef session/turn waiters are keyed by assign_id. They are
        # process-local only; the reliable start frame remains in the outbox.
        self._agent_waiters: dict[str, threading.Event] = {}
        self._agent_results: dict[str, dict[str, Any]] = {}
        self._turn_kernel: TurnKernel | None = None

    def set_turn_kernel(self, kernel: TurnKernel | None) -> None:
        self._turn_kernel = kernel

    # --- Timeline / Outbox / HITL 委托 store ---

    def post_human(self, workgroup_id: str, req: HumanPostRequest) -> TimelineEvent:
        self.store.assert_acl_member(workgroup_id, req.from_node_id)
        self.store.require_active(workgroup_id)
        return self.store.append_timeline(
            workgroup_id,
            type="human_message",
            actor_id=req.from_node_id,
            text=req.text,
            client_message_id=req.client_message_id,
            direct_member_id=req.direct_member_id,
        )

    def enqueue_agent_session_open(self, workgroup_id: str, member_id: str) -> OutboxFrame:
        """Bind a Workgroup member to an existing Node Agent session."""
        ctx = self.store.member_execution_context(member_id)
        frame = self.store.enqueue_outbox(
            workgroup_id,
            type="agent.session.open",
            payload={
                "workgroup_id": workgroup_id,
                "member_id": member_id,
                "agent_id": str(ctx.get("agent_id") or ""),
                "session_id": str(ctx.get("session_id") or ""),
                "home_node_id": str(ctx.get("home_node_id") or ""),
            },
        )
        if self.hub is not None:
            self.hub.deliver_outbox_frame(frame, home_node_id=ctx["home_node_id"])
            self.hub.request_resume(ctx["home_node_id"], workgroup_id)
        return frame

    def enqueue_agent_turn_start(self, workgroup_id: str, assign_id: str) -> OutboxFrame:
        assign = self.store.get_assign(assign_id)
        if assign is None or assign.workgroup_id != workgroup_id:
            raise WorkgroupError("not_found", "assign not found", http_status=404)
        ctx = self.store.member_execution_context(assign.member_id)
        with self._lock:
            self._agent_waiters.setdefault(assign_id, threading.Event())
        frame = self.store.enqueue_outbox(
            workgroup_id,
            type="agent.turn.start",
            payload={
                "workgroup_id": workgroup_id,
                "member_id": assign.member_id,
                "agent_id": str(ctx.get("agent_id") or ""),
                "session_id": str(ctx.get("session_id") or ""),
                "assign_id": assign_id,
                "source": assign.source,
                "parent_turn_id": assign.parent_turn_id,
                "child_turn_id": assign.child_turn_id,
                "attempt_id": assign.attempt_id,
                "user_message": assign.instruction,
                "client_message_id": assign.assign_id,
                "home_node_id": str(ctx.get("home_node_id") or ""),
            },
        )
        if self.hub is not None:
            self.hub.deliver_outbox_frame(frame, home_node_id=ctx["home_node_id"])
            self.hub.request_resume(ctx["home_node_id"], workgroup_id)
        return frame

    def enqueue_agent_turn_resume(
        self,
        workgroup_id: str,
        hitl_id: str,
        resolution: dict[str, Any],
    ) -> OutboxFrame:
        """Return a resolved Node AgentRef HITL to its owning session."""
        hitl = self.store.get_hitl(hitl_id)
        if hitl is None or hitl.workgroup_id != workgroup_id:
            raise WorkgroupError("not_found", "hitl not found", http_status=404)
        meta = dict(hitl.metadata or {})
        if meta.get("source") != "agent_ref":
            raise WorkgroupError("conflict", "hitl is not bound to an AgentRef", http_status=409)
        required = ("member_id", "agent_id", "session_id", "assign_id", "home_node_id")
        if any(not str(meta.get(key) or "").strip() for key in required):
            raise WorkgroupError("conflict", "agent_ref hitl routing metadata is incomplete", http_status=409)
        assign = self.store.get_assign(str(meta["assign_id"]))
        if assign is None or assign.workgroup_id != workgroup_id:
            raise WorkgroupError("not_found", "assign not found", http_status=404)
        payload = {
            "workgroup_id": workgroup_id,
            "member_id": str(meta["member_id"]),
            "agent_id": str(meta["agent_id"]),
            "session_id": str(meta["session_id"]),
            "assign_id": str(meta["assign_id"]),
            "child_turn_id": assign.child_turn_id,
            "attempt_id": assign.attempt_id,
            "hitl_id": str(meta.get("node_hitl_id") or ""),
            "resume_value": dict(resolution or {}),
            "home_node_id": str(meta["home_node_id"]),
        }
        frame = self.store.enqueue_outbox(workgroup_id, type="agent.turn.resume", payload=payload)
        if self.hub is not None:
            self.hub.deliver_outbox_frame(frame, home_node_id=payload["home_node_id"])
            # `deliver_outbox_frame` is the live path. Do not immediately call
            # request_resume as well: on an already connected Node that would
            # replay the same resume before the live delivery ACK and queue a
            # second continuation. The durable outbox is replayed on the next
            # reconnect, which is the only gap-fill path needed here.
        return frame

    def wait_agent_turn(
        self,
        assign_id: str,
        *,
        timeout_s: float | None = None,
        cancel_check: Any | None = None,
    ) -> dict[str, Any]:
        with self._lock:
            event = self._agent_waiters.get(assign_id)
        if event is None:
            raise WorkgroupError("conflict", "agent turn waiter not found", http_status=500)
        deadline = time.monotonic() + (self.turn_timeout_s if timeout_s is None else max(0.1, float(timeout_s)))
        while True:
            if cancel_check is not None and cancel_check():
                raise WorkgroupError("canceled", "workgroup turn cancelled", http_status=409)
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                with self._lock:
                    self._agent_waiters.pop(assign_id, None)
                raise WorkgroupError("conflict", "agent turn timed out", http_status=409, retryable=True)
            if event.wait(min(0.2, remaining)):
                break
        with self._lock:
            result = dict(self._agent_results.pop(assign_id, {}) or {})
            self._agent_waiters.pop(assign_id, None)
        if not result:
            raise WorkgroupError("conflict", "agent turn result missing after wait", http_status=500)
        return result

    def run_agent_ref_assign(
        self,
        workgroup_id: str,
        assign_id: str,
        member_id: str,
        instruction: str,
        *,
        cancel_check: Any | None = None,
        timeout_s: float | None = None,
    ) -> str:
        """Run one assignment on an existing Node Agent session."""
        member = self.store.get_member(member_id)
        if member is None or member.workgroup_id != workgroup_id:
            raise WorkgroupError("not_found", "member not found", http_status=404)
        if member.status != "ready" and not (
            member.status == "busy" and member.active_assign_id == assign_id
        ):
            raise WorkgroupError("conflict", "agent session is not ready", http_status=409)
        self.enqueue_agent_turn_start(workgroup_id, assign_id)
        result = self.wait_agent_turn(assign_id, timeout_s=timeout_s, cancel_check=cancel_check)
        status = str(result.get("status") or "failed").lower()
        if status in {"canceled", "cancelled"}:
            raise WorkgroupError("canceled", "agent turn cancelled", http_status=409)
        if status not in {"succeeded", "awaiting"}:
            raise WorkgroupError(
                str(result.get("error_code") or "agent_turn_failed"),
                str(result.get("message") or "agent turn failed"),
                http_status=409,
            )
        return str(result.get("final_text") or "").strip()[:8000] or "(empty)"

    def enqueue_agent_session_close(self, workgroup_id: str, member_id: str) -> OutboxFrame:
        """Close the Node Agent session bound to a workgroup member."""
        member = self.store.get_member(member_id)
        if member is None or member.workgroup_id != workgroup_id:
            raise WorkgroupError("not_found", "member not found", http_status=404)
        ctx = self.store.member_execution_context(member_id)
        payload = {
            "workgroup_id": workgroup_id,
            "member_id": member_id,
            "agent_id": member.agent_id,
            "session_id": member.session_id,
            "home_node_id": ctx["home_node_id"],
        }
        frame = self.store.enqueue_outbox(
            workgroup_id,
            type="agent.session.close",
            payload=payload,
        )
        if self.hub is not None:
            self.hub.deliver_outbox_frame(frame, home_node_id=ctx["home_node_id"])
            self.hub.request_resume(ctx["home_node_id"], workgroup_id)
        return frame

    def enqueue_agent_tool_cancel(
        self, workgroup_id: str, assign_id: str, tool_call_id: str
    ) -> OutboxFrame:
        """Cancel the current cancellable tool through the owning Node."""
        assign = self.store.get_assign(assign_id)
        if assign is None or assign.workgroup_id != workgroup_id:
            raise WorkgroupError("not_found", "assign not found", http_status=404)
        call_id = str(tool_call_id or "").strip()
        event = next(
            (
                item
                for item in reversed(self.store.list_timeline(workgroup_id))
                if item.assign_id == assign_id
                and item.tool_call_id == call_id
                and item.type == "tool_started"
                and item.status == "running"
            ),
            None,
        )
        if event is None or not event.tool_name:
            raise WorkgroupError(
                "tool_not_cancellable",
                "tool execution is not currently cancellable",
                http_status=409,
            )
        ctx = self.store.member_execution_context(assign.member_id)
        payload = {
            "workgroup_id": workgroup_id,
            "member_id": assign.member_id,
            "agent_id": str(ctx.get("agent_id") or ""),
            "session_id": str(ctx.get("session_id") or ""),
            "assign_id": assign_id,
            "tool_call_id": call_id,
            "tool_name": event.tool_name,
            "home_node_id": str(ctx.get("home_node_id") or ""),
        }
        frame = self.store.enqueue_outbox(
            workgroup_id,
            type="agent.tool.cancel",
            payload=payload,
        )
        if self.hub is not None and payload["home_node_id"]:
            self.hub.deliver_outbox_frame(frame, home_node_id=payload["home_node_id"])
        return frame

    def cancel_pending_agent_turns(
        self, workgroup_id: str, *, assign_id: str | None = None
    ) -> list[str]:
        """通过已建立的 Node→Manage WS 取消工作组内 AgentRef turn。"""
        canceled: list[str] = []
        for assign in self.store.list_assigns(workgroup_id, active_only=True):
            if assign_id is not None and assign.assign_id != assign_id:
                continue
            ctx = self.store.member_execution_context(assign.member_id)
            payload = {
                "workgroup_id": workgroup_id,
                "member_id": assign.member_id,
                "agent_id": str(ctx.get("agent_id") or ""),
                "session_id": str(ctx.get("session_id") or ""),
                "assign_id": assign.assign_id,
                "child_turn_id": assign.child_turn_id,
                "attempt_id": assign.attempt_id,
                "home_node_id": str(ctx.get("home_node_id") or ""),
            }
            try:
                frame = self.store.enqueue_outbox(
                    workgroup_id,
                    type="agent.turn.cancel",
                    payload=payload,
                )
                if self.hub is not None and payload["home_node_id"]:
                    self.hub.deliver_outbox_frame(frame, home_node_id=payload["home_node_id"])
            except Exception:  # noqa: BLE001 — 本地取消仍需唤醒 waiter
                pass
            self._signal_agent_result(
                assign.assign_id,
                {
                    **payload,
                    "status": "canceled",
                    "final_text": "",
                    "error_code": "canceled",
                    "message": "agent turn canceled",
                },
            )
            canceled.append(assign.assign_id)
        return canceled

    def _signal_agent_result(self, assign_id: str, result: dict[str, Any]) -> None:
        with self._lock:
            self._agent_results[assign_id] = dict(result)
            waiter = self._agent_waiters.get(assign_id)
        if waiter is not None:
            waiter.set()

    def handle_inbound(self, node_id: str, mtype: str, payload: dict[str, Any]) -> None:
        """处理 Node Agent session/turn 事件并唤醒等待中的 assignment。"""
        _ = node_id
        if mtype in {"agent.session.ready", "agent.session.error", "agent.session.closed"}:
            workgroup_id = str(payload.get("workgroup_id") or "").strip()
            member_id = str(payload.get("member_id") or "").strip()
            if not workgroup_id or not member_id:
                return
            member = self.store.get_member(member_id)
            if member is None or member.workgroup_id != workgroup_id or member.status == "archived":
                # An archive is authoritative. A close/ready/error response
                # from the old connection must not resurrect that member.
                return
            status = str(payload.get("status") or "error").strip().lower()
            if mtype == "agent.session.ready" and status == "ready":
                self.store.mark_member_status(member_id, "ready", workgroup_id=workgroup_id)
                if self.hub is not None:
                    self.hub.publish_realtime_event(
                        workgroup_id,
                        "agent_status",
                        {"member_id": member_id, "status": status},
                    )
            elif mtype == "agent.session.closed":
                self.store.mark_member_status(member_id, "provisioning", workgroup_id=workgroup_id)
            else:
                self.store.mark_member_status(
                    member_id,
                    "error",
                    workgroup_id=workgroup_id,
                    error_code=str(payload.get("error_code") or "agent_session_error"),
                    error_message=str(payload.get("message") or "agent session failed"),
                )
            return
        if mtype == "agent.turn.event":
            workgroup_id = str(payload.get("workgroup_id") or "").strip()
            member_id = str(payload.get("member_id") or "").strip()
            assign_id = str(payload.get("assign_id") or "").strip()
            if not workgroup_id or not member_id or not assign_id:
                return
            assign = self.store.get_assign(assign_id)
            if assign is None or assign.workgroup_id != workgroup_id or assign.member_id != member_id:
                return
            if str(payload.get("child_turn_id") or assign.child_turn_id) != assign.child_turn_id:
                return
            if str(payload.get("attempt_id") or assign.attempt_id) != assign.attempt_id:
                return
            try:
                if not self.assignments.advance_event_cursor(
                    assign_id,
                    int(payload.get("event_seq") or 0),
                    str(payload.get("stream_epoch") or ""),
                ):
                    return
            except (TypeError, ValueError, WorkgroupError):
                return
            event_type = str(payload.get("event_type") or "").strip()
            data = dict(payload.get("data") or {})
            data.update({"mode": "member", "member_id": member_id, "assign_id": payload.get("assign_id")})
            if event_type == "hitl_required":
                node_hitl_id = str(data.get("hitl_id") or "").strip()
                if not assign_id or not node_hitl_id:
                    return
                self.store.set_assign_status(assign_id, "awaiting_hitl")
                existing = next(
                    (
                        item
                        for item in self.store.list_hitl(workgroup_id)
                        if str((item.metadata or {}).get("source") or "") == "agent_ref"
                        and str((item.metadata or {}).get("assign_id") or "") == assign_id
                        and str((item.metadata or {}).get("node_hitl_id") or "") == node_hitl_id
                    ),
                    None,
                )
                hitl = existing or self.store.create_hitl(
                    workgroup_id,
                    prompt=str(data.get("message") or "成员请求确认工具执行"),
                    kind=str(data.get("hitl_kind") or "tool_approval"),
                    metadata={
                        "source": "agent_ref",
                        "node_hitl_id": node_hitl_id,
                        "member_id": member_id,
                        "agent_id": str(payload.get("agent_id") or ""),
                        "session_id": str(payload.get("session_id") or ""),
                        "assign_id": assign_id,
                        "home_node_id": node_id,
                        "items": list(data.get("items") or []),
                        "child_turn_id": assign.child_turn_id,
                        "attempt_id": assign.attempt_id,
                    },
                )
                if hitl.status == "pending" and self.hub is not None:
                    self.hub.publish_realtime_event(
                        workgroup_id,
                        "hitl_required",
                        {
                            "mode": "member",
                            "member_id": member_id,
                            "assign_id": assign_id,
                            "hitl_id": hitl.hitl_id,
                            "kind": hitl.kind,
                            "prompt": hitl.prompt,
                            "items": list((hitl.metadata or {}).get("items") or []),
                        },
                    )
            elif event_type == "assistant":
                content = str(data.get("content") or "")
                if content:
                    try:
                        self.store.append_timeline(
                            workgroup_id,
                            type="assistant_content",
                            actor_id=member_id,
                            text=content,
                            protocol_name=protocol_name_for_actor(member_id),
                            assign_id=assign_id,
                        )
                    except Exception:  # noqa: BLE001 - realtime output must continue
                        pass
                if self.hub is not None:
                    self.hub.publish_realtime_event(
                        workgroup_id,
                        "delta",
                        {**data, "text": content},
                    )
            elif event_type in {"reasoning", "turn_state"}:
                if self.hub is not None:
                    self.hub.publish_realtime_event(workgroup_id, "status", data)
            elif event_type == "tool_call":
                tool_calls = [] if bool(data.get("partial")) else list(data.get("tool_calls") or [])
                for call in tool_calls:
                    if not isinstance(call, dict):
                        continue
                    function = dict(call.get("function") or {})
                    tool_call_id = str(call.get("id") or data.get("tool_call_id") or "").strip()
                    tool_name = str(
                        data.get("tool_name") or function.get("name") or call.get("name") or ""
                    ).strip()
                    if not tool_call_id and not tool_name:
                        continue
                    try:
                        self.store.append_timeline(
                            workgroup_id,
                            type="tool_started",
                            actor_id=member_id,
                            assign_id=str(payload.get("assign_id") or "") or None,
                            tool_call_id=tool_call_id or None,
                            tool_name=tool_name or None,
                            status="running",
                        )
                    except Exception:  # noqa: BLE001
                        pass
                if self.hub is not None:
                    self.hub.publish_realtime_event(
                        workgroup_id,
                        "status",
                        {**data, "phase": "tool", "purpose": str(data.get("tool_name") or "执行工具")},
                    )
            elif event_type == "tool_result":
                assign_id = str(payload.get("assign_id") or "").strip()
                tool_call_id = str(data.get("tool_call_id") or "").strip()
                tool_name = str(data.get("tool_name") or "").strip()
                status = str(data.get("status") or data.get("result_status") or "succeeded").strip()
                try:
                    self.store.append_timeline(
                        workgroup_id,
                        type="tool_finished",
                        actor_id=member_id,
                        assign_id=assign_id or None,
                        tool_call_id=tool_call_id or None,
                        tool_name=tool_name or None,
                        status=status or "succeeded",
                    )
                except Exception:  # noqa: BLE001
                    pass
                if self.hub is not None:
                    self.hub.publish_realtime_event(
                        workgroup_id,
                        "status",
                        {**data, "phase": "tool_result", "purpose": tool_name or "工具已完成"},
                    )
            return
        if mtype == "agent.turn.result":
            assign_id = str(payload.get("assign_id") or "").strip()
            workgroup_id = str(payload.get("workgroup_id") or "").strip()
            member_id = str(payload.get("member_id") or "").strip()
            if not assign_id or not workgroup_id or not member_id:
                return
            result = dict(payload)
            assign = self.store.get_assign(assign_id)
            if assign is None or assign.workgroup_id != workgroup_id or assign.member_id != member_id:
                return
            if str(payload.get("child_turn_id") or assign.child_turn_id) != assign.child_turn_id:
                return
            if str(payload.get("attempt_id") or assign.attempt_id) != assign.attempt_id:
                return
            if assign is not None and assign.status in {
                "failed",
                "canceled",
                "succeeded",
                "indeterminate",
            }:
                # A cancellation or a completed retry may race with a late
                # Node frame. It must not revive the durable assign or emit a
                # misleading final answer after cancellation.
                return
            final_text = str(payload.get("final_text") or "").strip()
            timeline_event = None
            if str(payload.get("status") or "").strip().lower() in {"succeeded", "awaiting"} and final_text:
                # AgentRef runs do not execute the local Member LLM loop, so
                # their final text arrives through this inbound frame. Persist
                # it before broadcasting the ephemeral final frame; otherwise
                # the originating Node clears its live bubble and a reload has
                # no durable member reply to render.
                timeline_event = next(
                    (
                        event
                        for event in reversed(self.store.list_timeline(workgroup_id))
                        if event.assign_id == assign_id
                        and event.type == "actor_final_text"
                        and event.actor_id == member_id
                    ),
                    None,
                )
                if timeline_event is None:
                    timeline_event = self.store.append_timeline(
                        workgroup_id,
                        type="actor_final_text",
                        actor_id=member_id,
                        text=final_text,
                        protocol_name=protocol_name_for_actor(member_id),
                        assign_id=assign_id,
                    )
            result_status = str(payload.get("status") or "failed").strip().lower()
            final_status = {
                "succeeded": "succeeded",
                "canceled": "canceled",
                "cancelled": "canceled",
            }.get(result_status, "failed")
            self.assignments.finish(
                workgroup_id,
                assign_id,
                status=final_status,
                summary=final_text or str(payload.get("message") or "").strip() or None,
                error_code=None if final_status == "succeeded" else str(payload.get("error_code") or final_status),
                actor_id=member_id,
                text=(
                    "已完成"
                    if final_status == "succeeded"
                    else "已中断"
                    if final_status == "canceled"
                    else f"失败：{payload.get('message') or '成员任务失败'}"
                ),
            )
            if self.hub is not None:
                self.hub.publish_realtime_event(
                    workgroup_id,
                    "assistant_final",
                    {
                        "mode": "member",
                        "member_id": member_id,
                        "assign_id": assign_id,
                        "text": final_text,
                        "status": str(payload.get("status") or "failed"),
                        "timeline_event": (
                            timeline_event.model_dump(mode="json")
                            if timeline_event is not None
                            else None
                        ),
                    },
                )
            self._signal_agent_result(assign_id, result)
            return
    def make_assign_completer(self, kernel: TurnKernel, *, timeout_s: float | None = None):
        """Create a synchronous completer for an existing Node Agent session."""
        def completer(
            workgroup_id: str,
            assign_id: str,
            member_id: str,
            instruction: str,
            tool_call_id: str = "",
        ) -> str:
            _ = tool_call_id
            kernel._raise_if_cancelled(workgroup_id)
            assign = self.store.get_assign(assign_id)
            if assign is None or assign.workgroup_id != workgroup_id:
                raise WorkgroupError("not_found", "assign not found", http_status=404)
            if assign.status in {"failed", "canceled", "succeeded", "indeterminate"}:
                raise WorkgroupError(
                    assign.error_code or "canceled",
                    assign.result_summary or "assign already finished",
                    http_status=409,
                )
            member = self.store.get_member(member_id)
            if member is None or member.workgroup_id != workgroup_id:
                raise WorkgroupError("not_found", "member not found", http_status=404)
            if member.status != "ready" and not (
                member.status == "busy" and member.active_assign_id == assign_id
            ):
                raise WorkgroupError("conflict", "member not ready", http_status=409)
            return self.run_agent_ref_assign(
                workgroup_id,
                assign_id,
                member_id,
                instruction,
                cancel_check=lambda: kernel._is_cancelled(workgroup_id)
                or kernel._is_assign_cancelled(assign_id),
                timeout_s=timeout_s,
            )

        return completer
    def create_info_hitl(self, workgroup_id: str, req: HITLCreateRequest) -> HITLRequest:
        return self.store.create_hitl(workgroup_id, prompt=req.prompt)

    def resolve_info_hitl(self, workgroup_id: str, hitl_id: str, req: HITLResolveRequest) -> HITLRequest:
        hitl = self.store.get_hitl(hitl_id)
        if hitl is None or hitl.workgroup_id != workgroup_id:
            raise WorkgroupError("not_found", "hitl not found", http_status=404)
        if (hitl.metadata or {}).get("source") == "agent_ref":
            if not str((req.resolution or {}).get("type") or "").strip():
                raise WorkgroupError(
                    "invalid_request",
                    "agent HITL resolution type is required",
                    http_status=400,
                )
        had_waiter = self.store.has_hitl_waiter(hitl_id)
        hitl = self.store.resolve_hitl_cas(workgroup_id, hitl_id, resolution=req.resolution)
        if not had_waiter and self._turn_kernel is not None:
            self._turn_kernel.resume_resolved_hitl(hitl)
        if (hitl.metadata or {}).get("source") == "agent_ref":
            self.enqueue_agent_turn_resume(workgroup_id, hitl.hitl_id, dict(req.resolution or {}))
        return hitl
