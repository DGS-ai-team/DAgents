"""Placement 控制面路由（D5：peers/create 已 410；DELETE 仍供遗留 stub 清理）。"""

from __future__ import annotations

from fastapi import APIRouter, HTTPException, Request

from manage.control.models import ControlCreateAgentRequest, ControlDeleteAgentResponse
from manage.control.node_client import call_home_delete_agent
from manage.platform.audit import AuditLog
from manage.platform.auth import audit_actor, authenticate, ensure_node_identity
from manage.registry.store import AgentRegistryStore


def build_control_router(registry: AgentRegistryStore, audit: AuditLog) -> APIRouter:
    router = APIRouter(tags=["control"])

    @router.get("/v1/control/peers")
    def list_peers(request: Request) -> None:
        authenticate(request)
        raise HTTPException(
            status_code=410,
            detail={
                "code": "placement_deprecated",
                "message": "远程 Placement peers 已下线：跨机器协作请使用工作组",
            },
        )

    @router.post("/v1/control/nodes/{home_node_id}/agents")
    def create_on_home(
        home_node_id: str, payload: ControlCreateAgentRequest, request: Request
    ) -> None:
        authenticate(request)
        _ = home_node_id, payload
        raise HTTPException(
            status_code=410,
            detail={
                "code": "placement_deprecated",
                "message": "远程 Placement 创建已下线：跨机器协作请使用工作组",
            },
        )

    @router.delete("/v1/control/nodes/{home_node_id}/agents/{agent_id}", response_model=ControlDeleteAgentResponse)
    def delete_on_home(home_node_id: str, agent_id: str, request: Request) -> ControlDeleteAgentResponse:
        auth = authenticate(request)
        owner = (request.headers.get("x-dagents-agent-id") or "").strip()
        if not owner:
            raise HTTPException(status_code=400, detail="x-dagents-agent-id required")
        ensure_node_identity(request, owner, auth)
        home_id = home_node_id.strip()
        aid = agent_id.strip()
        if not home_id or not aid:
            raise HTTPException(status_code=400, detail="home_node_id and agent_id required")

        ok, reason = registry.can_a2a_invoke(owner, home_id)
        if not ok and reason not in {"target_not_found"}:
            if reason != "target_not_found":
                raise HTTPException(status_code=403, detail=reason or "placement_forbidden")

        home = registry.get(home_id)
        home_deleted = False
        if home is not None and home.base_url:
            result = call_home_delete_agent(
                base_url=home.base_url,
                home_node_id=home_id,
                agent_id=aid,
                owner_node_id=owner,
            )
            home_deleted = bool(result.get("home_deleted", True))
        else:
            home_deleted = False

        audit.record(
            actor=audit_actor(request, auth, fallback_agent_id=owner),
            action="control.agent.delete",
            target_agent_id=aid,
            detail={"owner_node_id": owner, "home_node_id": home_id, "home_deleted": home_deleted},
        )
        return ControlDeleteAgentResponse(
            ok=True,
            agent_id=aid,
            home_node_id=home_id,
            home_deleted=home_deleted,
        )

    return router
