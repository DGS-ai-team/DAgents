"""案例库 pydantic 模型。"""

from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field

_SLUG = r"^[A-Za-z0-9._-]+$"


class CaseMessage(BaseModel):
    """单条会话消息（对齐 Node history JSONL 的 message 字段）。"""

    id: str = Field(min_length=1, max_length=64)
    recorded_at: str = ""
    role: str = "user"
    content: str = ""
    raw: dict[str, Any] | None = None


class CaseResources(BaseModel):
    """案例关联的外部资源。"""

    skill_ids: list[str] = Field(default_factory=list)
    plugin_ids: list[str] = Field(default_factory=list)
    externaltool_ids: list[str] = Field(default_factory=list)


class CaseCreate(BaseModel):
    case_id: str = Field(min_length=1, max_length=128, pattern=_SLUG)
    name: str = Field(min_length=1, max_length=256)
    description: str = ""
    resources: CaseResources = Field(default_factory=CaseResources)


class CaseExample(CaseCreate):
    messages: list[CaseMessage] = Field(default_factory=list)
    created_at: int
    updated_at: int


class CaseMetadataPatch(BaseModel):
    name: str | None = None
    description: str | None = None
    resources: CaseResources | None = None


class CaseMessageInsert(BaseModel):
    index: int | None = None
    message: CaseMessage


class CaseMessagesReplace(BaseModel):
    messages: list[CaseMessage]
