"""Placement 控制面模型（D5：create/delete/peers 已 410；保留 create 请求体校验）。"""

from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field, field_validator


class ControlCreateAgentRequest(BaseModel):
    """遗留请求体：路由恒返回 410，仍需可解析旧客户端 payload。"""

    owner_node_id: str = Field(min_length=1, max_length=256)
    display_name: str = Field(min_length=1, max_length=256)
    template_id: str = Field(default="", max_length=256)
    defaults: dict[str, Any] = Field(default_factory=dict)
    sandbox: dict[str, Any] | None = None
    origin: str = Field(default="local")

    @field_validator("owner_node_id", "display_name", "template_id", "origin", mode="before")
    @classmethod
    def strip_text(cls, value: Any) -> str:
        if value is None:
            return ""
        if not isinstance(value, str):
            raise ValueError("字段必须是字符串")
        return value.strip()
