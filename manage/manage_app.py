"""Manage FastAPI 应用装配。"""

from __future__ import annotations

from contextlib import asynccontextmanager
from pathlib import Path

from fastapi import FastAPI, Query, Request, Response
from fastapi.responses import RedirectResponse
from fastapi.staticfiles import StaticFiles

from manage.config import ManageSettings
from manage.platform.audit import AuditLog
from manage.platform.auth import authenticate, require_admin
from manage.platform.auth_routes import build_auth_router
from manage.platform.blob import BlobStore, BlobStoreConfig
from manage.platform.metrics import metrics_text
from manage.platform.sessions import SessionStore
from manage.registry.models import AuditListResponse, HealthResponse
from manage.llm.routes import build_llm_router
from manage.llm.store import LLMConfigStore
from manage.platform.blob_routes import build_blob_router
from manage.registry.routes import build_registry_router
from manage.registry.store import AgentRegistryStore
from manage.cases.routes import build_cases_router
from manage.cases.store import CaseExampleStore
from manage.releases.routes import build_releases_router
from manage.releases.seed import seed_bundled_releases
from manage.releases.store import ReleasePackageStore
from manage.externaltools.routes import build_externaltools_router
from manage.externaltools.store import ExternalToolPackageStore
from manage.plugins.routes import build_plugins_router
from manage.plugins.store import PluginPackageStore
from manage.skills.routes import build_skills_router
from manage.skills.store import SkillPackageStore
from manage.storage.sqlite import SQLiteDatabase
from manage.version import read_version
from manage.workgroup.routes import build_workgroup_router
from manage.workgroup.store import WorkGroupStore
from manage.workgroup.vertical import VerticalLoop
from manage.workgroup.ws_hub import WorkgroupWSHub
from manage.workgroup.ws_routes import build_workgroup_ws_router

_CONSOLE_DIR = Path(__file__).resolve().parent / "console" / "static"


def create_app(settings: ManageSettings | None = None) -> FastAPI:
    cfg = settings or ManageSettings.from_env()
    db = SQLiteDatabase(cfg.db_path)
    store = AgentRegistryStore(db=db if db.enabled else None)
    llm_store = LLMConfigStore(db=db if db.enabled else None)
    audit = AuditLog(max_entries=cfg.audit_max_entries)
    blob = BlobStore(BlobStoreConfig.from_settings(cfg))
    releases_store = ReleasePackageStore(db=db if db.enabled else None)
    session_store = SessionStore()

    @asynccontextmanager
    async def lifespan(app: FastAPI):
        seed_bundled_releases(
            bundled_dir=cfg.bundled_releases_dir,
            releases_dir=cfg.releases_dir,
            store=releases_store,
            enabled=cfg.seed_bundled_releases,
        )
        yield

    app = FastAPI(
        title="DAgents Manage",
        version=read_version(),
        description="统一控制面：Registry + 工作组 + Platform（制品/案例/发布）。",
        lifespan=lifespan,
    )

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

    skills_store = SkillPackageStore(db=db if db.enabled else None)
    externaltools_store = ExternalToolPackageStore(db=db if db.enabled else None)
    plugins_store = PluginPackageStore(db=db if db.enabled else None)
    cases_store = CaseExampleStore(db=db if db.enabled else None)
    workgroup_store = WorkGroupStore(db=db if db.enabled else None)
    workgroup_ws_hub = WorkgroupWSHub(store=workgroup_store)
    workgroup_loop = VerticalLoop(workgroup_store, hub=workgroup_ws_hub)

    app.include_router(build_auth_router(session_store, store))
    app.include_router(build_registry_router(store, audit))
    app.include_router(
        build_workgroup_router(
            workgroup_store,
            audit,
            hub=workgroup_ws_hub,
            llm_store=llm_store,
            registry_store=store,
            loop=workgroup_loop,
        )
    )
    app.include_router(
        build_workgroup_ws_router(workgroup_ws_hub, on_inbound=workgroup_loop.handle_inbound)
    )
    app.include_router(build_llm_router(llm_store, audit))
    app.include_router(build_blob_router(blob))
    app.include_router(build_skills_router(skills_store, blob, audit))
    app.include_router(build_externaltools_router(externaltools_store, blob, audit))
    app.include_router(build_plugins_router(plugins_store, blob, audit))
    app.include_router(
        build_cases_router(
            cases_store,
            audit,
            blob=blob,
            skills_store=skills_store,
            plugins_store=plugins_store,
            externaltools_store=externaltools_store,
        )
    )
    app.include_router(
        build_releases_router(
            releases_store,
            audit,
            releases_dir=cfg.releases_dir,
            release_max_bytes=cfg.release_max_bytes,
        )
    )

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
    app.state.session_store = session_store
    app.state.llm_store = llm_store
    app.state.skills_store = skills_store
    app.state.externaltools_store = externaltools_store
    app.state.plugins_store = plugins_store
    app.state.cases_store = cases_store
    app.state.releases_store = releases_store
    app.state.workgroup_store = workgroup_store
    app.state.workgroup_ws_hub = workgroup_ws_hub
    app.state.workgroup_loop = workgroup_loop
    return app


app = create_app()
