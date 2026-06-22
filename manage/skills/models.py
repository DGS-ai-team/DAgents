"""Skill package pydantic models."""

from __future__ import annotations

from pydantic import BaseModel, Field


_SLUG = r"^[A-Za-z0-9._-]+$"  # URL 路径段安全：字母数字与 . _ -


class SkillPackageCreate(BaseModel):
    skill_id: str = Field(min_length=1, max_length=128, pattern=_SLUG)
    version: str = Field(min_length=1, max_length=64, pattern=_SLUG)
    name: str = Field(min_length=1)
    description: str = ""
    owner: str = ""
    team: str = ""
    risk_level: str = "low"
    required_tools: list[str] = Field(default_factory=list)
    required_scopes: list[str] = Field(default_factory=list)
    blob_id: str = ""


class SkillPackage(SkillPackageCreate):
    status: str = "draft"
    created_at: int
    updated_at: int
    catalog_seq: int = 0  # publish sequence number, 0 = unpublished
