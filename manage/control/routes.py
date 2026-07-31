"""Placement 控制面路由（D5 Cut7：peers/create/delete 一律 410）。"""

from __future__ import annotations

from fastapi import APIRouter, HTTPException, Request

from manage.control.models import ControlCreateAgentRequest
from manage.platform.audit import AuditLog
from manage.platform.auth import authenticate
from manage.registry.store import AgentRegistryStore

_PLACEMENT_GONE = {
    "code": "placement_deprecated",
    "message": "远程 Placement 已下线：跨机器协作请使用工作组",
}


def build_control_router(registry: AgentRegistryStore, audit: AuditLog) -> APIRouter:
    _ = registry, audit
    router = APIRouter(tags=["control"])

    @router.get("/v1/control/peers")
    def list_peers(request: Request) -> None:
        authenticate(request)
        raise HTTPException(status_code=410, detail=_PLACEMENT_GONE)

    @router.post("/v1/control/nodes/{home_node_id}/agents")
    def create_on_home(
        home_node_id: str, payload: ControlCreateAgentRequest, request: Request
    ) -> None:
        authenticate(request)
        _ = home_node_id, payload
        raise HTTPException(status_code=410, detail=_PLACEMENT_GONE)

    @router.delete("/v1/control/nodes/{home_node_id}/agents/{agent_id}")
    def delete_on_home(home_node_id: str, agent_id: str, request: Request) -> None:
        authenticate(request)
        _ = home_node_id, agent_id
        raise HTTPException(status_code=410, detail=_PLACEMENT_GONE)

    return router
