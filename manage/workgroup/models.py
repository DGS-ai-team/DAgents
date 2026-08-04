"""Workgroup pydantic 模型（对齐 D0.5 schemas）。"""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, Field, field_validator

SCHEMA_VERSION = "0.5.0"

_WG = r"^wg_[0-9a-z]{26}$"
_MB = r"^mb_[0-9a-z]{26}$"
_GR = r"^gr_[0-9a-z]{26}$"
_LS = r"^ls_[0-9a-z]{26}$"
_AS = r"^as_[0-9a-z]{26}$"
_RN = r"^rn_[0-9a-z]{26}$"
_SHA = r"^sha256:[0-9a-f]{64}$"
_TOOL = r"^[a-z][a-z0-9_]{0,63}$"


class WorkGroup(BaseModel):
    workgroup_id: str = Field(pattern=_WG)
    schema_version: Literal["0.5.0"] = SCHEMA_VERSION
    display_name: str = Field(min_length=1, max_length=128)
    status: Literal["active", "archiving", "archived"]
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


class MemberPrompt(BaseModel):
    soul_md: str = ""
    user_md: str = ""
    custom_md: str = ""


class MemoryEntry(BaseModel):
    key: str
    value: str


class MemberMemory(BaseModel):
    remember_enabled: Literal[False] = False
    initial_entries: list[MemoryEntry] = Field(default_factory=list, max_length=32)


class MemberTools(BaseModel):
    allow_names: list[str] = Field(default_factory=list)
    side_effect_classes: list[str] = Field(default_factory=list)

    @field_validator("allow_names")
    @classmethod
    def _tool_names(cls, values: list[str]) -> list[str]:
        out: list[str] = []
        seen: set[str] = set()
        for raw in values:
            name = str(raw or "").strip()
            if not name or name in seen:
                continue
            seen.add(name)
            out.append(name)
        return out


class MemberWorkspace(BaseModel):
    root_kind: Literal["member_workspace"] = "member_workspace"


class MemberSpec(BaseModel):
    member_id: str = Field(pattern=_MB)
    workgroup_id: str = Field(pattern=_WG)
    home_node_id: str = Field(min_length=1, max_length=128)
    display_name: str = Field(min_length=1, max_length=64)
    member_generation: int = Field(ge=1)
    llm_profile_id: str = Field(min_length=1)
    llm_profile_revision: str = Field(min_length=1)
    max_tool_loops: int = Field(ge=1, le=256, default=32)
    prompt: MemberPrompt = Field(default_factory=MemberPrompt)
    memory: MemberMemory = Field(default_factory=MemberMemory)
    tools: MemberTools = Field(default_factory=MemberTools)
    policy_ceiling: dict[str, Any] = Field(default_factory=dict)
    workspace: MemberWorkspace = Field(default_factory=MemberWorkspace)
    skills: Literal["disabled"] = "disabled"
    hooks: Literal["disabled"] = "disabled"
    digest: str = Field(pattern=_SHA)


class WorkGroupMember(BaseModel):
    member_id: str = Field(pattern=_MB)
    workgroup_id: str = Field(pattern=_WG)
    home_node_id: str = Field(min_length=1, max_length=128)
    display_name: str = Field(min_length=1)
    status: Literal["requested", "provisioning", "ready", "busy", "archived", "error"]
    member_generation: int = Field(ge=1)
    member_spec_digest: str = Field(pattern=_SHA)
    active_assign_id: str | None = Field(default=None, pattern=_AS)
    created_at: str
    archived_at: str | None = None


class NodeExecutionGrant(BaseModel):
    grant_id: str = Field(pattern=_GR)
    workgroup_id: str = Field(pattern=_WG)
    member_id: str = Field(pattern=_MB)
    home_node_id: str = Field(min_length=1, max_length=128)
    member_spec_digest: str = Field(pattern=_SHA)
    status: Literal["invited", "accepted", "revoked"]
    lease_id: str = Field(pattern=_LS)
    lease_epoch: int = Field(ge=1)
    member_generation: int = Field(ge=1)
    tool_allow_names: list[str] = Field(default_factory=list)
    workspace_contract: dict[str, Any] = Field(default_factory=dict)
    policy_ceiling: dict[str, Any] = Field(default_factory=dict)
    invited_at: str
    accepted_at: str | None = None
    revoked_at: str | None = None


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
    leader_tool_call_id: str = Field(min_length=1)
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


class ACLPatchRequest(BaseModel):
    owners: list[str] | None = None
    collaborators: list[str] | None = None
    expected_revision: int = Field(ge=1)


class MemberCreateRequest(BaseModel):
    home_node_id: str = Field(min_length=1, max_length=128)
    display_name: str = Field(min_length=1, max_length=64)
    llm_profile_id: str | None = None
    llm_profile_revision: str | None = None
    max_tool_loops: int = Field(ge=1, le=256, default=32)
    prompt: MemberPrompt = Field(default_factory=MemberPrompt)
    memory: MemberMemory = Field(default_factory=MemberMemory)
    allow_tool_names: list[str] = Field(default_factory=list)
    side_effect_classes: list[str] = Field(default_factory=list)
    policy_ceiling: dict[str, Any] = Field(default_factory=dict)


class GrantInviteRequest(BaseModel):
    member_id: str = Field(pattern=_MB)
    tool_allow_names: list[str] | None = None


class AssignCreateRequest(BaseModel):
    member_id: str = Field(pattern=_MB)
    leader_run_id: str | None = Field(default=None, pattern=_RN)
    leader_tool_call_id: str = Field(min_length=1, default="call_assign_1")
    instruction: str = Field(min_length=1)


class ActorRunCreateRequest(BaseModel):
    actor_id: str = Field(min_length=1)
    assign_id: str | None = Field(default=None, pattern=_AS)
    llm_profile_revision: str | None = None
