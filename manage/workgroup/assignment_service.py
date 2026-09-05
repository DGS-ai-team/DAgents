"""Canonical Workgroup Assign lifecycle.

An Assign is the one durable delegation record shared by direct member
mentions and Supervisor tool calls.  This module intentionally contains no
execution loop; Node owns the child Agent turn and this service only owns the
Manage-side state transition and public Timeline projection.
"""

from __future__ import annotations

from manage.workgroup.models import Assign, AssignCreateRequest
from manage.workgroup.protocol_names import protocol_name_for_actor
from manage.workgroup.store import WorkGroupStore


_TERMINAL = {"succeeded", "failed", "canceled", "indeterminate"}


class AssignmentService:
    def __init__(self, store: WorkGroupStore) -> None:
        self.store = store

    def create(
        self,
        workgroup_id: str,
        req: AssignCreateRequest,
        *,
        actor_id: str,
        started_text: str,
        direct_member_id: str | None = None,
    ) -> Assign:
        assign = self.store.create_assign(workgroup_id, req)
        assign = self.store.set_assign_status(assign.assign_id, "running")
        self.store.append_timeline(
            workgroup_id,
            type="assign_started",
            actor_id=actor_id,
            text=started_text,
            protocol_name=protocol_name_for_actor(actor_id),
            assign_id=assign.assign_id,
            direct_member_id=direct_member_id,
        )
        return assign

    def finish(
        self,
        workgroup_id: str,
        assign_id: str,
        *,
        status: str,
        summary: str | None = None,
        error_code: str | None = None,
        actor_id: str | None = None,
        text: str | None = None,
        direct_member_id: str | None = None,
    ) -> Assign:
        current = self.store.get_assign(assign_id)
        if current is None or current.workgroup_id != workgroup_id:
            raise ValueError("assign not found")
        if current.status in _TERMINAL:
            return current
        updated = self.store.set_assign_status(
            assign_id,
            status,
            result_summary=summary,
            error_code=error_code,
        )
        if status in _TERMINAL:
            event_exists = any(
                event.assign_id == assign_id and event.type == "assign_finished"
                for event in self.store.list_timeline(workgroup_id)
            )
            if not event_exists:
                actor = actor_id or current.member_id
                self.store.append_timeline(
                    workgroup_id,
                    type="assign_finished",
                    actor_id=actor,
                    text=text or ("已完成" if status == "succeeded" else "已中断" if status == "canceled" else "执行失败"),
                    protocol_name=protocol_name_for_actor(actor),
                    assign_id=assign_id,
                    direct_member_id=direct_member_id,
                )
        return updated

    def advance_event_cursor(self, assign_id: str, event_seq: int, stream_epoch: str = "") -> bool:
        return self.store.advance_assign_event_cursor(assign_id, event_seq, stream_epoch)

    def cancel_active(
        self,
        workgroup_id: str,
        *,
        reason: str = "cancelled by user",
        error_code: str = "canceled",
        assign_ids: set[str] | None = None,
        leader_run_id: str | None = None,
        exclude_assign_ids: set[str] | None = None,
    ) -> list[Assign]:
        """Cancel active assignments through the canonical lifecycle.

        Cancellation is a domain state, not a failed result with a canceled
        error code.  Routing bulk cancellation through ``finish`` also keeps
        the durable state and the single public ``assign_finished`` event in
        sync with normal assignment completion.
        """
        cancelled: list[Assign] = []
        for assign in self.store.list_assigns(workgroup_id, active_only=True):
            if assign_ids is not None and assign.assign_id not in assign_ids:
                continue
            if leader_run_id is not None and assign.leader_run_id != leader_run_id:
                continue
            if exclude_assign_ids and assign.assign_id in exclude_assign_ids:
                continue
            updated = self.finish(
                workgroup_id,
                assign.assign_id,
                status="canceled",
                summary=reason,
                error_code=error_code,
                actor_id=assign.member_id,
                text="已中断",
                direct_member_id=(
                    assign.member_id if assign.source == "direct_member" else None
                ),
            )
            if updated.status == "canceled":
                cancelled.append(updated)
        return cancelled
