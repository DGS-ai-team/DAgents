"""Release 安装包磁盘布局与 manifest。"""

from __future__ import annotations

import json
import os
import re
import shutil
import tempfile
from pathlib import Path
from pathlib import PureWindowsPath

from manage.releases.models import ReleasePackage

_SLUG_RE = re.compile(r"^[A-Za-z0-9._-]+$")


def validate_slug(value: str, field: str) -> str:
    text = str(value or "").strip()
    maximum = {"artifact": 128, "version": 64, "platform": 64, "channel": 64}.get(field, 128)
    if not text or text in {".", ".."} or len(text) > maximum or not _SLUG_RE.match(text):
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
        "published_at": pkg.published_at,
        "source": pkg.source,
        "release_notes": pkg.release_notes,
        "uploaded_by": pkg.uploaded_by,
        "created_at": pkg.created_at,
        "updated_at": pkg.updated_at,
    }
    _write_json_atomic(version_path / "manifest.json", manifest)


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
    _write_json_atomic(platform_dir / "latest.json", payload)


def _write_json_atomic(path: Path, payload: dict) -> None:
    """Replace small release metadata files atomically to avoid partial JSON."""
    temporary: Path | None = None
    try:
        with tempfile.NamedTemporaryFile(
            mode="w",
            encoding="utf-8",
            newline="\n",
            dir=path.parent,
            prefix=f".{path.name}.",
            suffix=".tmp",
            delete=False,
        ) as stream:
            temporary = Path(stream.name)
            stream.write(json.dumps(payload, ensure_ascii=False, indent=2) + "\n")
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)


def package_file_path(releases_dir: Path, pkg: ReleasePackage) -> Path:
    if pkg.rel_path:
        return releases_dir / pkg.rel_path
    return version_dir(releases_dir, pkg.artifact, pkg.channel, pkg.platform, pkg.version) / pkg.filename


def remove_package_tree(releases_dir: Path, pkg: ReleasePackage) -> None:
    tree = version_dir(releases_dir, pkg.artifact, pkg.channel, pkg.platform, pkg.version)
    if tree.exists():
        shutil.rmtree(tree)


def allowed_release_filename(name: str) -> bool:
    try:
        safe_release_filename(name)
    except ValueError:
        return False
    return True


def safe_release_filename(name: str) -> str:
    """Validate an uploaded filename as a single safe path component."""
    value = str(name or "").strip()
    if (
        not value
        or value in {".", ".."}
        or len(value.encode("utf-8")) > 255
        or value != name
        or Path(value).name != value
        or PureWindowsPath(value).name != value
        or PureWindowsPath(value).drive
        or any(char in '<>:"|?*' for char in value)
        or any(ord(char) < 32 or ord(char) == 127 for char in value)
        or value.endswith((".", " "))
    ):
        raise ValueError("invalid package filename")
    lower = value.lower()
    if not (lower.endswith(".tar.gz") or lower.endswith(".zip") or lower.endswith(".exe")):
        raise ValueError("file must be .tar.gz, .zip, or .exe")
    return value


def content_type_for_filename(name: str) -> str:
    lower = str(name or "").lower()
    if lower.endswith(".zip"):
        return "application/zip"
    if lower.endswith(".tar.gz"):
        return "application/gzip"
    return "application/octet-stream"
