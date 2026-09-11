from __future__ import annotations
from datetime import datetime, timezone
from fastapi import APIRouter, HTTPException, Query, Request
from manage.platform.auth import authenticate, ensure_node_identity, require_admin
from .models import FeedbackCreate, FeedbackPatch, FeedbackRecord
from .store import FeedbackConflict, FeedbackStore
import threading
import time


def _allow(rate_lock, rate, node: str) -> bool:
    now = time.time()
    cutoff = now - 60
    with rate_lock:
        xs = [x for x in rate.get(node, []) if x > cutoff]
        if len(xs) >= 20:
            rate[node] = xs
            return False
        rate[node] = xs + [now]
        return True


def build_feedback_router(store: FeedbackStore):
    r = APIRouter(tags=["feedback"])
    rate_lock = threading.Lock()
    rate: dict[str, list[float]] = {}

    def now():
        return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")

    @r.post("/v1/feedback", response_model=FeedbackRecord)
    def create(payload: FeedbackCreate, request: Request):
        auth = authenticate(request)
        ensure_node_identity(request, payload.node_id, auth)
        existing = store.get(payload.node_id, payload.client_feedback_id)
        if existing is None and not _allow(rate_lock, rate, payload.node_id):
            raise HTTPException(429, detail="feedback rate limit exceeded")
        try:
            rec, created = store.create(payload, now())
        except FeedbackConflict as e:
            raise HTTPException(409, detail=str(e))
        return rec

    @r.get("/v1/feedback")
    def list_feedback(
        request: Request,
        status: str | None = None,
        category: str | None = None,
        node_id: str | None = None,
        page: int = Query(1, ge=1),
        page_size: int = Query(50, ge=1, le=200),
    ):
        auth = authenticate(request)
        if auth.is_admin:
            items, total = store.list(node_id=node_id, status=status, category=category, page=page, page_size=page_size)
            return {"items": items, "total": total, "page": page, "page_size": page_size}
        if not (auth.is_node or auth.session_kind == "node") or not auth.agent_id:
            raise HTTPException(403, detail="node identity required")
        ensure_node_identity(request, auth.agent_id, auth)
        nid = auth.agent_id
        items, total = store.list(node_id=nid, status=status, category=category, page=page, page_size=page_size)
        return {"items": items, "total": total, "page": page, "page_size": page_size}

    @r.get("/v1/feedback/{client_feedback_id}", response_model=FeedbackRecord)
    def get_feedback(client_feedback_id: str, request: Request, node_id: str | None = None):
        auth = authenticate(request)
        nid = auth.agent_id if not auth.is_admin else node_id
        if not nid:
            raise HTTPException(400, detail="node_id required")
        if not auth.is_admin:
            ensure_node_identity(request, nid, auth)
        rec = store.get(nid, client_feedback_id)
        if rec is None:
            raise HTTPException(404, detail="feedback not found")
        return rec

    @r.patch("/v1/feedback/{client_feedback_id}", response_model=FeedbackRecord)
    def patch(client_feedback_id: str, payload: FeedbackPatch, request: Request, node_id: str | None = None):
        auth = authenticate(request)
        require_admin(auth)
        if not node_id:
            raise HTTPException(400, detail="node_id required")
        try:
            rec = store.patch(node_id, client_feedback_id, payload, now())
        except FeedbackConflict as e:
            raise HTTPException(409, detail=str(e))
        if rec is None:
            raise HTTPException(404, detail="feedback not found")
        return rec

    return r
