"""External tool package pydantic models."""

from __future__ import annotations

from pydantic import BaseModel, Field

_SLUG = r"^[A-Za-z0-9._-]+$"


class ExternalToolPackageCreate(BaseModel):
    tool_id: str = Field(min_length=1, max_length=128, pattern=_SLUG)
    version: str = Field(min_length=1, max_length=64, pattern=_SLUG)
    name: str = Field(min_length=1)
    description: str = ""
    platform: str = "any"
    owner: str = ""
    team: str = ""
    risk_level: str = "low"
    blob_id: str = ""


class ExternalToolPackage(ExternalToolPackageCreate):
    status: str = "draft"
    created_at: int
    updated_at: int
    catalog_seq: int = 0
