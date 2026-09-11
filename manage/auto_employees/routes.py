from __future__ import annotations

from datetime import datetime, timezone

from fastapi import APIRouter, HTTPException, Query, Request

from manage.platform.auth import authenticate, ensure_node_identity, require_admin

from .models import AutoSummary, AutoSummaryList, AutoSummaryRecord
from .store import AutoSummaryConflict, AutoSummaryStore


def build_auto_employee_router(store: AutoSummaryStore) -> APIRouter:
    router = APIRouter(tags=["auto"])

    @router.put("/v1/registry/nodes/{node_id}/auto-summary", response_model=AutoSummaryRecord)
    def put_summary(node_id: str, payload: AutoSummary, request: Request) -> AutoSummaryRecord:
        auth = authenticate(request)
        ensure_node_identity(request, node_id, auth)
        try:
            return store.put(node_id, payload, datetime.now(timezone.utc))
        except AutoSummaryConflict as exc:
            raise HTTPException(status_code=409, detail=str(exc)) from exc

    @router.get("/v1/auto/overview", response_model=AutoSummaryList)
    def overview(request: Request, node_id: str | None = None, state: str | None = None, page: int = Query(1, ge=1), page_size: int = Query(50, ge=1, le=200)) -> AutoSummaryList:
        auth = authenticate(request)
        require_admin(auth)
        server_time = datetime.now(timezone.utc)
        items, total = store.list(node_id=node_id, state=state, page=page, page_size=page_size, stale_after_seconds=120, now=server_time)
        return AutoSummaryList(items=items, total=total, page=page, page_size=page_size, as_of=max((x.as_of for x in items), default=None), server_time=server_time)

    return router
