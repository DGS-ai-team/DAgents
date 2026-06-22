"""Manage Blob 存储（M0 占位；M2 A2A / M3 Skills 使用）。"""

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from pathlib import Path

from fastapi import HTTPException

from manage.config import ManageSettings


@dataclass(frozen=True)
class BlobStoreConfig:
    root: Path | None
    max_bytes: int | None

    @classmethod
    def from_settings(cls, settings: ManageSettings) -> "BlobStoreConfig":
        return cls(root=settings.blob_dir, max_bytes=settings.blob_max_bytes)


class BlobStore:
    """内容寻址 Blob 存储；M2(A2A) / M3(Skills) 使用。"""

    def __init__(self, config: BlobStoreConfig) -> None:
        self.config = config
        if config.root is not None:
            config.root.mkdir(parents=True, exist_ok=True)

    @property
    def enabled(self) -> bool:
        return self.config.root is not None

    def status(self) -> dict[str, object]:
        return {
            "enabled": self.enabled,
            "max_bytes": self.config.max_bytes,
            "note": "Blob API 在 M2(A2A) / M3(Skills) 实现；单文件上限由 MANAGE_BLOB_MAX_BYTES 配置，未设则不限制。",
        }

    def put(self, data: bytes, content_type: str) -> dict:
        """Store bytes; returns {blob_id, sha256, size, content_type}. blob_id == sha256."""
        if not self.enabled:
            raise HTTPException(status_code=503, detail="blob store not configured")
        if self.config.max_bytes is not None and len(data) > self.config.max_bytes:
            raise HTTPException(status_code=413, detail="payload too large")
        sha256 = hashlib.sha256(data).hexdigest()
        blob_id = sha256
        blob_path = self.config.root / blob_id  # type: ignore[operator]
        sidecar_path = self.config.root / f"{blob_id}.json"  # type: ignore[operator]
        blob_path.write_bytes(data)
        meta = {"sha256": sha256, "size": len(data), "content_type": content_type}
        sidecar_path.write_text(json.dumps(meta), encoding="utf-8")
        return {"blob_id": blob_id, **meta}

    def get(self, blob_id: str) -> tuple[bytes, dict] | None:
        """Return (bytes, meta) or None if not found."""
        if not self.enabled:
            return None
        blob_path = self.config.root / blob_id  # type: ignore[operator]
        sidecar_path = self.config.root / f"{blob_id}.json"  # type: ignore[operator]
        if not blob_path.exists() or not sidecar_path.exists():
            return None
        data = blob_path.read_bytes()
        meta = json.loads(sidecar_path.read_text(encoding="utf-8"))
        return data, meta

    def head(self, blob_id: str) -> dict | None:
        """Return meta dict or None if not found."""
        if not self.enabled:
            return None
        sidecar_path = self.config.root / f"{blob_id}.json"  # type: ignore[operator]
        if not sidecar_path.exists():
            return None
        return json.loads(sidecar_path.read_text(encoding="utf-8"))

    def delete(self, blob_id: str) -> bool:
        """Delete blob and sidecar; return True if something was deleted."""
        if not self.enabled:
            return False
        blob_path = self.config.root / blob_id  # type: ignore[operator]
        sidecar_path = self.config.root / f"{blob_id}.json"  # type: ignore[operator]
        deleted = False
        if blob_path.exists():
            blob_path.unlink()
            deleted = True
        if sidecar_path.exists():
            sidecar_path.unlink()
            deleted = True
        return deleted
