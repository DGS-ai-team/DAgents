"""External tools registry API routes (upload/publish/catalog/download/sync)."""

from __future__ import annotations

import time

from fastapi import APIRouter, File, Form, HTTPException, Request, Response, UploadFile
from pydantic import ValidationError

from manage.externaltools.models import ExternalToolPackage, ExternalToolPackageCreate
from manage.externaltools.store import ExternalToolPackageStore
from manage.platform.audit import AuditLog
from manage.platform.auth import authenticate, require_admin
from manage.platform.blob import BlobStore


def build_externaltools_router(
    store: ExternalToolPackageStore,
    blob: BlobStore,
    audit: AuditLog,
) -> APIRouter:
    router = APIRouter(prefix="/v1/externaltools", tags=["externaltools"])

    @router.post("/packages", response_model=ExternalToolPackage)
    async def upload(
        request: Request,
        file: UploadFile = File(...),
        tool_id: str = Form(...),
        version: str = Form(...),
        name: str = Form(...),
        description: str = Form(""),
        platform: str = Form("any"),
        owner: str = Form(""),
        team: str = Form(""),
        risk_level: str = Form("low"),
    ) -> ExternalToolPackage:
        auth = authenticate(request)
        require_admin(auth)
        try:
            payload = ExternalToolPackageCreate(
                tool_id=tool_id,
                version=version,
                name=name,
                description=description,
                platform=platform,
                owner=owner,
                team=team,
                risk_level=risk_level,
            )
        except ValidationError as exc:
            raise HTTPException(status_code=422, detail="invalid external tool metadata") from exc
        data = await file.read()
        content_type = file.content_type or "application/octet-stream"
        meta = blob.put(data, content_type=content_type)
        payload = payload.model_copy(update={"blob_id": meta["blob_id"]})
        pkg = store.create(payload, now=int(time.time()))
        audit.record(
            actor=auth.token_id,
            action="externaltool.upload",
            target_agent_id=f"{tool_id}@{version}",
        )
        return pkg

    @router.post("/packages/{tool_id}/versions/{version}/publish", response_model=ExternalToolPackage)
    def publish(tool_id: str, version: str, request: Request) -> ExternalToolPackage:
        auth = authenticate(request)
        require_admin(auth)
        pkg = store.publish(tool_id, version, now=int(time.time()))
        if not pkg:
            raise HTTPException(status_code=404, detail="not found")
        audit.record(
            actor=auth.token_id,
            action="externaltool.publish",
            target_agent_id=f"{tool_id}@{version}",
        )
        return pkg

    @router.get("/catalog", response_model=list[ExternalToolPackage])
    def catalog(request: Request) -> list[ExternalToolPackage]:
        authenticate(request)
        return store.catalog()

    @router.get("/sync/manifest")
    def sync_manifest(request: Request, since: int = 0) -> dict:
        authenticate(request)
        return {
            "catalog_version": store.catalog_version(),
            "items": store.sync_manifest(since),
        }

    @router.get("/catalog/{tool_id}", response_model=list[ExternalToolPackage])
    def get_tool(tool_id: str, request: Request) -> list[ExternalToolPackage]:
        authenticate(request)
        pkgs = store.get(tool_id)
        if not pkgs:
            raise HTTPException(status_code=404, detail="not found")
        return pkgs

    @router.get("/catalog/{tool_id}/versions/{version}/download")
    def download(tool_id: str, version: str, request: Request) -> Response:
        authenticate(request)
        pkg = store.get_version(tool_id, version)
        if not pkg or pkg.status != "published":
            raise HTTPException(status_code=404, detail="not found")
        got = blob.get(pkg.blob_id)
        if got is None:
            raise HTTPException(status_code=404, detail="blob missing")
        data, _meta = got
        return Response(content=data, media_type="application/octet-stream")

    return router
