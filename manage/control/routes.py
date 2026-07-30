"""Placement 控制面路由：peers 列表、远端创建/删除。"""

from __future__ import annotations

from typing import Any

from fastapi import APIRouter, HTTPException, Request

from manage.control.models import (
    ControlCreateAgentRequest,
    ControlCreateAgentResponse,
    ControlDeleteAgentResponse,
    ControlHostInfo,
    PeerNodesResponse,
    PeerNodeView,
)
from manage.control.node_client import call_home_create_agent, call_home_delete_agent
from manage.platform.audit import AuditLog
from manage.platform.auth import audit_actor, authenticate, ensure_node_identity
from manage.registry.store import AgentListQuery, AgentRegistryStore, record_node_id


def _host_from_metadata(metadata: dict[str, Any] | None) -> ControlHostInfo:
    meta = metadata if isinstance(metadata, dict) else {}
    host_info = meta.get("host_info") if isinstance(meta.get("host_info"), dict) else {}
    display = meta.get("display") if isinstance(meta.get("display"), dict) else {}
    os_kind = str(host_info.get("os_kind") or "").strip().lower()
    sys_platform = str(host_info.get("sys_platform") or "").strip().lower()
    machine = str(host_info.get("machine") or "").strip()
    if "available" in display:
        display_available = bool(display.get("available"))
    else:
        # 启发式：Windows/macOS 默认有 GUI；Linux 看 display.available（无则 false）
        display_available = os_kind in {"windows", "darwin"} or sys_platform in {"windows", "darwin"}
    label = str(display.get("label") or "").strip()
    if not label:
        if os_kind == "windows" or sys_platform == "windows":
            label = "Windows"
        elif os_kind == "darwin" or sys_platform == "darwin":
            label = "macOS"
        elif os_kind or sys_platform:
            label = (os_kind or sys_platform).capitalize()
        else:
            label = "Unknown"
    return ControlHostInfo(
        os_kind=os_kind or sys_platform or "unknown",
        sys_platform=sys_platform or os_kind,
        machine=machine,
        display_available=display_available,
        display_label=label,
    )


def _placement_flags(metadata: dict[str, Any] | None) -> tuple[bool, bool]:
    meta = metadata if isinstance(metadata, dict) else {}
    placement = meta.get("placement") if isinstance(meta.get("placement"), dict) else {}
    allow_create = placement.get("allow_peer_create")
    allow_screen = placement.get("allow_screen_view")
    return (
        True if allow_create is None else bool(allow_create),
        True if allow_screen is None else bool(allow_screen),
    )


def _parse_snapshot(raw: Any) -> dict[str, Any] | str | None:
    if raw is None:
        return None
    if isinstance(raw, dict):
        return raw
    if isinstance(raw, str):
        return raw
    return None


