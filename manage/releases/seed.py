"""启动时从 bundled 目录 seed 安装包到 MANAGE_RELEASES_DIR。"""

from __future__ import annotations

import hashlib
import json
import logging
import shutil
import time
from pathlib import Path

from manage.releases.files import (
    content_type_for_filename,
    rel_path_for,
    validate_slug,
    version_dir,
    write_latest_pointer,
    write_manifest,
)
from manage.releases.models import DEFAULT_ARTIFACT, ReleasePackageCreate
from manage.releases.store import ReleasePackageStore

logger = logging.getLogger(__name__)


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as fh:
        while True:
            chunk = fh.read(1024 * 1024)
            if not chunk:
                break
            digest.update(chunk)
    return digest.hexdigest()


def _load_manifest(path: Path) -> dict | None:
    manifest_path = path / "manifest.json"
    if not manifest_path.is_file():
        return None
    try:
        return json.loads(manifest_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        return None


def _find_package_file(version_path: Path, manifest: dict | None) -> Path | None:
    if manifest:
        candidate = version_path / str(manifest.get("filename") or "")
        if candidate.is_file():
            return candidate
    for pattern in ("*.tar.gz", "*.zip"):
        matches = sorted(version_path.glob(pattern))
        if matches:
            return matches[0]
    return None


def seed_bundled_releases(
    *,
    bundled_dir: Path | None,
    releases_dir: Path | None,
    store: ReleasePackageStore,
    enabled: bool = True,
    set_latest: bool = True,
) -> int:
    """Import bundled release trees; returns count imported."""
    if not enabled or bundled_dir is None or releases_dir is None:
        return 0
    if not bundled_dir.is_dir():
        return 0
    releases_dir.mkdir(parents=True, exist_ok=True)
    imported = 0
    artifact_root = bundled_dir / DEFAULT_ARTIFACT
    if not artifact_root.is_dir():
        return 0
    for channel_dir in sorted(artifact_root.iterdir()):
        if not channel_dir.is_dir():
            continue
        channel = validate_slug(channel_dir.name, "channel")
        for platform_dir in sorted(channel_dir.iterdir()):
            if not platform_dir.is_dir() or platform_dir.name == "latest.json":
                continue
            platform = validate_slug(platform_dir.name, "platform")
            for version_path in sorted(platform_dir.iterdir()):
                if not version_path.is_dir():
                    continue
                version = validate_slug(version_path.name, "version")
                if store.get(DEFAULT_ARTIFACT, channel, platform, version):
                    continue
                manifest = _load_manifest(version_path)
                package_file = _find_package_file(version_path, manifest)
                if package_file is None:
                    logger.warning("seed skip missing package file: %s", version_path)
                    continue
                filename = package_file.name
                rel = rel_path_for(DEFAULT_ARTIFACT, channel, platform, version, filename)
                dest_dir = version_dir(releases_dir, DEFAULT_ARTIFACT, channel, platform, version)
                dest_dir.mkdir(parents=True, exist_ok=True)
                dest_file = dest_dir / filename
                if not dest_file.exists():
                    shutil.copy2(package_file, dest_file)
                sha256 = _sha256_file(dest_file)
                now = int(time.time())
                notes = str(manifest.get("release_notes") if manifest else "") or ""
                payload = ReleasePackageCreate(
                    artifact=DEFAULT_ARTIFACT,
                    version=version,
                    platform=platform,
                    channel=channel,
                    filename=filename,
                    sha256=sha256,
                    size_bytes=dest_file.stat().st_size,
                    content_type=content_type_for_filename(filename),
                    release_notes=notes,
                    uploaded_by="bundled_seed",
                    rel_path=rel,
                    source="bundled_seed",
                )
                store.create_draft(payload, now=now)
                published = store.publish(
                    DEFAULT_ARTIFACT,
                    channel,
                    platform,
                    version,
                    now=now,
                    set_latest=set_latest and not store.get_latest(DEFAULT_ARTIFACT, channel, platform),
                )
                if published:
                    write_manifest(dest_dir, published)
                    if published.is_latest:
                        write_latest_pointer(
                            releases_dir,
                            DEFAULT_ARTIFACT,
                            channel,
                            platform,
                            version,
                            filename,
                        )
                    imported += 1
                    logger.info(
                        "seeded release %s/%s/%s@%s latest=%s",
                        DEFAULT_ARTIFACT,
                        channel,
                        platform,
                        version,
                        published.is_latest,
                    )
    return imported
