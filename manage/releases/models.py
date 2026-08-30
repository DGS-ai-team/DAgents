"""Release 安装包 pydantic 模型。"""

from __future__ import annotations

from pydantic import BaseModel, Field

_SLUG = r"^[A-Za-z0-9._-]+$"

DEFAULT_ARTIFACT = "dagents-local-assistant"
DEFAULT_CHANNEL = "stable"

PLATFORMS = frozenset({"linux-amd64", "linux-arm64", "windows-amd64", "windows-386"})


class ReleasePublishBody(BaseModel):
    set_latest: bool = False


class ReleasePackageCreate(BaseModel):
    artifact: str = Field(default=DEFAULT_ARTIFACT, min_length=1, max_length=128, pattern=_SLUG)
    version: str = Field(min_length=1, max_length=64, pattern=_SLUG)
    platform: str = Field(min_length=1, max_length=64, pattern=_SLUG)
    channel: str = Field(default=DEFAULT_CHANNEL, min_length=1, max_length=64, pattern=_SLUG)
    filename: str = Field(min_length=1, max_length=256)
    sha256: str = Field(min_length=64, max_length=64)
    size_bytes: int = Field(ge=0)
    content_type: str = "application/octet-stream"
    release_notes: str = ""
    uploaded_by: str = ""
    rel_path: str = ""
    source: str = "upload"


class ReleasePackage(ReleasePackageCreate):
    status: str = "draft"
    is_latest: bool = False
    created_at: int
    updated_at: int


class ReleaseCheckResponse(BaseModel):
    artifact: str
    platform: str
    channel: str
    current: str
    latest: str
    upgrade_available: bool
    published_at: int | None = None
    release_notes_url: str = ""
    release_notes: str = ""
    asset: dict | None = None


class ReleaseUploadResponse(ReleasePackage):
    download_url: str = ""
