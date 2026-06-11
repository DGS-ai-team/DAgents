"""Register Center FastAPI 应用装配。"""

from __future__ import annotations

import asyncio
import os
import time
import uuid
from pathlib import Path
from typing import Literal

import httpx
from fastapi import FastAPI, HTTPException, Query, Request, Response
from fastapi.responses import RedirectResponse
from fastapi.staticfiles import StaticFiles

from metrics import metrics_text, record_a2a_operation, record_a2a_terminal_state
from rc_a2a_recent import A2ARecentLog
from rc_audit import AuditLog
from rc_auth import AuthContext, authenticate, require_admin
from rc_models import (
    A2ARecentListResponse,
    AgentListResponse,
    AgentRecord,
    AgentUpsertRequest,
    AuditListResponse,
    BroadcastRequest,
    BroadcastResponse,
    BroadcastResultItem,
    HealthResponse,
    RelayRequest,
    RelayResponse,
)
from rc_status import AgentStatus, is_deliverable
from rc_store import AgentListQuery, AgentRegistryStore

_A2A_TOKEN_HEADER = "x-dagents-a2a-token"
_TRACE_HEADER = "x-dagents-trace-id"
_UI_DIR = Path(__file__).resolve().parent / "ui"


def _shared_token() -> str:
    return os.environ.get("AGENT_PEER_SHARED_TOKEN", "").strip()


def _a2a_auth_headers() -> dict[str, str]:
    token = _shared_token()
    if not token:
        return {}
    return {_A2A_TOKEN_HEADER: token}


def _registry_store_path() -> str | None:
    raw = os.environ.get("REGISTER_CENTER_STORE_PATH", "").strip()
    return raw or None


def _trace_id(request: Request) -> str:
    raw = (request.headers.get(_TRACE_HEADER) or "").strip()
    return raw or uuid.uuid4().hex


def _ensure_member_can_access_groups(auth: AuthContext, groups: list[str]) -> None:
    if auth.is_admin:
        return
    for group in groups:
        if not auth.allows_discovery_group(group):
            raise HTTPException(status_code=403, detail=f"discovery_group={group!r} 不在 token 可见范围")


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


async def _broadcast_to_agent(
    client: httpx.AsyncClient,
    *,
    agent: AgentRecord,
    message: str,
    source: str,
) -> BroadcastResultItem:
    started = time.monotonic()
    session_id = f"broadcast-{agent.agent_id}-{uuid.uuid4().hex[:8]}"
    client_id = f"broadcast-{uuid.uuid4().hex}"
    target_url = f"{agent.base_url}/v1/messages"
    payload = {
        "session_id": session_id,
        "client_id": client_id,
        "request_type": "message",
        "content": message,
        "source": source,
        "priority": "human",
    }
    try:
        resp = await client.post(target_url, json=payload, headers=_a2a_auth_headers())
        detail = None
        try:
            body = resp.json()
        except Exception:
            body = None
        if resp.status_code >= 400:
            detail = resp.text[:300]
            record_a2a_operation(
                component="register_center",
                operation="broadcast_downstream",
                status="http_error",
                elapsed_seconds=time.monotonic() - started,
            )
            return BroadcastResultItem(
                agent_id=agent.agent_id,
                base_url=agent.base_url,
                discovery_group=agent.discovery_group,
                ok=False,
                status_code=resp.status_code,
                session_id=session_id,
                client_id=client_id,
                detail=detail,
            )
        record_a2a_operation(
            component="register_center",
            operation="broadcast_downstream",
            status="ok",
            elapsed_seconds=time.monotonic() - started,
        )
        return BroadcastResultItem(
            agent_id=agent.agent_id,
            base_url=agent.base_url,
            discovery_group=agent.discovery_group,
            ok=True,
            status_code=resp.status_code,
            session_id=session_id,
            client_id=client_id,
            detail=None if body is not None else "下游返回非 JSON 响应",
        )
    except Exception as exc:
        record_a2a_operation(
            component="register_center",
            operation="broadcast_downstream",
            status="error",
            elapsed_seconds=time.monotonic() - started,
        )
        return BroadcastResultItem(
            agent_id=agent.agent_id,
            base_url=agent.base_url,
            discovery_group=agent.discovery_group,
            ok=False,
            status_code=None,
            session_id=session_id,
            client_id=client_id,
            detail=str(exc),
        )


