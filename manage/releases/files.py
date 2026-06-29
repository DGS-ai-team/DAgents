"""Release 安装包磁盘布局与 manifest。"""

from __future__ import annotations

import json
import re
import shutil
from pathlib import Path

from manage.releases.models import ReleasePackage

_SLUG_RE = re.compile(r"^[A-Za-z0-9._-]+$")


def validate_slug(value: str, field: str) -> str:
    text = str(value or "").strip()
    if not text or not _SLUG_RE.match(text):
        raise ValueError(f"invalid {field}")
    return text


def version_dir(
    releases_dir: Path,
    artifact: str,
    channel: str,
    platform: str,
    version: str,
) -> Path:
    artifact = validate_slug(artifact, "artifact")
    channel = validate_slug(channel, "channel")
    platform = validate_slug(platform, "platform")
    version = validate_slug(version, "version")
    return releases_dir / artifact / channel / platform / version


def rel_path_for(artifact: str, channel: str, platform: str, version: str, filename: str) -> str:
    return str(
        Path(artifact) / channel / platform / version / filename
    ).replace("\\", "/")


def write_manifest(version_path: Path, pkg: ReleasePackage) -> None:
    version_path.mkdir(parents=True, exist_ok=True)
    manifest = {
        "artifact": pkg.artifact,
        "version": pkg.version,
        "channel": pkg.channel,
        "platform": pkg.platform,
        "filename": pkg.filename,
        "sha256": pkg.sha256,
        "size_bytes": pkg.size_bytes,
        "content_type": pkg.content_type,
        "status": pkg.status,
        "is_latest": pkg.is_latest,
        "source": pkg.source,
        "release_notes": pkg.release_notes,
        "uploaded_by": pkg.uploaded_by,
        "created_at": pkg.created_at,
        "updated_at": pkg.updated_at,
    }
    (version_path / "manifest.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )


def write_latest_pointer(
    releases_dir: Path,
    artifact: str,
    channel: str,
    platform: str,
    version: str,
    filename: str,
) -> None:
    platform_dir = version_dir(releases_dir, artifact, channel, platform, version).parent
    platform_dir.mkdir(parents=True, exist_ok=True)
    payload = {
        "artifact": artifact,
        "channel": channel,
        "platform": platform,
        "version": version,
        "filename": filename,
        "path": rel_path_for(artifact, channel, platform, version, filename),
    }
    (platform_dir / "latest.json").write_text(
        json.dumps(payload, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )


def package_file_path(releases_dir: Path, pkg: ReleasePackage) -> Path:
    if pkg.rel_path:
        return releases_dir / pkg.rel_path
    return version_dir(releases_dir, pkg.artifact, pkg.channel, pkg.platform, pkg.version) / pkg.filename


def remove_package_tree(releases_dir: Path, pkg: ReleasePackage) -> None:
    tree = version_dir(releases_dir, pkg.artifact, pkg.channel, pkg.platform, pkg.version)
    if tree.exists():
        shutil.rmtree(tree)


def allowed_release_filename(name: str) -> bool:
    lower = str(name or "").lower()
    return lower.endswith(".tar.gz") or lower.endswith(".zip")


def content_type_for_filename(name: str) -> str:
    lower = str(name or "").lower()
    if lower.endswith(".zip"):
        return "application/zip"
    if lower.endswith(".tar.gz"):
        return "application/gzip"
    return "application/octet-stream"
