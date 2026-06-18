"""Manage Blob 存储（M0 占位；M2 A2A / M3 Skills 使用）。"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

from manage.config import ManageSettings


@dataclass(frozen=True)
class BlobStoreConfig:
    root: Path | None
    max_bytes: int | None

    @classmethod
    def from_settings(cls, settings: ManageSettings) -> "BlobStoreConfig":
        return cls(root=settings.blob_dir, max_bytes=settings.blob_max_bytes)


class BlobStore:
    """M0：仅暴露配置状态；上传/下载在 M2 实现。"""

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