def create_app() -> FastAPI:
    app = FastAPI(
        title="DAgents Register Center",
        version="0.3.0",
        description="企业 Agent 目录：登记、发现、广播与中继。",
    )
    store = AgentRegistryStore(persist_path=_registry_store_path())
    audit = AuditLog()
    a2a_recent = A2ARecentLog()

    @app.get("/metrics", tags=["system"])
    def get_metrics() -> Response:
        body, content_type = metrics_text()
        return Response(content=body, media_type=content_type)

    @app.get("/health", response_model=HealthResponse, tags=["system"])
    def get_health() -> HealthResponse:
        return HealthResponse(status="ok", agents=store.count())

    @app.post("/v1/agents", response_model=AgentRecord, tags=["agents"])
    def upsert_agent(payload: AgentUpsertRequest, request: Request) -> AgentRecord:
        auth = authenticate(request)
        _ensure_member_can_access_groups(auth, payload.discovery_group)
        record = store.upsert(payload)
        audit.record(
            actor=auth.token_id,
            action="agent.upsert",
            target_agent_id=record.agent_id,
            discovery_group=record.discovery_group[0] if record.discovery_group else None,
            detail={"status": record.status, "team": record.team},
        )
        return record

    @app.get("/v1/agents", response_model=AgentListResponse, tags=["agents"])
    def list_agents(
        request: Request,
        discovery_group: str | None = Query(default=None, description="按发现分组精确筛选；admin 可省略。"),
        team: str | None = Query(default=None),
        status: AgentStatus | Literal["all"] = Query(default="online"),
        q: str | None = Query(default=None, description="模糊匹配 name/agent_id/description。"),
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

    @app.get("/v1/agents/{agent_id}", response_model=AgentRecord, tags=["agents"])
    def get_agent(
        agent_id: str,
        request: Request,
        discovery_group: str = Query(..., description="调用方所属分组（必填）。"),
    ) -> AgentRecord:
        auth = authenticate(request)
        if not auth.allows_discovery_group(discovery_group):
            raise HTTPException(status_code=403, detail=f"discovery_group={discovery_group!r} 不在 token 可见范围")
        record = store.get(agent_id)
        if record is None or discovery_group not in record.discovery_group:
            raise HTTPException(status_code=404, detail=f"agent_id={agent_id!r} 不存在")
        return record

    @app.delete("/v1/agents/{agent_id}", tags=["agents"])
    def delete_agent(agent_id: str, request: Request) -> dict[str, bool]:
        auth = authenticate(request)
        existing = store.get(agent_id)
        if existing is None:
            raise HTTPException(status_code=404, detail=f"agent_id={agent_id!r} 不存在")
        if not auth.is_admin:
            _ensure_member_can_access_groups(auth, existing.discovery_group)
        ok = store.delete(agent_id)
        if not ok:
            raise HTTPException(status_code=404, detail=f"agent_id={agent_id!r} 不存在")
        audit.record(actor=auth.token_id, action="agent.delete", target_agent_id=agent_id)
        return {"deleted": True}

    @app.get("/v1/admin/audit", response_model=AuditListResponse, tags=["admin"])
    def list_audit(
        request: Request,
        limit: int = Query(default=100, ge=1, le=500),
    ) -> AuditListResponse:
        auth = authenticate(request)
        require_admin(auth)
        return AuditListResponse(events=audit.list_recent(limit=limit))

    @app.get("/v1/admin/a2a/recent", response_model=A2ARecentListResponse, tags=["admin"])
    def list_a2a_recent(
        request: Request,
        limit: int = Query(default=100, ge=1, le=500),
    ) -> A2ARecentListResponse:
        auth = authenticate(request)
        require_admin(auth)
        return A2ARecentListResponse(entries=a2a_recent.list_recent(limit=limit))

    @app.post("/v1/broadcast", response_model=BroadcastResponse, tags=["broadcast"])
    async def broadcast_message(
        payload: BroadcastRequest,
        request: Request,
        response: Response,
    ) -> BroadcastResponse:
        auth = authenticate(request)
        _ensure_member_can_access_groups(auth, payload.discovery_group_ids)
        trace_id = _trace_id(request)
        started_unix = int(time.time())
        started = time.monotonic()
        targets_by_id: dict[str, AgentRecord] = {}
        for group_id in payload.discovery_group_ids:
            for item in store.list_deliverable(discovery_group=group_id):
                targets_by_id[item.agent_id] = item
        targets = list(targets_by_id.values())
        if not targets:
            record_a2a_operation(
                component="register_center",
                operation="broadcast",
                status="no_targets",
                elapsed_seconds=time.monotonic() - started,
            )
            record_a2a_terminal_state(component="register_center", operation="broadcast", final_state="no_targets")
            a2a_recent.record(
                trace_id=trace_id,
                operation="broadcast",
                delivery_mode="relay",
                caller_groups=payload.discovery_group_ids,
                started_at_unix=started_unix,
                final_state="no_targets",
            )
            audit.record(
                actor=auth.token_id,
                action="broadcast",
                detail={"trace_id": trace_id, "final_state": "no_targets", "groups": payload.discovery_group_ids},
            )
            response.headers[_TRACE_HEADER] = trace_id
            return BroadcastResponse(
                message=payload.message,
                discovery_group_ids=payload.discovery_group_ids,
                total_targets=0,
                success_count=0,
                failed_count=0,
                results=[],
            )

        async with httpx.AsyncClient(timeout=20.0) as client:
            tasks = [
                _broadcast_to_agent(
                    client,
                    agent=target,
                    message=payload.message,
                    source=payload.source,
                )
                for target in targets
            ]
            results = await asyncio.gather(*tasks)

        success_count = sum(1 for item in results if item.ok)
        failed_count = len(results) - success_count
        if failed_count == 0:
            final_status = "ok"
        elif success_count == 0:
            final_status = "failed"
        else:
            final_status = "partial"
        record_a2a_operation(
            component="register_center",
            operation="broadcast",
            status=final_status,
            elapsed_seconds=time.monotonic() - started,
        )
        record_a2a_terminal_state(component="register_center", operation="broadcast", final_state=final_status)
        a2a_recent.record(
            trace_id=trace_id,
            operation="broadcast",
            delivery_mode="relay",
            caller_groups=payload.discovery_group_ids,
            started_at_unix=started_unix,
            final_state=final_status,
            error_summary=None if final_status == "ok" else f"failed={failed_count}",
        )
        audit.record(
            actor=auth.token_id,
            action="broadcast",
            detail={
                "trace_id": trace_id,
                "final_state": final_status,
                "groups": payload.discovery_group_ids,
                "total_targets": len(results),
            },
        )
        response.headers[_TRACE_HEADER] = trace_id
        return BroadcastResponse(
            message=payload.message,
            discovery_group_ids=payload.discovery_group_ids,
            total_targets=len(results),
            success_count=success_count,
            failed_count=failed_count,
            results=results,
        )

    @app.post("/v1/relay", response_model=RelayResponse, tags=["relay"])
    async def relay_message(
        payload: RelayRequest,
        request: Request,
        response: Response,
    ) -> RelayResponse:
        auth = authenticate(request)
        if payload.caller_groups:
            _ensure_member_can_access_groups(auth, payload.caller_groups)
        trace_id = _trace_id(request)
        started_unix = int(time.time())
        started = time.monotonic()
        target = store.get(payload.target_agent_id)
        if target is None:
            record_a2a_operation(
                component="register_center",
                operation="relay",
                status="target_not_found",
                elapsed_seconds=time.monotonic() - started,
            )
            raise HTTPException(status_code=404, detail=f"agent_id={payload.target_agent_id!r} 不存在")
        if payload.caller_groups and not any(group in target.discovery_group for group in payload.caller_groups):
            record_a2a_operation(
                component="register_center",
                operation="relay",
                status="forbidden_group",
                elapsed_seconds=time.monotonic() - started,
            )
            raise HTTPException(status_code=404, detail=f"agent_id={payload.target_agent_id!r} 不存在")
        if not is_deliverable(now_unix=int(time.time()), expires_at_unix=target.expires_at_unix):
            record_a2a_operation(
                component="register_center",
                operation="relay",
                status="agent_offline",
                elapsed_seconds=time.monotonic() - started,
            )
            a2a_recent.record(
                trace_id=trace_id,
                operation="relay",
                delivery_mode="relay",
                caller_groups=payload.caller_groups,
                target_agent_id=target.agent_id,
                target_session_id=payload.session_id,
                started_at_unix=started_unix,
                final_state="agent_offline",
                error_summary=f"status={target.status}",
            )
            raise HTTPException(status_code=409, detail=f"agent_id={payload.target_agent_id!r} 不在线")

        target_url = f"{target.base_url}/v1/messages"
        forward_payload = {
            "session_id": payload.session_id,
            "client_id": payload.client_id,
            "request_type": payload.request_type,
            "source": payload.source,
            "priority": payload.priority,
        }
        if payload.request_type == "resume":
            forward_payload["resume_value"] = payload.resume_value
        else:
            forward_payload["content"] = payload.content
        async with httpx.AsyncClient(timeout=20.0) as client:
            try:
                resp = await client.post(target_url, json=forward_payload, headers=_a2a_auth_headers())
            except Exception as exc:
                record_a2a_operation(
                    component="register_center",
                    operation="relay",
                    status="downstream_error",
                    elapsed_seconds=time.monotonic() - started,
                )
                a2a_recent.record(
                    trace_id=trace_id,
                    operation="relay",
                    delivery_mode="relay",
                    caller_groups=payload.caller_groups,
                    target_agent_id=target.agent_id,
                    target_session_id=payload.session_id,
                    started_at_unix=started_unix,
                    final_state="failed",
                    error_summary=str(exc),
                )
                raise HTTPException(status_code=502, detail=f"relay 下游调用失败: {exc}") from exc
        if resp.status_code >= 400:
            record_a2a_operation(
                component="register_center",
                operation="relay",
                status="downstream_http_error",
                elapsed_seconds=time.monotonic() - started,
            )
            error_text = resp.text[:300]
            a2a_recent.record(
                trace_id=trace_id,
                operation="relay",
                delivery_mode="relay",
                caller_groups=payload.caller_groups,
                target_agent_id=target.agent_id,
                target_session_id=payload.session_id,
                started_at_unix=started_unix,
                final_state="failed",
                error_summary=error_text,
            )
            raise HTTPException(status_code=502, detail=f"relay 下游返回错误: {error_text}")
        record_a2a_operation(
            component="register_center",
            operation="relay",
            status="ok",
            elapsed_seconds=time.monotonic() - started,
        )
        record_a2a_terminal_state(component="register_center", operation="relay", final_state="accepted")
        a2a_recent.record(
            trace_id=trace_id,
            operation="relay",
            delivery_mode="relay",
            caller_groups=payload.caller_groups,
            target_agent_id=target.agent_id,
            target_session_id=payload.session_id,
            started_at_unix=started_unix,
            final_state="accepted",
        )
        audit.record(
            actor=auth.token_id,
            action="relay",
            target_agent_id=target.agent_id,
            detail={"trace_id": trace_id, "session_id": payload.session_id},
        )
        response.headers[_TRACE_HEADER] = trace_id
        return RelayResponse(
            accepted=True,
            target_agent_id=target.agent_id,
            target_base_url=target.base_url,
            session_id=payload.session_id,
            client_id=payload.client_id,
        )

    @app.get("/ui", include_in_schema=False)
    def ui_root_redirect() -> RedirectResponse:
        return RedirectResponse(url="/ui/")

    if _UI_DIR.is_dir():
        app.mount("/ui", StaticFiles(directory=str(_UI_DIR), html=True), name="directory-ui")

    return app


app = create_app()
