"""Release Hub API routes。"""

from __future__ import annotations

import hashlib
import logging
import os
import time
import uuid
from pathlib import Path

from fastapi import APIRouter, File, Form, HTTPException, Query, Request, Response, UploadFile
from fastapi.responses import FileResponse

from manage.platform.audit import AuditLog
from manage.platform.auth import authenticate, require_admin
from manage.releases.files import (
    content_type_for_filename,
    package_file_path,
    rel_path_for,
    remove_package_tree,
    safe_release_filename,
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
    ReleasePackageCreate,
    ReleasePublishBody,
    PublicReleasePackage,
    ReleaseUploadResponse,
)
from manage.releases.store import ReleasePackageStore

logger = logging.getLogger(__name__)


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
        try:
            artifact = validate_slug(artifact, "artifact")
            version = validate_slug(version, "version")
            platform = validate_slug(platform, "platform")
            channel = validate_slug(channel, "channel")
            filename = safe_release_filename(str(file.filename or ""))
        except ValueError as exc:
            raise HTTPException(status_code=422, detail=str(exc)) from exc
        if platform not in PLATFORMS:
            raise HTTPException(status_code=422, detail="unsupported platform")
        existing = store.get(artifact, channel, platform, version)
        if existing and existing.status != "draft":
            raise HTTPException(status_code=409, detail="version already published")
        if len(str(release_notes or "").encode("utf-8")) > 16_384:
            raise HTTPException(status_code=422, detail="release notes exceed 16 KiB")

        root = _require_releases_dir()
        rel = rel_path_for(artifact, channel, platform, version, filename)
        dest_dir = version_dir(root, artifact, channel, platform, version)
        dest_dir.mkdir(parents=True, exist_ok=True)
        dest_file = dest_dir / filename
        staged_file = dest_dir / f".upload-{uuid.uuid4().hex}.tmp"
        backup_file: Path | None = None
        previous_file: Path | None = None
        if existing:
            try:
                previous_candidate = package_file_path(root, existing).resolve()
                if previous_candidate.is_relative_to(dest_dir.resolve()):
                    previous_file = previous_candidate
            except (OSError, ValueError):
                previous_file = None
        installed = False
        metadata_saved = False
        digest = hashlib.sha256()
        size_bytes = 0
        try:
            with staged_file.open("xb") as output:
                while chunk := await file.read(1024 * 1024):
                    size_bytes += len(chunk)
                    if release_max_bytes is not None and size_bytes > release_max_bytes:
                        raise HTTPException(status_code=413, detail="release package too large")
                    digest.update(chunk)
                    output.write(chunk)
            if size_bytes == 0:
                raise HTTPException(status_code=422, detail="package file is empty")

            if dest_file.exists():
                backup_file = dest_dir / f".previous-{uuid.uuid4().hex}.tmp"
                os.replace(dest_file, backup_file)
            os.replace(staged_file, dest_file)
            installed = True

            now = int(time.time())
            payload = ReleasePackageCreate(
                artifact=artifact,
                version=version,
                platform=platform,
                channel=channel,
                filename=filename,
                sha256=digest.hexdigest(),
                size_bytes=size_bytes,
                content_type=content_type_for_filename(filename),
                release_notes=str(release_notes or "").strip(),
                uploaded_by=auth.token_id,
                rel_path=rel,
                source="upload",
            )
            pkg = store.create_draft(payload, now=now)
            metadata_saved = True
            if str(publish).lower() in {"1", "true", "yes", "on"}:
                pkg = store.publish(
                    artifact,
                    channel,
                    platform,
                    version,
                    now=now,
                    set_latest=str(set_latest).lower() in {"1", "true", "yes", "on"},
                ) or pkg
            _sync_release_mirrors(root, pkg)
        except HTTPException:
            raise
        except ValueError as exc:
            raise HTTPException(status_code=409, detail=str(exc)) from exc
        except OSError as exc:
            logger.exception("failed to store release package")
            raise HTTPException(status_code=500, detail="failed to store package") from exc
        finally:
            _best_effort_unlink(staged_file)
            if not metadata_saved:
                if backup_file and backup_file.exists():
                    dest_file.unlink(missing_ok=True)
                    os.replace(backup_file, dest_file)
                elif installed:
                    dest_file.unlink(missing_ok=True)
            elif backup_file:
                _best_effort_unlink(backup_file)
            if metadata_saved and previous_file and previous_file != dest_file.resolve():
                _best_effort_unlink(previous_file)
            await file.close()

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
        auth = authenticate(request)
        require_admin(auth)
        return store.list_packages(
            artifact=artifact,
            channel=channel,
            platform=platform,
            status=status,
        )

    @router.get("/public/packages", response_model=list[PublicReleasePackage])
    def list_public_packages() -> list[PublicReleasePackage]:
        root = _require_releases_dir()
        packages = store.list_packages(artifact=DEFAULT_ARTIFACT, status="published")
        public_packages = []
        for pkg in packages:
            try:
                _verify_package_file(root, pkg)
            except HTTPException:
                logger.warning("hiding unavailable public release %s@%s", pkg.platform, pkg.version)
                continue
            public_packages.append(PublicReleasePackage.model_validate(pkg.model_dump()))
        return public_packages

    @router.get("/public/packages/{channel}/{platform}/{version}/download")
    def download_public_package(channel: str, platform: str, version: str) -> Response:
        try:
            validate_slug(channel, "channel")
            validate_slug(platform, "platform")
            validate_slug(version, "version")
        except ValueError as exc:
            raise HTTPException(status_code=404, detail="not found") from exc
        pkg = store.get(DEFAULT_ARTIFACT, channel, platform, version)
        if not pkg or pkg.status != "published":
            raise HTTPException(status_code=404, detail="not found")
        return _file_response(pkg)

    @router.get("/public/latest/{channel}/{platform}/download")
    def download_public_latest(channel: str, platform: str) -> Response:
        try:
            validate_slug(channel, "channel")
            validate_slug(platform, "platform")
        except ValueError as exc:
            raise HTTPException(status_code=404, detail="not found") from exc
        pkg = store.get_latest(DEFAULT_ARTIFACT, channel, platform)
        if not pkg:
            raise HTTPException(status_code=404, detail="latest not found")
        return _file_response(pkg)

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
        draft = store.get(artifact, channel, platform, version)
        if not draft:
            raise HTTPException(status_code=404, detail="not found")
        _verify_package_file(root, draft, verify_sha256=True)
        set_latest = bool(body.set_latest) if body else False
        pkg = store.publish(artifact, channel, platform, version, now=now, set_latest=set_latest)
        if not pkg:
            raise HTTPException(status_code=404, detail="not found")
        _sync_release_mirrors(root, pkg)
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
        current = store.get(artifact, channel, platform, version)
        if not current or current.status != "published":
            raise HTTPException(status_code=404, detail="not found or not published")
        _verify_package_file(root, current, verify_sha256=True)
        pkg = store.promote(artifact, channel, platform, version, now=now)
        if not pkg:
            raise HTTPException(status_code=404, detail="not found or not published")
        _sync_release_mirrors(root, pkg)
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
        try:
            remove_package_tree(root, pkg)
        except OSError:
            # The database is authoritative; a failed disk cleanup leaves an
            # inaccessible orphan that can be removed during storage maintenance.
            logger.exception("failed to remove release files for %s@%s", platform, version)
        audit.record(
            actor=auth.token_id,
            action="release.delete",
            target_agent_id=f"{artifact}@{version}:{platform}",
        )
        return {"deleted": True}

    def _file_response(pkg: ReleasePackage) -> Response:
        root = _require_releases_dir()
        path = _verify_package_file(root, pkg)
        headers = {
            "X-Release-Version": pkg.version,
            "X-Release-Sha256": pkg.sha256,
        }
        return FileResponse(path, media_type=pkg.content_type, filename=pkg.filename, headers=headers)

    def _sync_release_mirrors(root: Path, pkg: ReleasePackage) -> None:
        """Best-effort sidecar sync; package metadata in the store is authoritative."""
        dest_dir = version_dir(root, pkg.artifact, pkg.channel, pkg.platform, pkg.version)
        try:
            write_manifest(dest_dir, pkg)
        except OSError:
            logger.exception("failed to write release manifest for %s@%s", pkg.platform, pkg.version)
        if pkg.is_latest:
            try:
                write_latest_pointer(root, pkg.artifact, pkg.channel, pkg.platform, pkg.version, pkg.filename)
            except OSError:
                logger.exception("failed to write latest pointer for %s@%s", pkg.platform, pkg.version)

    def _best_effort_unlink(path: Path) -> None:
        try:
            path.unlink(missing_ok=True)
        except OSError:
            logger.warning("failed to remove temporary release file %s", path, exc_info=True)

    def _verify_package_file(
        root: Path,
        pkg: ReleasePackage,
        *,
        verify_sha256: bool = False,
    ) -> Path:
        try:
            filename = safe_release_filename(pkg.filename)
            root_path = root.resolve()
            path = package_file_path(root, pkg).resolve()
            if filename != pkg.filename or path.name != filename or not path.is_relative_to(root_path):
                raise ValueError("invalid package path")
            stat = path.stat()
        except (OSError, ValueError):
            raise HTTPException(status_code=409, detail="package file is missing or invalid") from None
        if not path.is_file() or stat.st_size != pkg.size_bytes:
            raise HTTPException(status_code=409, detail="package file is missing or invalid")
        if verify_sha256:
            digest = hashlib.sha256()
            with path.open("rb") as source:
                for chunk in iter(lambda: source.read(1024 * 1024), b""):
                    digest.update(chunk)
            if digest.hexdigest() != pkg.sha256:
                raise HTTPException(status_code=409, detail="package file checksum mismatch")
        return path

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
        result = store.check(
            current=current,
            platform=platform,
            channel=channel,
            artifact=artifact,
        )
        latest = store.get_latest(artifact, channel, platform)
        if latest and result.get("upgrade_available"):
            _verify_package_file(_require_releases_dir(), latest)
        return ReleaseCheckResponse(**result)

    return router
