"""Turn kernel 骨架：Assign / ActorRun / Projector / HITL CAS 占位。"""

from __future__ import annotations

from typing import Any

from manage.workgroup.errors import WorkgroupError
from manage.workgroup.models import (
    ActorRun,
    ActorRunCreateRequest,
    Assign,
    AssignCreateRequest,
)
from manage.workgroup.projector import project_actor_context
from manage.workgroup.store import WorkGroupStore


class TurnKernel:
    """Manage 侧 turn 编排骨架。

    D1 仅提供可测的状态落库与权限门禁；不含 LLM 循环、WS 下发、真实工具结果。
    """

    def __init__(self, store: WorkGroupStore) -> None:
        self._store = store
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
        return project_actor_context(actor_id=actor_id, run=run, member=member)

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
