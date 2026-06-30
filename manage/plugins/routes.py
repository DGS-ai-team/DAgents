"""Hook plugin registry API routes (upload/publish/catalog/download/sync)."""

from __future__ import annotations

import time

from fastapi import APIRouter, File, Form, HTTPException, Request, Response, UploadFile
from pydantic import ValidationError

from manage.platform.audit import AuditLog
from manage.platform.auth import authenticate, require_admin
from manage.platform.blob import BlobStore
from manage.plugins.models import PluginPackage, PluginPackageCreate
from manage.plugins.store import PluginPackageStore


def build_plugins_router(
    store: PluginPackageStore,
    blob: BlobStore,
    audit: AuditLog,
) -> APIRouter:
    router = APIRouter(prefix="/v1/plugins", tags=["plugins"])

    @router.post("/packages", response_model=PluginPackage)
    async def upload(
        request: Request,
        file: UploadFile = File(...),
        plugin_id: str = Form(...),
        version: str = Form(...),
        name: str = Form(...),
        description: str = Form(""),
        platform: str = Form("any"),
        owner: str = Form(""),
        team: str = Form(""),
        risk_level: str = Form("low"),
    ) -> PluginPackage:
        auth = authenticate(request)
        require_admin(auth)
        try:
            payload = PluginPackageCreate(
                plugin_id=plugin_id,
                version=version,
                name=name,
                description=description,
                platform=platform,
                owner=owner,
                team=team,
                risk_level=risk_level,
            )
        except ValidationError as exc:
            raise HTTPException(status_code=422, detail="invalid plugin metadata") from exc
        data = await file.read()
        content_type = file.content_type or "application/octet-stream"
        meta = blob.put(data, content_type=content_type)
        payload = payload.model_copy(update={"blob_id": meta["blob_id"]})
        pkg = store.create(payload, now=int(time.time()))
        audit.record(
            actor=auth.token_id,
            action="plugin.upload",
            target_agent_id=f"{plugin_id}@{version}",
        )
        return pkg

    @router.post("/packages/{plugin_id}/versions/{version}/publish", response_model=PluginPackage)
    def publish(plugin_id: str, version: str, request: Request) -> PluginPackage:
        auth = authenticate(request)
        require_admin(auth)
        pkg = store.publish(plugin_id, version, now=int(time.time()))
        if not pkg:
            raise HTTPException(status_code=404, detail="not found")
        audit.record(
            actor=auth.token_id,
            action="plugin.publish",
            target_agent_id=f"{plugin_id}@{version}",
        )
        return pkg

    @router.get("/catalog", response_model=list[PluginPackage])
    def catalog(request: Request) -> list[PluginPackage]:
        authenticate(request)
        return store.catalog()

    @router.get("/sync/manifest")
    def sync_manifest(request: Request, since: int = 0) -> dict:
        authenticate(request)
        return {
            "catalog_version": store.catalog_version(),
            "items": store.sync_manifest(since),
        }

    @router.get("/catalog/{plugin_id}", response_model=list[PluginPackage])
    def get_plugin(plugin_id: str, request: Request) -> list[PluginPackage]:
        authenticate(request)
        pkgs = store.get(plugin_id)
        if not pkgs:
            raise HTTPException(status_code=404, detail="not found")
        return pkgs

    @router.get("/catalog/{plugin_id}/versions/{version}/download")
    def download(plugin_id: str, version: str, request: Request) -> Response:
        authenticate(request)
        pkg = store.get_version(plugin_id, version)
        if not pkg or pkg.status != "published":
            raise HTTPException(status_code=404, detail="not found")
        got = blob.get(pkg.blob_id)
        if got is None:
            raise HTTPException(status_code=404, detail="blob missing")
        data, _meta = got
        return Response(content=data, media_type="application/octet-stream")

    return router
