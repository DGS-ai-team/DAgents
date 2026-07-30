"""Timeline / Outbox / HITL 轻量模型（D3）。"""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, Field

_WG = r"^wg_[0-9a-z]{26}$"
_EV = r"^ev_[0-9a-z]{26}$"
_HT = r"^ht_[0-9a-z]{26}$"


class TimelineEvent(BaseModel):
    event_id: str = Field(pattern=_EV)
    workgroup_id: str = Field(pattern=_WG)
    seq: int = Field(ge=1)
    type: Literal["human_message", "actor_final_text", "system_notice"]
    actor_id: str
    text: str = ""
    created_at: str
    # 禁止原始工具载荷字段出现在公开 Timeline
    client_message_id: str | None = None


class OutboxFrame(BaseModel):
    delivery_seq: int = Field(ge=1)
    workgroup_id: str
    type: str
    payload: dict[str, Any] = Field(default_factory=dict)
    created_at: str
    acked: bool = False


class HITLRequest(BaseModel):
    hitl_id: str = Field(pattern=_HT)
    workgroup_id: str = Field(pattern=_WG)
    kind: Literal["information"] = "information"
    prompt: str
    status: Literal["pending", "resolved"] = "pending"
    created_at: str
    resolution: dict[str, Any] | None = None
    resolved_at: str | None = None


class HumanPostRequest(BaseModel):
    text: str = Field(min_length=1)
    client_message_id: str | None = None
    from_node_id: str = Field(min_length=1)


class ProvisionCompleteRequest(BaseModel):
    member_id: str
    provision_id: str
    workspace_path: str = ""
    tool_catalog_revision: str = ""
    status: Literal["ready", "error"] = "ready"


class ToolResultApplyRequest(BaseModel):
    command_id: str
    assign_id: str
    member_id: str
    status: Literal["succeeded", "failed", "indeterminate", "rejected"]
    result_text: str = ""
    error_code: str | None = None


class MemberFinalRequest(BaseModel):
    assign_id: str
    member_id: str
    text: str = Field(min_length=1)


class HITLCreateRequest(BaseModel):
    prompt: str = Field(min_length=1)


class HITLResolveRequest(BaseModel):
    resolution: dict[str, Any] = Field(default_factory=dict)
