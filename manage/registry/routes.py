"""Registry HTTP 路由。"""

from __future__ import annotations

import time
from typing import Literal

from fastapi import APIRouter, HTTPException, Query, Request

from manage.platform.audit import AuditLog
from manage.platform.auth import (
    AuthContext,
    audit_actor,
    authenticate,
    ensure_node_identity,
    extract_agent_id,
    require_admin,
)
from manage.platform.metrics import record_registry_operation
from manage.registry.models import (
    AgentDeregisterRequest,
    AgentDiscoverResponse,
    AgentGroupsUpdateRequest,
    AgentHeartbeatRequest,
    AgentListResponse,
    AgentRecord,
    AgentRegisterRequest,
    AgentRegisterResponse,
)
from manage.registry.status import AgentStatus
from manage.registry.store import AgentListQuery, AgentRegistryStore

HEARTBEAT_INTERVAL_SECONDS = 30


def _ensure_member_groups(auth: AuthContext, groups: list[str]) -> None:
    if auth.is_admin:
        return
    for group in groups:
        if not auth.allows_discovery_group(group):
            raise HTTPException(status_code=403, detail=f"discovery_group={group!r} 不在 token 可见范围")


def _ensure_node_agent_request(request: Request, agent_id: str, auth: AuthContext) -> None:
    ensure_node_identity(request, agent_id, auth)


def _resolve_discover_caller_groups(
    store: AgentRegistryStore,
    request: Request,
    auth: AuthContext,
    caller_groups: list[str],
) -> list[str] | None:
    """解析 discover 的 caller 可见分组。

    未传 discovery_group 查询参数时，按调用方 Manage 已分配的
    discovery_group 与对端求交集，限制 discover 可见范围。
    """
    if caller_groups:
        return caller_groups
    header_id = extract_agent_id(request)
    if header_id:
        record = store.get(header_id)
        if record is None:
            return []
        return list(record.discovery_group)
    if auth.is_admin:
        return None
    return auth.discovery_groups or None


def _resolve_list_query(
    auth: AuthContext,
    *,
    discovery_group: str | None,
    team: str | None,
    status: AgentStatus | Literal["all"],
    q: str | None,
    page: int,
    page_size: int,
) -> AgentListQuery:
    if auth.requires_group_on_list() and not discovery_group:
        raise HTTPException(status_code=422, detail="discovery_group 为必填")
    if discovery_group and not auth.allows_discovery_group(discovery_group):
        raise HTTPException(status_code=403, detail=f"discovery_group={discovery_group!r} 不在 token 可见范围")
    return AgentListQuery(
        discovery_group=discovery_group,
        team=team,
        status=status,
        q=q,
        page=page,
        page_size=page_size,
    )


