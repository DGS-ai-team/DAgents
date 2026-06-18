"""Manage Admin API：只读观测（A2A Task 列表）。"""

from __future__ import annotations

from fastapi import APIRouter, Query, Request

from manage.a2a.models import AdminTaskListResponse, TaskStatus
from manage.a2a.store import A2ATaskStore
from manage.platform.auth import authenticate, require_admin
from manage.registry.store import AgentRegistryStore


def build_admin_router(
    registry: AgentRegistryStore,
    a2a_store: A2ATaskStore,
) -> APIRouter:
    del registry  # 保留参数以兼容 create_app 装配签名
    router = APIRouter(prefix="/v1/admin", tags=["admin"])

    @router.get("/a2a/tasks", response_model=AdminTaskListResponse)
    def list_a2a_tasks(
        request: Request,
        to_agent_id: str | None = Query(default=None),
        from_agent_id: str | None = Query(default=None),
        status: TaskStatus | None = Query(default=None),
        limit: int = Query(default=50, ge=1, le=200),
        offset: int = Query(default=0, ge=0),
    ) -> AdminTaskListResponse:
        auth = authenticate(request)
        require_admin(auth)
        tasks, total = a2a_store.list_tasks(
            to_agent_id=to_agent_id,
            from_agent_id=from_agent_id,
            status=status,
            limit=limit,
            offset=offset,
        )
        return AdminTaskListResponse(tasks=tasks, total=total, limit=limit, offset=offset)

    return router
