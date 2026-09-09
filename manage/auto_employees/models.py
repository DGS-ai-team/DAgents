from __future__ import annotations

from datetime import datetime
from typing import Any

from pydantic import BaseModel, Field, field_validator
from datetime import timedelta, timezone


def _text(value: Any) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("must be a string")
    return value.strip()


class DreamingSummary(BaseModel):
    model_config = {"extra": "forbid"}
    state: str = Field(default="unknown", min_length=1, max_length=64)
    next_at: datetime | None = None
    last_success: datetime | None = None

    _trim_state = field_validator("state", mode="before")(_text)

    @field_validator("next_at", "last_success")
    @classmethod
    def validate_timestamp(cls, value: datetime | None) -> datetime | None:
        if value is None:
            return None
        if value.tzinfo is None or value.utcoffset() is None:
            raise ValueError("timestamp must include a timezone offset")
        return value.astimezone(timezone.utc)


class AutoSummary(BaseModel):
    model_config = {"extra": "forbid"}
    agent_id: str = Field(min_length=1, max_length=256)
    display_name: str = Field(default="", max_length=256)
    role: str = Field(default="", max_length=128)
    wake_interval_seconds: int = Field(default=0, ge=0)
    todo_counts: dict[str, int] = Field(default_factory=dict)
    state: str = Field(min_length=1, max_length=64)
    reason: str = Field(default="", max_length=2048)
    next_at: datetime | None = None
    profile_revision: int = Field(default=0, ge=0)
    runtime_revision: int = Field(default=0, ge=0)
    as_of: datetime
    dreaming: DreamingSummary = Field(default_factory=DreamingSummary)

    _trim_text = field_validator("agent_id", "display_name", "role", "state", "reason", mode="before")(_text)

    @field_validator("as_of", "next_at")
    @classmethod
    def validate_timestamp(cls, value: datetime | None, info) -> datetime | None:
        if value is None:
            return None
        if value.tzinfo is None or value.utcoffset() is None:
            raise ValueError("timestamp must include a timezone offset")
        value = value.astimezone(timezone.utc)
        if info.field_name == "as_of" and value > datetime.now(timezone.utc) + timedelta(minutes=5):
            raise ValueError("as_of cannot be far in the future")
        return value


class AutoSummaryRecord(AutoSummary):
    model_config = {"extra": "forbid"}
    node_id: str = Field(min_length=1, max_length=256)
    received_at: datetime
    stale: bool = False
    age_seconds: int = Field(default=0, ge=0)


class AutoSummaryList(BaseModel):
    items: list[AutoSummaryRecord]
    total: int
    page: int
    page_size: int
    as_of: datetime | None = None
    server_time: datetime
