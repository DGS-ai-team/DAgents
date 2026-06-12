"""Manage Admin API：只读观测（A2A Task 列表、Node session 代理）。"""

from __future__ import annotations

import httpx
from fastapi import APIRouter, HTTPException, Query, Request

from manage.a2a.models import AdminTaskListResponse, TaskStatus
from manage.a2a.store import A2ATaskStore
from manage.platform.auth import authenticate, require_admin
from manage.registry.store import AgentRegistryStore

NODE_PROXY_TIMEOUT_SECONDS = 8.0


def _proxy_node_json(base_url: str, path: str, *, params: dict[str, str | int] | None = None) -> object:
    url = base_url.rstrip("/") + path
    try:
        with httpx.Client(timeout=NODE_PROXY_TIMEOUT_SECONDS) as client:
            resp = client.get(url, params=params or {})
    except httpx.RequestError as exc:
        raise HTTPException(status_code=502, detail=f"node_unreachable: {exc}") from exc
    if resp.status_code == 404:
        raise HTTPException(status_code=404, detail="not_found")
    if resp.status_code >= 400:
        detail = resp.text.strip() or f"node_error_{resp.status_code}"
        raise HTTPException(status_code=502, detail=detail[:500])
    try:
        return resp.json()
    except ValueError as exc:
        raise HTTPException(status_code=502, detail="node_invalid_json") from exc


def _require_agent(registry: AgentRegistryStore, agent_id: str):
    cleaned = agent_id.strip()
    if not cleaned:
        raise HTTPException(status_code=400, detail="agent_id is required")
    record = registry.get(cleaned)
    if record is None:
        raise HTTPException(status_code=404, detail="agent_not_found")
    return record


def build_admin_router(
    registry: AgentRegistryStore,
    a2a_store: A2ATaskStore,
) -> APIRouter:
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

    @router.get("/nodes/{agent_id}/sessions")
    def proxy_node_sessions(request: Request, agent_id: str) -> object:
        auth = authenticate(request)
        require_admin(auth)
        record = _require_agent(registry, agent_id)
        return _proxy_node_json(record.base_url, "/v1/sessions")

    @router.get("/nodes/{agent_id}/sessions/{session_id}/context")
    def proxy_node_session_context(request: Request, agent_id: str, session_id: str) -> object:
        auth = authenticate(request)
        require_admin(auth)
        record = _require_agent(registry, agent_id)
        path = f"/v1/sessions/{session_id.strip()}/context"
        return _proxy_node_json(record.base_url, path)

    return router
