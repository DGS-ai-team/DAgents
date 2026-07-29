"""Placement 控制面模型。"""

from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field, field_validator


class ControlCreateAgentRequest(BaseModel):
    """owner Node 请求在 home Node 上创建 Agent。"""

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


class ControlHostInfo(BaseModel):
    os_kind: str = ""
    sys_platform: str = ""
    machine: str = ""
    display_available: bool = False
    display_label: str = ""


class ControlCreateAgentResponse(BaseModel):
    agent_id: str
    display_name: str
    home_node_id: str
    owner_node_id: str
    origin: str = "remote"
    host: ControlHostInfo = Field(default_factory=ControlHostInfo)
    config_snapshot: dict[str, Any] | str | None = None
    sandbox_enabled: bool = False
    sandbox_backend: str = "process"
    created_at: str = ""
    updated_at: str = ""


class ControlDeleteAgentResponse(BaseModel):
    ok: bool = True
    agent_id: str
    home_node_id: str
    home_deleted: bool = False


class PeerNodeView(BaseModel):
    node_id: str
    name: str = ""
    status: str = "offline"
    discovery_group: list[str] = Field(default_factory=list)
    version: str = ""
    host: ControlHostInfo = Field(default_factory=ControlHostInfo)
    allow_peer_create: bool = True
    allow_screen_view: bool = True


class PeerNodesResponse(BaseModel):
    nodes: list[PeerNodeView]
    self_node_id: str = ""
