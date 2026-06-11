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
        legacy = os.environ.get("MANAGE_LEGACY_DIRECT_RELAY", "").strip().lower() in {"1", "true", "yes"}
        return cls(
            host=host,
            port=port,
            db_path=Path(db_raw) if db_raw else None,
            blob_dir=Path(blob_raw) if blob_raw else None,
            blob_max_bytes=_env_optional_int("MANAGE_BLOB_MAX_BYTES"),
            offline_grace_seconds=_env_int("MANAGE_OFFLINE_GRACE_SECONDS", 86400),
            audit_max_entries=_env_int("MANAGE_AUDIT_MAX_ENTRIES", 500),
            legacy_direct_relay=legacy,
            a2a_inbox_content_max_chars=_env_int("MANAGE_A2A_INBOX_CONTENT_MAX_CHARS", 4096),
            a2a_expire_sweep_seconds=_env_int("MANAGE_A2A_EXPIRE_SWEEP_SECONDS", 30),
        )