def build_registry_router(store: AgentRegistryStore, audit: AuditLog) -> APIRouter:
    router = APIRouter(tags=["registry"])

    @router.post("/v1/registry/agents", response_model=AgentRegisterResponse)
    def register_agent(payload: AgentRegisterRequest, request: Request) -> AgentRegisterResponse:
        auth = authenticate(request)
        _ensure_node_agent_request(request, payload.agent_id, auth)
        record = store.register(payload)
        audit.record(
            actor=audit_actor(request, auth, fallback_agent_id=payload.agent_id),
            action="registry.register",
            target_agent_id=record.agent_id,
            discovery_group=record.discovery_group[0] if record.discovery_group else None,
            detail={"status": record.status, "team": record.team},
        )
        record_registry_operation(operation="register", status="ok")
        now = int(time.time())
        return AgentRegisterResponse(
            agent=record,
            heartbeat_interval_seconds=HEARTBEAT_INTERVAL_SECONDS,
            server_time_unix=now,
        )

    @router.post("/v1/registry/agents/{agent_id}/heartbeat", response_model=AgentRecord)
    def heartbeat_agent(agent_id: str, payload: AgentHeartbeatRequest, request: Request) -> AgentRecord:
        auth = authenticate(request)
        _ensure_node_agent_request(request, agent_id, auth)
        record = store.heartbeat(agent_id, payload)
        if record is None:
            record_registry_operation(operation="heartbeat", status="not_found")
            raise HTTPException(status_code=404, detail=f"agent_id={agent_id!r} 不存在")
        audit.record(actor=audit_actor(request, auth, fallback_agent_id=agent_id), action="registry.heartbeat", target_agent_id=agent_id)
        record_registry_operation(operation="heartbeat", status="ok")
        return record

    @router.post("/v1/registry/agents/{agent_id}/deregister")
    def deregister_agent(agent_id: str, payload: AgentDeregisterRequest, request: Request) -> dict[str, bool]:
        auth = authenticate(request)
        _ensure_node_agent_request(request, agent_id, auth)
        existing = store.get(agent_id)
        if existing is None:
            raise HTTPException(status_code=404, detail=f"agent_id={agent_id!r} 不存在")
        if not auth.is_admin:
            _ensure_member_groups(auth, existing.discovery_group)
        ok = store.delete(agent_id)
        if not ok:
            raise HTTPException(status_code=404, detail=f"agent_id={agent_id!r} 不存在")
        audit.record(
            actor=audit_actor(request, auth, fallback_agent_id=agent_id),
            action="registry.deregister",
            target_agent_id=agent_id,
            detail={"reason": payload.reason},
        )
        record_registry_operation(operation="deregister", status="ok")
        return {"deleted": True}

    @router.patch("/v1/registry/agents/{agent_id}/groups", response_model=AgentRecord)
    def update_agent_groups(agent_id: str, payload: AgentGroupsUpdateRequest, request: Request) -> AgentRecord:
        auth = authenticate(request)
        if not auth.is_admin:
            raise HTTPException(status_code=403, detail="admin role required")
        record = store.update_groups(agent_id, payload.discovery_group)
        if record is None:
            raise HTTPException(status_code=404, detail=f"agent_id={agent_id!r} 不存在")
        audit.record(
            actor=audit_actor(request, auth),
            action="registry.update_groups",
            target_agent_id=agent_id,
            discovery_group=payload.discovery_group[0] if payload.discovery_group else None,
            detail={"discovery_group": payload.discovery_group},
        )
        record_registry_operation(operation="update_groups", status="ok")
        return record

    @router.get("/v1/registry/discovery-groups")
    def list_discovery_groups(request: Request) -> list[dict]:
        auth = authenticate(request)
        require_admin(auth)
        return store.list_discovery_groups()

    @router.post("/v1/registry/discovery-groups")
    def create_discovery_group(request: Request, body: dict) -> dict:
        auth = authenticate(request)
        require_admin(auth)
        name = str((body or {}).get("name") or "").strip()
        if not name:
            raise HTTPException(status_code=422, detail="name required")
        try:
            group = store.create_discovery_group(name)
        except ValueError as exc:
            raise HTTPException(status_code=422, detail=str(exc)) from exc
        audit.record(actor=audit_actor(request, auth), action="registry.discovery_group.create", detail={"name": name})
        return group

    @router.delete("/v1/registry/discovery-groups/{name}")
    def delete_discovery_group(name: str, request: Request, detach_nodes: bool = True) -> dict:
        auth = authenticate(request)
        require_admin(auth)
        ok = store.delete_discovery_group(name, detach_nodes=detach_nodes)
        if not ok:
            raise HTTPException(status_code=404, detail=f"group={name!r} 不存在")
        audit.record(
            actor=audit_actor(request, auth),
            action="registry.discovery_group.delete",
            detail={"name": name, "detach_nodes": detach_nodes},
        )
        return {"deleted": True, "name": name}

    @router.get("/v1/registry/agents", response_model=AgentListResponse)
    def list_agents(
        request: Request,
        discovery_group: str | None = Query(default=None),
        team: str | None = Query(default=None),
        status: AgentStatus | Literal["all"] = Query(default="all"),
        q: str | None = Query(default=None),
        page: int = Query(default=1, ge=1),
        page_size: int = Query(default=50, ge=1, le=200),
    ) -> AgentListResponse:
        auth = authenticate(request)
        query = _resolve_list_query(
            auth,
            discovery_group=discovery_group,
            team=team,
            status=status,
            q=q,
            page=page,
            page_size=page_size,
        )
        agents, total = store.list(query)
        return AgentListResponse(agents=agents, page=query.page, page_size=query.page_size, total=total)

    @router.get("/v1/registry/agents/discover", response_model=AgentDiscoverResponse)
    def discover_agents(
        request: Request,
        discovery_group: str | None = Query(default=None),
        caller_groups: list[str] = Query(default=[]),
    ) -> AgentDiscoverResponse:
        auth = authenticate(request)
        groups = _resolve_discover_caller_groups(store, request, auth, caller_groups)
        if discovery_group and not auth.allows_discovery_group(discovery_group):
            raise HTTPException(status_code=403, detail=f"discovery_group={discovery_group!r} 不在 token 可见范围")
        if discovery_group is None and groups == []:
            agents = []
        else:
            agents = store.discover(discovery_group=discovery_group, caller_groups=groups)
        return AgentDiscoverResponse(agents=agents)

    @router.get("/v1/registry/agents/{agent_id}", response_model=AgentRecord)
    def get_agent(
        agent_id: str,
        request: Request,
        discovery_group: str = Query(...),
    ) -> AgentRecord:
        auth = authenticate(request)
        if not auth.allows_discovery_group(discovery_group):
            raise HTTPException(status_code=403, detail=f"discovery_group={discovery_group!r} 不在 token 可见范围")
        record = store.get(agent_id)
        if record is None or discovery_group not in record.discovery_group:
            raise HTTPException(status_code=404, detail=f"agent_id={agent_id!r} 不存在")
        return record

    @router.get("/v1/registry/nodes/{node_id}", response_model=AgentRecord)
    def get_node(node_id: str, request: Request) -> AgentRecord:
        """P5：按 node_id 读取 Registry 行（当前与 agent_id 同键）。"""
        authenticate(request)
        nid = (node_id or "").strip()
        record = store.get(nid)
        if record is None:
            raise HTTPException(status_code=404, detail=f"node_id={nid!r} 不存在")
        return record

    @router.delete("/v1/registry/agents/{agent_id}")
    def delete_agent(agent_id: str, request: Request) -> dict[str, bool]:
        auth = authenticate(request)
        existing = store.get(agent_id)
        if existing is None:
            raise HTTPException(status_code=404, detail=f"agent_id={agent_id!r} 不存在")
        if not auth.is_admin:
            _ensure_member_groups(auth, existing.discovery_group)
        ok = store.delete(agent_id)
        if not ok:
            raise HTTPException(status_code=404, detail=f"agent_id={agent_id!r} 不存在")
        audit.record(actor=auth.token_id, action="registry.delete", target_agent_id=agent_id)
        record_registry_operation(operation="delete", status="ok")
        return {"deleted": True}

    return router
