"""Edge Tunnel 模型。"""

from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field, field_validator


class EdgeSessionCreateRequest(BaseModel):
    home_node_id: str = Field(min_length=1, max_length=256)
    agent_id: str = Field(min_length=1, max_length=256)
    scopes: list[str] = Field(default_factory=lambda: ["agent", "messages", "streams", "screen:view"])
    ttl_seconds: int = Field(default=3600, ge=60, le=86400)

    @field_validator("home_node_id", "agent_id", mode="before")
    @classmethod
    def strip_text(cls, value: Any) -> str:
        if value is None:
            return ""
        if not isinstance(value, str):
            raise ValueError("字段必须是字符串")
        return value.strip()

    @field_validator("scopes", mode="before")
    @classmethod
    def normalize_scopes(cls, value: Any) -> list[str]:
        if value is None:
            return ["agent", "messages", "streams", "screen:view"]
        if not isinstance(value, list):
            raise ValueError("scopes 必须是字符串列表")
        out: list[str] = []
        for item in value:
            s = str(item or "").strip().lower()
            if s and s not in out:
                out.append(s)
        return out or ["agent", "messages", "streams", "screen:view"]


class EdgeSessionResponse(BaseModel):
    edge_session_id: str
    home_node_id: str
    agent_id: str
    owner_node_id: str
    scopes: list[str]
    expires_at: str
    proxy_prefix: str
