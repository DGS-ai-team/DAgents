"""Release Hub API routes。"""

from __future__ import annotations

import hashlib
import time
from pathlib import Path

from fastapi import APIRouter, File, Form, HTTPException, Query, Request, Response, UploadFile

from manage.platform.audit import AuditLog
from manage.platform.auth import authenticate, require_admin
from manage.releases.files import (
    allowed_release_filename,
    content_type_for_filename,
    package_file_path,
    rel_path_for,
    remove_package_tree,
    validate_slug,
    version_dir,
    write_latest_pointer,
    write_manifest,
)
from manage.releases.models import (
    DEFAULT_ARTIFACT,
    DEFAULT_CHANNEL,
    PLATFORMS,
    ReleaseCheckResponse,
    ReleasePackage,
    ReleasePublishBody,
    ReleaseUploadResponse,
)
from manage.releases.store import ReleasePackageStore


def build_releases_router(
    store: ReleasePackageStore,
    audit: AuditLog,
    *,
    releases_dir: Path | None,
    release_max_bytes: int | None,
) -> APIRouter:
    router = APIRouter(prefix="/v1/releases", tags=["releases"])

    def _require_releases_dir() -> Path:
        if releases_dir is None:
            raise HTTPException(status_code=503, detail="release store not configured")
        releases_dir.mkdir(parents=True, exist_ok=True)
        return releases_dir

    def _download_url(artifact: str, channel: str, platform: str, version: str) -> str:
        return (
            f"/v1/releases/packages/{artifact}/{channel}/{platform}/{version}/download"
        )

    @router.post("/packages", response_model=ReleaseUploadResponse)
    async def upload_package(
        request: Request,
        file: UploadFile = File(...),
        artifact: str = Form(DEFAULT_ARTIFACT),
        version: str = Form(...),
        platform: str = Form(...),
        channel: str = Form(DEFAULT_CHANNEL),
        publish: str = Form("false"),
        set_latest: str = Form("false"),
        release_notes: str = Form(""),
    ) -> ReleaseUploadResponse:
        auth = authenticate(request)
        require_admin(auth)
        root = _require_releases_dir()
        filename = str(file.filename or "").strip()
        if not allowed_release_filename(filename):
            raise HTTPException(status_code=422, detail="file must be .tar.gz or .zip")
        try:
            artifact = validate_slug(artifact, "artifact")
            version = validate_slug(version, "version")
            platform = validate_slug(platform, "platform")
            channel = validate_slug(channel, "channel")
        except ValueError as exc:
            raise HTTPException(status_code=422, detail=str(exc)) from exc
        if platform not in PLATFORMS:
            raise HTTPException(status_code=422, detail="unsupported platform")
        existing = store.get(artifact, channel, platform, version)
        if existing and existing.status != "draft":
            raise HTTPException(status_code=409, detail="version already published")

        data = await file.read()
        if release_max_bytes is not None and len(data) > release_max_bytes:
            raise HTTPException(status_code=413, detail="release package too large")
        sha256 = hashlib.sha256(data).hexdigest()
        rel = rel_path_for(artifact, channel, platform, version, filename)
        dest_dir = version_dir(root, artifact, channel, platform, version)
        dest_dir.mkdir(parents=True, exist_ok=True)
        dest_file = dest_dir / filename
        dest_file.write_bytes(data)

        now = int(time.time())
        from manage.releases.models import ReleasePackageCreate

        payload = ReleasePackageCreate(
            artifact=artifact,
            version=version,
            platform=platform,
            channel=channel,
            filename=filename,
            sha256=sha256,
            size_bytes=len(data),
            content_type=content_type_for_filename(filename),
            release_notes=str(release_notes or "").strip(),
            uploaded_by=auth.token_id,
            rel_path=rel,
            source="upload",
        )
        pkg = store.create_draft(payload, now=now)
        if str(publish).lower() in {"1", "true", "yes", "on"}:
            promoted = store.publish(
                artifact,
                channel,
                platform,
                version,
                now=now,
                set_latest=str(set_latest).lower() in {"1", "true", "yes", "on"},
            )
            pkg = promoted or pkg
        write_manifest(dest_dir, pkg)
        if pkg.is_latest:
            write_latest_pointer(root, artifact, channel, platform, version, filename)
        audit.record(
            actor=auth.token_id,
            action="release.upload",
            target_agent_id=f"{artifact}@{version}:{platform}",
        )
        return ReleaseUploadResponse(
            **pkg.model_dump(),
            download_url=_download_url(artifact, channel, platform, version),
        )

    @router.get("/packages", response_model=list[ReleasePackage])
    def list_packages(
        request: Request,
        artifact: str | None = Query(default=None),
        channel: str | None = Query(default=None),
        platform: str | None = Query(default=None),
        status: str = Query(default="all"),
    ) -> list[ReleasePackage]:
        authenticate(request)
        return store.list_packages(
            artifact=artifact,
            channel=channel,
            platform=platform,
            status=status,
        )

    @router.post("/packages/{artifact}/{channel}/{platform}/{version}/publish", response_model=ReleasePackage)
    def publish_package(
        artifact: str,
        channel: str,
        platform: str,
        version: str,
        request: Request,
        body: ReleasePublishBody | None = None,
    ) -> ReleasePackage:
        auth = authenticate(request)
        require_admin(auth)
        root = _require_releases_dir()
        now = int(time.time())
        set_latest = bool(body.set_latest) if body else False
        pkg = store.publish(artifact, channel, platform, version, now=now, set_latest=set_latest)
        if not pkg:
            raise HTTPException(status_code=404, detail="not found")
        dest_dir = version_dir(root, artifact, channel, platform, version)
        write_manifest(dest_dir, pkg)
        if pkg.is_latest:
            write_latest_pointer(root, artifact, channel, platform, version, pkg.filename)
        audit.record(
            actor=auth.token_id,
            action="release.publish",
            target_agent_id=f"{artifact}@{version}:{platform}",
        )
        return pkg

    @router.post("/packages/{artifact}/{channel}/{platform}/{version}/promote", response_model=ReleasePackage)
    def promote_package(
        artifact: str,
        channel: str,
        platform: str,
        version: str,
        request: Request,
    ) -> ReleasePackage:
        auth = authenticate(request)
        require_admin(auth)
        root = _require_releases_dir()
        now = int(time.time())
        pkg = store.promote(artifact, channel, platform, version, now=now)
        if not pkg:
            raise HTTPException(status_code=404, detail="not found or not published")
        write_latest_pointer(root, artifact, channel, platform, version, pkg.filename)
        dest_dir = version_dir(root, artifact, channel, platform, version)
        write_manifest(dest_dir, pkg)
        audit.record(
            actor=auth.token_id,
            action="release.promote",
            target_agent_id=f"{artifact}@{version}:{platform}",
        )
        return pkg

    @router.delete("/packages/{artifact}/{channel}/{platform}/{version}")
    def delete_package(
        artifact: str,
        channel: str,
        platform: str,
        version: str,
        request: Request,
    ) -> dict:
        auth = authenticate(request)
        require_admin(auth)
        root = _require_releases_dir()
        try:
            pkg = store.delete(artifact, channel, platform, version)
        except ValueError as exc:
            raise HTTPException(status_code=409, detail=str(exc)) from exc
        if not pkg:
            raise HTTPException(status_code=404, detail="not found")
        remove_package_tree(root, pkg)
        audit.record(
            actor=auth.token_id,
            action="release.delete",
            target_agent_id=f"{artifact}@{version}:{platform}",
        )
        return {"deleted": True}

    def _file_response(pkg: ReleasePackage) -> Response:
        root = _require_releases_dir()
        path = package_file_path(root, pkg)
        if not path.is_file():
            raise HTTPException(status_code=404, detail="package file missing")
        data = path.read_bytes()
        headers = {
            "X-Release-Version": pkg.version,
            "X-Release-Sha256": pkg.sha256,
        }
        return Response(content=data, media_type=pkg.content_type, headers=headers)

    @router.get("/packages/{artifact}/{channel}/{platform}/latest/download")
    def download_latest(
        artifact: str,
        channel: str,
        platform: str,
        request: Request,
    ) -> Response:
        authenticate(request)
        pkg = store.get_latest(artifact, channel, platform)
        if not pkg:
            raise HTTPException(status_code=404, detail="latest not found")
        return _file_response(pkg)

    @router.get("/packages/{artifact}/{channel}/{platform}/{version}/download")
    def download_version(
        artifact: str,
        channel: str,
        platform: str,
        version: str,
        request: Request,
    ) -> Response:
        authenticate(request)
        pkg = store.get(artifact, channel, platform, version)
        if not pkg or pkg.status != "published":
            raise HTTPException(status_code=404, detail="not found")
        return _file_response(pkg)

    @router.get("/check", response_model=ReleaseCheckResponse)
    def check_release(
        request: Request,
        current: str = Query(..., min_length=1),
        platform: str = Query(..., min_length=1),
        channel: str = Query(default=DEFAULT_CHANNEL),
        artifact: str = Query(default=DEFAULT_ARTIFACT),
    ) -> ReleaseCheckResponse:
        authenticate(request)
        try:
            validate_slug(platform, "platform")
            validate_slug(channel, "channel")
            validate_slug(artifact, "artifact")
        except ValueError as exc:
            raise HTTPException(status_code=422, detail=str(exc)) from exc
        return ReleaseCheckResponse(**store.check(
            current=current,
            platform=platform,
            channel=channel,
            artifact=artifact,
        ))

    return router
