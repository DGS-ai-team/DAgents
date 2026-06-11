"""Manage FastAPI 应用装配（M0 + M1）。"""

from __future__ import annotations

from pathlib import Path

from fastapi import FastAPI, Query, Request, Response
from fastapi.responses import RedirectResponse
from fastapi.staticfiles import StaticFiles

from manage.config import ManageSettings
from manage.platform.audit import AuditLog
from manage.platform.auth import authenticate, require_admin
from manage.platform.blob import BlobStore, BlobStoreConfig
from manage.platform.metrics import metrics_text
from manage.registry.models import AuditListResponse, HealthResponse
from manage.registry.routes import build_registry_router
from manage.registry.store import AgentRegistryStore
from manage.storage.sqlite import SQLiteDatabase

_CONSOLE_DIR = Path(__file__).resolve().parent / "console" / "static"


def create_app(settings: ManageSettings | None = None) -> FastAPI:
    cfg = settings or ManageSettings.from_env()
    app = FastAPI(
        title="DAgents Manage",
        version="0.4.0-m0m1",
        description="统一控制面：Registry（M1）+ Platform（M0）；A2A/Skills 待 M2/M3。",
    )
    db = SQLiteDatabase(cfg.db_path)
    store = AgentRegistryStore(db=db if db.enabled else None)
    audit = AuditLog(max_entries=cfg.audit_max_entries)
    blob = BlobStore(BlobStoreConfig.from_settings(cfg))

    @app.get("/health", response_model=HealthResponse, tags=["system"])
    def health() -> HealthResponse:
        return HealthResponse(status="ok", agents=store.count(), blob=blob.status())

    @app.get("/metrics", tags=["system"])
    def metrics() -> Response:
        body, content_type = metrics_text()
        return Response(content=body, media_type=content_type)

    @app.get("/v1/admin/audit", response_model=AuditListResponse, tags=["admin"])
    def list_audit(
        request: Request,
        limit: int = Query(default=100, ge=1, le=500),
    ) -> AuditListResponse:
        auth = authenticate(request)
        require_admin(auth)
        return AuditListResponse(events=audit.list_recent(limit=limit))

    app.include_router(build_registry_router(store, audit))

    @app.get("/", include_in_schema=False)
    def root_redirect() -> RedirectResponse:
        return RedirectResponse(url="/console/")

    @app.get("/console", include_in_schema=False)
    def console_redirect() -> RedirectResponse:
        return RedirectResponse(url="/console/")

    if _CONSOLE_DIR.is_dir():
        app.mount("/console", StaticFiles(directory=str(_CONSOLE_DIR), html=True), name="console")

    app.state.manage_settings = cfg
    app.state.registry_store = store
    return app


app = create_app()
