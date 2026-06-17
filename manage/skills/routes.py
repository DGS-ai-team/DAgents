"""Skills registry API routes (upload/publish/catalog/download/sync)."""

from __future__ import annotations

import time

from fastapi import APIRouter, File, Form, HTTPException, Request, Response, UploadFile

from manage.platform.audit import AuditLog
from manage.platform.auth import authenticate, require_admin
from manage.platform.blob import BlobStore
from manage.skills.models import SkillPackage, SkillPackageCreate
from manage.skills.store import SkillPackageStore


def build_skills_router(store: SkillPackageStore, blob: BlobStore, audit: AuditLog) -> APIRouter:
    router = APIRouter(prefix="/v1/skills", tags=["skills"])

    @router.post("/packages", response_model=SkillPackage)
    async def upload(
        request: Request,
        file: UploadFile = File(...),
        skill_id: str = Form(...),
        version: str = Form(...),
        name: str = Form(...),
        description: str = Form(""),
        owner: str = Form(""),
        team: str = Form(""),
        risk_level: str = Form("low"),
    ) -> SkillPackage:
        auth = authenticate(request)
        require_admin(auth)
        data = await file.read()
        meta = blob.put(data, content_type="application/zip")
        payload = SkillPackageCreate(
            skill_id=skill_id,
            version=version,
            name=name,
            description=description,
            owner=owner,
            team=team,
            risk_level=risk_level,
            blob_id=meta["blob_id"],
        )
        pkg = store.create(payload, now=int(time.time()))
        audit.record(
            actor=auth.token_id,
            action="skill.upload",
            target_agent_id=f"{skill_id}@{version}",
        )
        return pkg

    @router.post("/packages/{skill_id}/versions/{version}/publish", response_model=SkillPackage)
    def publish(skill_id: str, version: str, request: Request) -> SkillPackage:
        auth = authenticate(request)
        require_admin(auth)
        pkg = store.publish(skill_id, version, now=int(time.time()))
        if not pkg:
            raise HTTPException(status_code=404, detail="not found")
        audit.record(
            actor=auth.token_id,
            action="skill.publish",
            target_agent_id=f"{skill_id}@{version}",
        )
        return pkg

    # STATIC paths declared before parameterized /catalog/{skill_id}
    @router.get("/catalog", response_model=list[SkillPackage])
    def catalog(request: Request) -> list[SkillPackage]:
        authenticate(request)
        return store.catalog()

    @router.get("/sync/manifest")
    def sync_manifest(request: Request, since: int = 0) -> list[dict]:
        authenticate(request)
        return store.sync_manifest(since)

    @router.get("/catalog/{skill_id}", response_model=list[SkillPackage])
    def get_skill(skill_id: str, request: Request) -> list[SkillPackage]:
        authenticate(request)
        pkgs = store.get(skill_id)
        if not pkgs:
            raise HTTPException(status_code=404, detail="not found")
        return pkgs

    @router.get("/catalog/{skill_id}/versions/{version}/download")
    def download(skill_id: str, version: str, request: Request) -> Response:
        authenticate(request)
        pkg = store.get_version(skill_id, version)
        if not pkg or pkg.status != "published":
            raise HTTPException(status_code=404, detail="not found")
        got = blob.get(pkg.blob_id)
        if got is None:
            raise HTTPException(status_code=404, detail="blob missing")
        data, _meta = got
        return Response(content=data, media_type="application/zip")

    return router
