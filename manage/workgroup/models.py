"""Workgroup Pydantic models for the current AgentRef protocol."""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, Field, field_validator

SCHEMA_VERSION = "0.5.0"

_WG = r"^wg_[0-9a-z]{26}$"
_MB = r"^mb_[0-9a-z]{26}$"
_AS = r"^as_[0-9a-z]{26}$"
_RN = r"^rn_[0-9a-z]{26}$"
_SHA = r"^sha256:[0-9a-f]{64}$"


class WorkGroup(BaseModel):
    workgroup_id: str = Field(pattern=_WG)
    schema_version: Literal["0.5.0"] = SCHEMA_VERSION
    display_name: str = Field(min_length=1, max_length=128)
    status: Literal["configuring", "active", "archiving", "archived"]
    created_by_node_id: str = Field(min_length=1, max_length=128)
    llm_profile_id: str = Field(min_length=1)
    llm_profile_revision: str = Field(min_length=1)
    created_at: str
    archived_at: str | None = None


class WorkGroupACL(BaseModel):
    workgroup_id: str = Field(pattern=_WG)
    owners: list[str] = Field(min_length=1)
    collaborators: list[str] = Field(default_factory=list)
    revision: int = Field(ge=1)
    updated_at: str

    @field_validator("owners", "collaborators")
    @classmethod
    def _unique_nonempty(cls, values: list[str]) -> list[str]:
        out: list[str] = []
        seen: set[str] = set()
        for item in values:
            node = str(item or "").strip()
            if not node or node in seen:
                continue
            seen.add(node)
            out.append(node)
        return out


class WorkGroupMember(BaseModel):
    member_id: str = Field(pattern=_MB)
    workgroup_id: str = Field(pattern=_WG)
    agent_id: str = Field(min_length=1, max_length=256)
    session_id: str = Field(min_length=1, max_length=512)
    home_node_id: str = Field(min_length=1, max_length=128)
    display_name: str = Field(min_length=1)
    description: str = Field(default="", max_length=256)
    status: Literal["provisioning", "ready", "busy", "archived", "error"]
    active_assign_id: str | None = Field(default=None, pattern=_AS)
    error_code: str | None = None
    error_message: str | None = None
    created_at: str
    archived_at: str | None = None


class ActorRun(BaseModel):
    run_id: str = Field(pattern=_RN)
    workgroup_id: str = Field(pattern=_WG)
    actor_id: str
    assign_id: str | None = Field(default=None, pattern=_AS)
    status: Literal["running", "awaiting_hitl", "succeeded", "failed", "canceled", "indeterminate"]
    llm_profile_revision: str
    timeline_watermark_seq: int = Field(ge=0, default=0)
    checkpoint_ordinal: int = Field(ge=0, default=0)
    created_at: str


class Assign(BaseModel):
    assign_id: str = Field(pattern=_AS)
    workgroup_id: str = Field(pattern=_WG)
    member_id: str = Field(pattern=_MB)
    leader_run_id: str = Field(pattern=_RN)
    # Optional because a direct @member assignment has no parent tool call.
    leader_tool_call_id: str | None = Field(default=None, min_length=1)
    source: Literal["leader_tool", "direct_member"] = "leader_tool"
    parent_turn_id: str = Field(min_length=1, max_length=128)
    child_turn_id: str = Field(min_length=1, max_length=128)
    attempt_id: str = Field(min_length=1, max_length=128)
    last_event_seq: int = Field(ge=0, default=0)
    event_stream_epoch: str = ""
    updated_at: str
    status: Literal["queued", "running", "awaiting_hitl", "succeeded", "failed", "canceled", "indeterminate"]
    instruction: str
    result_summary: str | None = None
    error_code: str | None = None
    created_at: str


# --- API 入参 ---


class WorkGroupCreateRequest(BaseModel):
    display_name: str = Field(min_length=1, max_length=128)
    created_by_node_id: str = Field(min_length=1, max_length=128)
    llm_profile_id: str = Field(min_length=1, default="default")
    llm_profile_revision: str = Field(min_length=1, default="1")


class WorkGroupPatchRequest(BaseModel):
    display_name: str | None = Field(default=None, min_length=1, max_length=128)
    llm_profile_id: str | None = Field(default=None, min_length=1)
    llm_profile_revision: str | None = Field(default=None, min_length=1)


class ACLPatchRequest(BaseModel):
    owners: list[str] | None = None
    collaborators: list[str] | None = None
    expected_revision: int = Field(ge=1)


class MemberCreateRequest(BaseModel):
    """Bind an existing registered Agent to the workgroup."""

    agent_id: str = Field(min_length=1, max_length=256)
    home_node_id: str = Field(default="", max_length=128)
    display_name: str = Field(min_length=1, max_length=64)
    description: str = Field(default="", max_length=256)


class MemberPatchRequest(BaseModel):
    """Update member presentation metadata only."""

    display_name: str | None = Field(default=None, min_length=1, max_length=64)
    description: str | None = Field(default=None, max_length=256)


class AssignCreateRequest(BaseModel):
    member_id: str = Field(pattern=_MB)
    leader_run_id: str | None = Field(default=None, pattern=_RN)
    leader_tool_call_id: str | None = Field(default=None, min_length=1)
    source: Literal["leader_tool", "direct_member"] = "leader_tool"
    parent_turn_id: str | None = Field(default=None, min_length=1, max_length=128)
    instruction: str = Field(min_length=1)


class ActorRunCreateRequest(BaseModel):
    actor_id: str = Field(min_length=1)
    assign_id: str | None = Field(default=None, pattern=_AS)
    llm_profile_revision: str | None = None