def build_control_router(registry: AgentRegistryStore, audit: AuditLog) -> APIRouter:
    router = APIRouter(tags=["control"])

    @router.get("/v1/control/peers", response_model=PeerNodesResponse)
    def list_peers(request: Request) -> PeerNodesResponse:
        auth = authenticate(request)
        caller = (request.headers.get("x-dagents-agent-id") or "").strip()
        if not caller:
            raise HTTPException(status_code=400, detail="x-dagents-agent-id required")
        ensure_node_identity(request, caller, auth)
        caller_rec = registry.get(caller)
        caller_groups = list(caller_rec.discovery_group) if caller_rec else []
        # 无分组时仍返回空列表（需先在 Console 分配 discovery_group）
        nodes: list[PeerNodeView] = []
        if caller_groups:
            # Placement peers ≠ A2A discover：ops 节点默认 expose_to_peers=false，
            # 但仍可作远端创建的 home；这里只按同组 + online，用 placement.allow_* 标注能力。
            online, _ = registry.list(
                AgentListQuery(discovery_group=None, status="online", page=1, page_size=10_000)
            )
            for full in online:
                nid = record_node_id(full) or full.agent_id
                if full.agent_id == caller or nid == caller:
                    continue
                if not any(group in full.discovery_group for group in caller_groups):
                    continue
                allow_create, allow_screen = _placement_flags(full.metadata)
                nodes.append(
                    PeerNodeView(
                        node_id=nid,
                        name=full.name or full.agent_id,
                        status=full.status,
                        discovery_group=list(full.discovery_group),
                        version=full.version or "",
                        host=_host_from_metadata(full.metadata),
                        allow_peer_create=allow_create,
                        allow_screen_view=allow_screen,
                    )
                )
        audit.record(
            actor=audit_actor(request, auth, fallback_agent_id=caller),
            action="control.peers.list",
            target_agent_id=caller,
            detail={"count": len(nodes)},
        )
        return PeerNodesResponse(nodes=nodes, self_node_id=caller)

    @router.post("/v1/control/nodes/{home_node_id}/agents", response_model=ControlCreateAgentResponse)
    def create_on_home(home_node_id: str, payload: ControlCreateAgentRequest, request: Request) -> ControlCreateAgentResponse:
        auth = authenticate(request)
        ensure_node_identity(request, payload.owner_node_id, auth)
        home_id = home_node_id.strip()
        if not home_id:
            raise HTTPException(status_code=400, detail="home_node_id required")
        if home_id == payload.owner_node_id:
            raise HTTPException(status_code=400, detail="home_node_id must differ from owner_node_id")

        ok, reason = registry.can_a2a_invoke(payload.owner_node_id, home_id)
        if not ok:
            status = 404 if reason in {"caller_not_found", "target_not_found"} else 403
            raise HTTPException(status_code=status, detail=reason or "placement_forbidden")

        home = registry.get(home_id)
        if home is None:
            raise HTTPException(status_code=404, detail="target_not_found")
        if home.status != "online":
            raise HTTPException(status_code=409, detail="target_offline")
        # Placement 创建不要求 A2A expose_to_peers（ops 节点默认为 false）
        allow_create, _ = _placement_flags(home.metadata)
        if not allow_create:
            raise HTTPException(status_code=403, detail="peer_create_disabled")

        home_body = {
            "display_name": payload.display_name,
            "template_id": payload.template_id,
            "defaults": payload.defaults,
            "sandbox": payload.sandbox,
            "placement": {
                "role": "home",
                "owner_node_id": payload.owner_node_id,
                "home_node_id": home_id,
            },
        }
        created = call_home_create_agent(base_url=home.base_url, home_node_id=home_id, payload=home_body)
        agent_id = str(created.get("agent_id") or "").strip()
        if not agent_id:
            raise HTTPException(status_code=502, detail="home_missing_agent_id")

        host = _host_from_metadata(home.metadata)
        # home 响应可覆盖 display
        if isinstance(created.get("host"), dict):
            host = ControlHostInfo(**{**host.model_dump(), **created["host"]})

        audit.record(
            actor=audit_actor(request, auth, fallback_agent_id=payload.owner_node_id),
            action="control.agent.create",
            target_agent_id=agent_id,
            detail={"owner_node_id": payload.owner_node_id, "home_node_id": home_id},
        )
        return ControlCreateAgentResponse(
            agent_id=agent_id,
            display_name=str(created.get("display_name") or payload.display_name),
            home_node_id=home_id,
            owner_node_id=payload.owner_node_id,
            origin="remote",
            host=host,
            config_snapshot=_parse_snapshot(created.get("config_snapshot")),
            sandbox_enabled=bool(created.get("sandbox_enabled")),
            sandbox_backend=str(created.get("sandbox_backend") or "process"),
            created_at=str(created.get("created_at") or ""),
            updated_at=str(created.get("updated_at") or ""),
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
            # home 已从 registry 消失时仍尝试（若还能拿到 base_url 则下面 404）
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
            # registry 无 home：视为远端已不可达，owner 仍可删本地引用
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
