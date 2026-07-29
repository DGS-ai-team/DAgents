"""Edge Tunnel 路由：签发会话 + 反代到 home。"""

from __future__ import annotations

from datetime import datetime, timezone

from fastapi import APIRouter, HTTPException, Request, Response

from manage.edge.models import EdgeSessionCreateRequest, EdgeSessionResponse
from manage.edge.proxy import forward_to_home, path_allowed
from manage.edge.store import EdgeSessionStore
from manage.platform.audit import AuditLog
from manage.platform.auth import audit_actor, authenticate, ensure_node_identity
from manage.registry.store import AgentRegistryStore


def _rfc3339(ts: int) -> str:
    return datetime.fromtimestamp(ts, tz=timezone.utc).isoformat().replace("+00:00", "Z")


def build_edge_router(
    registry: AgentRegistryStore,
    sessions: EdgeSessionStore,
    audit: AuditLog,
) -> APIRouter:
    router = APIRouter(tags=["edge"])

    @router.post("/v1/edge/sessions", response_model=EdgeSessionResponse)
    def create_session(payload: EdgeSessionCreateRequest, request: Request) -> EdgeSessionResponse:
        auth = authenticate(request)
        owner = (request.headers.get("x-dagents-agent-id") or "").strip()
        if not owner:
            raise HTTPException(status_code=400, detail="x-dagents-agent-id required")
        ensure_node_identity(request, owner, auth)

        home_id = payload.home_node_id.strip()
        agent_id = payload.agent_id.strip()
        if home_id == owner:
            raise HTTPException(status_code=400, detail="home_node_id must differ from owner")

        ok, reason = registry.can_a2a_invoke(owner, home_id)
        if not ok:
            status = 404 if reason in {"caller_not_found", "target_not_found"} else 403
            raise HTTPException(status_code=status, detail=reason or "edge_forbidden")

        home = registry.get(home_id)
        if home is None:
            raise HTTPException(status_code=404, detail="target_not_found")
        if home.status != "online":
            raise HTTPException(status_code=409, detail="target_offline")
        if not home.base_url:
            raise HTTPException(status_code=502, detail="home_base_url_missing")

        sess = sessions.create(
            owner_node_id=owner,
            home_node_id=home_id,
            agent_id=agent_id,
            scopes=payload.scopes,
            ttl_seconds=payload.ttl_seconds,
        )
        audit.record(
            actor=audit_actor(request, auth, fallback_agent_id=owner),
            action="edge.session.create",
            target_agent_id=agent_id,
            detail={
                "edge_session_id": sess.session_id,
                "owner_node_id": owner,
                "home_node_id": home_id,
                "scopes": sess.scopes,
            },
        )
        return EdgeSessionResponse(
            edge_session_id=sess.session_id,
            home_node_id=home_id,
            agent_id=agent_id,
            owner_node_id=owner,
            scopes=sess.scopes,
            expires_at=_rfc3339(sess.expires_at_unix),
            proxy_prefix=f"/v1/edge/{sess.session_id}/proxy",
        )

    @router.api_route(
        "/v1/edge/{session_id}/proxy/{path:path}",
        methods=["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"],
    )
    async def proxy_path(session_id: str, path: str, request: Request) -> Response:
        return await _proxy(session_id, path, request)

    @router.api_route(
        "/v1/edge/{session_id}/proxy",
        methods=["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"],
    )
    async def proxy_root(session_id: str, request: Request) -> Response:
        return await _proxy(session_id, "", request)

    async def _proxy(session_id: str, path: str, request: Request) -> Response:
        auth = authenticate(request)
        caller = (request.headers.get("x-dagents-agent-id") or "").strip()
        if not caller:
            raise HTTPException(status_code=400, detail="x-dagents-agent-id required")
        ensure_node_identity(request, caller, auth)

        sess = sessions.get(session_id)
        if sess is None:
            raise HTTPException(status_code=404, detail="edge_session_not_found")
        if sess.owner_node_id != caller:
            raise HTTPException(status_code=403, detail="edge_session_owner_mismatch")

        target = "/" + (path or "").lstrip("/")
        if not path_allowed(target, agent_id=sess.agent_id, scopes=sess.scopes):
            raise HTTPException(status_code=403, detail="edge_scope_denied")

        # messages / streams：强制 agent_id 与会话绑定，防止串会话
        if target == "/v1/messages" or target.startswith("/v1/messages"):
            # body 校验留给 home；此处仅路径 scope
            pass
        if target == "/v1/streams" or target.startswith("/v1/streams"):
            q_agent = (request.query_params.get("agent_id") or "").strip()
            if q_agent and q_agent != sess.agent_id:
                raise HTTPException(status_code=403, detail="edge_agent_mismatch")

        home = registry.get(sess.home_node_id)
        if home is None or not home.base_url:
            raise HTTPException(status_code=502, detail="home_unreachable")
        if home.status != "online":
            raise HTTPException(status_code=409, detail="target_offline")

        return await forward_to_home(
            request=request,
            base_url=home.base_url,
            target_path=target,
            home_node_id=sess.home_node_id,
            owner_node_id=sess.owner_node_id,
            edge_session_id=sess.session_id,
        )

    return router
