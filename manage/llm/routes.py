from __future__ import annotations
import time
from fastapi import APIRouter, HTTPException, Request
from manage.llm.models import (
    LLMConfig,
    LLMConfigCreate,
    LLMConfigMasked,
    LLMProbeRequest,
    LLMProbeResponse,
    LLMResolved,
)
from manage.llm.probe import probe_models
from manage.llm.store import LLMConfigStore
from manage.platform.audit import AuditLog
from manage.platform.auth import authenticate, require_admin

def build_llm_router(store: LLMConfigStore, audit: AuditLog) -> APIRouter:
    router = APIRouter(prefix="/v1/llm", tags=["llm"])

    @router.post("/configs", response_model=LLMConfigMasked)
    def create_config(payload: LLMConfigCreate, request: Request) -> LLMConfigMasked:
        auth = authenticate(request)
        require_admin(auth)
        cfg = store.create(payload, now=int(time.time()))
        audit.record(actor=auth.token_id, action="llm_config.create", target_agent_id=cfg.id)
        return store.mask(cfg)

    @router.get("/configs", response_model=list[LLMConfigMasked])
    def list_configs(request: Request) -> list[LLMConfigMasked]:
        auth = authenticate(request)
        return [
            store.mask(c)
            for c in store.list()
            if auth.allows_resource_groups(c.allowed_groups)
        ]

    @router.get("/configs/default/resolve", response_model=LLMResolved)
    def resolve_default(request: Request) -> LLMResolved:
        auth = authenticate(request)
        cfg = store.get_default()
        if not cfg or not auth.allows_resource_groups(cfg.allowed_groups):
            raise HTTPException(status_code=404, detail="no default llm config")
        return store.resolve(cfg)

    @router.post("/probe-models", response_model=LLMProbeResponse)
    def probe_llm_models(payload: LLMProbeRequest, request: Request) -> LLMProbeResponse:
        auth = authenticate(request)
        require_admin(auth)
        api_key = (payload.api_key or "").strip()
        config_id = (payload.config_id or "").strip()
        if not api_key and config_id:
            cfg = store.get(config_id)
            if not cfg:
                raise HTTPException(status_code=404, detail="config not found")
            api_key = cfg.api_key or ""
        try:
            result = probe_models(payload.base_url, api_key)
        except ValueError as exc:
            raise HTTPException(status_code=400, detail=str(exc)) from exc
        return LLMProbeResponse(**result)

    @router.get("/configs/{cfg_id}", response_model=LLMConfigMasked)
    def get_config(cfg_id: str, request: Request) -> LLMConfigMasked:
        auth = authenticate(request)
        cfg = store.get(cfg_id)
        if not cfg or not auth.allows_resource_groups(cfg.allowed_groups):
            raise HTTPException(status_code=404, detail="not found")
        return store.mask(cfg)

    @router.get("/configs/{cfg_id}/resolve", response_model=LLMResolved)
    def resolve_config(cfg_id: str, request: Request) -> LLMResolved:
        auth = authenticate(request)
        cfg = store.get(cfg_id)
        if not cfg or not auth.allows_resource_groups(cfg.allowed_groups):
            raise HTTPException(status_code=404, detail="not found")
        return store.resolve(cfg)

    @router.put("/configs/{cfg_id}", response_model=LLMConfigMasked)
    def update_config(cfg_id: str, payload: LLMConfigCreate, request: Request) -> LLMConfigMasked:
        auth = authenticate(request)
        require_admin(auth)
        cfg = store.update(cfg_id, payload, now=int(time.time()))
        if not cfg:
            raise HTTPException(status_code=404, detail="not found")
        audit.record(actor=auth.token_id, action="llm_config.update", target_agent_id=cfg_id)
        return store.mask(cfg)

    @router.delete("/configs/{cfg_id}")
    def delete_config(cfg_id: str, request: Request) -> dict[str, bool]:
        auth = authenticate(request)
        require_admin(auth)
        ok = store.delete(cfg_id)
        if not ok:
            raise HTTPException(status_code=404, detail="not found")
        audit.record(actor=auth.token_id, action="llm_config.delete", target_agent_id=cfg_id)
        return {"deleted": True}

    return router
