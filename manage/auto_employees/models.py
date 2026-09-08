from __future__ import annotations

from datetime import datetime
from typing import Any

from pydantic import BaseModel, Field, field_validator
from datetime import timedelta, timezone
import math


def _text(value: Any) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("must be a string")
    return value.strip()


class AutoSummary(BaseModel):
    model_config = {"extra": "forbid"}
    agent_id: str = Field(min_length=1, max_length=256)
    display_name: str = Field(default="", max_length=256)
    role: str = Field(default="", max_length=2048)
    state: str = Field(min_length=1, max_length=64)
    reason: str = Field(default="", max_length=2048)
    last_result: str = Field(default="", max_length=4096)
    next_at: datetime | None = None
    usage: dict[str, int | float | bool | str] = Field(default_factory=dict)
    as_of: datetime

    _trim_text = field_validator("agent_id", "display_name", "role", "state", "reason", "last_result", mode="before")(_text)

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

    @field_validator("usage", mode="before")
    @classmethod
    def validate_usage(cls, value: Any) -> dict[str, int | float | bool | str]:
        if value is None:
            return {}
        if not isinstance(value, dict) or len(value) > 16:
            raise ValueError("usage must be a small object")
        out: dict[str, int | float | bool | str] = {}
        for key, item in value.items():
            if not isinstance(key, str) or len(key) > 64 or not isinstance(item, (int, float, bool, str)):
                raise ValueError("usage contains an invalid value")
            if isinstance(item, float) and (not math.isfinite(item) or item < 0):
                raise ValueError("usage contains an invalid number")
            if isinstance(item, int) and not isinstance(item, bool) and item < 0:
                raise ValueError("usage contains an invalid number")
            if isinstance(item, str) and len(item) > 256:
                raise ValueError("usage contains an oversized string")
            out[key] = item
        return out


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
