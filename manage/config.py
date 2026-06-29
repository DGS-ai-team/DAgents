"""Manage 服务配置（环境变量）。"""

from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path


def _env_int(name: str, default: int) -> int:
    raw = os.environ.get(name, "").strip()
    if not raw:
        return default
    try:
        return int(raw)
    except ValueError:
        return default


def _env_optional_int(name: str) -> int | None:
    raw = os.environ.get(name, "").strip()
    if not raw:
        return None
    try:
        return max(0, int(raw))
    except ValueError:
        return None


@dataclass(frozen=True)
class ManageSettings:
    host: str
    port: int
    db_path: Path | None
    blob_dir: Path | None
    blob_max_bytes: int | None
    releases_dir: Path | None
    release_max_bytes: int | None
    seed_bundled_releases: bool
    bundled_releases_dir: Path | None
    offline_grace_seconds: int
    audit_max_entries: int
    legacy_direct_relay: bool
    a2a_inbox_content_max_chars: int
    a2a_expire_sweep_seconds: int

    @classmethod
    def from_env(cls) -> "ManageSettings":
        host = os.environ.get("MANAGE_HOST", "0.0.0.0").strip() or "0.0.0.0"
        port = _env_int("MANAGE_PORT", 8020)
        db_raw = os.environ.get("MANAGE_DB_PATH", "").strip()
        blob_raw = os.environ.get("MANAGE_BLOB_DIR", "").strip()
        releases_raw = os.environ.get("MANAGE_RELEASES_DIR", "").strip()
        bundled_raw = os.environ.get("MANAGE_BUNDLED_RELEASES_DIR", "").strip()
        legacy = os.environ.get("MANAGE_LEGACY_DIRECT_RELAY", "").strip().lower() in {"1", "true", "yes"}
        seed = os.environ.get("MANAGE_SEED_BUNDLED_RELEASES", "1").strip().lower() not in {"0", "false", "no"}
        db_path = Path(db_raw) if db_raw else None
        releases_dir: Path | None
        if releases_raw:
            releases_dir = Path(releases_raw)
        elif db_path is not None:
            releases_dir = db_path.parent / "releases"
        else:
            releases_dir = None
        return cls(
            host=host,
            port=port,
            db_path=db_path,
            blob_dir=Path(blob_raw) if blob_raw else None,
            blob_max_bytes=_env_optional_int("MANAGE_BLOB_MAX_BYTES"),
            releases_dir=releases_dir,
            release_max_bytes=_env_optional_int("MANAGE_RELEASE_MAX_BYTES") or 536_870_912,
            seed_bundled_releases=seed,
            bundled_releases_dir=Path(bundled_raw) if bundled_raw else Path("/app/bundled/releases"),
            offline_grace_seconds=_env_int("MANAGE_OFFLINE_GRACE_SECONDS", 86400),
            audit_max_entries=_env_int("MANAGE_AUDIT_MAX_ENTRIES", 500),
            legacy_direct_relay=legacy,
            a2a_inbox_content_max_chars=_env_int("MANAGE_A2A_INBOX_CONTENT_MAX_CHARS", 4096),
            a2a_expire_sweep_seconds=_env_int("MANAGE_A2A_EXPIRE_SWEEP_SECONDS", 30),
        )

    @classmethod
    def for_test(
        cls,
        *,
        host: str = "127.0.0.1",
        port: int = 8020,
        db_path: Path | None = None,
        blob_dir: Path | None = None,
        blob_max_bytes: int | None = None,
        releases_dir: Path | None = None,
        release_max_bytes: int | None = 536_870_912,
        seed_bundled_releases: bool = False,
        bundled_releases_dir: Path | None = Path("/app/bundled/releases"),
        offline_grace_seconds: int = 86400,
        audit_max_entries: int = 100,
        legacy_direct_relay: bool = False,
        a2a_inbox_content_max_chars: int = 4096,
        a2a_expire_sweep_seconds: int = 30,
    ) -> "ManageSettings":
        return cls(
            host=host,
            port=port,
            db_path=db_path,
            blob_dir=blob_dir,
            blob_max_bytes=blob_max_bytes,
            releases_dir=releases_dir,
            release_max_bytes=release_max_bytes,
            seed_bundled_releases=seed_bundled_releases,
            bundled_releases_dir=bundled_releases_dir,
            offline_grace_seconds=offline_grace_seconds,
            audit_max_entries=audit_max_entries,
            legacy_direct_relay=legacy_direct_relay,
            a2a_inbox_content_max_chars=a2a_inbox_content_max_chars,
            a2a_expire_sweep_seconds=a2a_expire_sweep_seconds,
        )
