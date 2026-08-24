"""Timeline / Outbox / HITL 轻量模型（D3）。"""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, Field

_WG = r"^wg_[0-9a-z]{26}$"
_EV = r"^ev_[0-9a-z]{26}$"
_HT = r"^ht_[0-9a-z]{26}$"
_RN = r"^rn_[0-9a-z]{26}$"
_QH = r"^qh_[0-9a-z]{26}$"
_TURN = r"^[0-9a-zA-Z_-]{1,128}$"


class TimelineEvent(BaseModel):
    event_id: str = Field(pattern=_EV)
    workgroup_id: str = Field(pattern=_WG)
    seq: int = Field(ge=1)
    type: Literal[
        "human_message",
        "assistant_content",
        "actor_final_text",
        "system_notice",
        "assign_started",
        "assign_finished",
    ]
    actor_id: str
    text: str = ""
    created_at: str
    # 禁止原始工具载荷字段出现在公开 Timeline
    client_message_id: str | None = None
    # 投影用：provider-safe name；缺省由 actor_id 推导
    protocol_name: str | None = None
    # 成员最终产出绑定的 Assign（用于 Leader 去重）
    assign_id: str | None = None
    # 结构化直达关系；仅由成员选择器写入，不能从 text 中推断。
    direct_member_id: str | None = None


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
    # Bind an in-loop HITL to the durable actor history.  Explicit API-created
    # information requests may leave these fields empty.
    run_id: str | None = Field(default=None, pattern=_RN)
    tool_call_id: str | None = Field(default=None, min_length=1)
    # Opaque routing metadata for Node AgentRef HITL. It is not exposed as
    # prompt text, but makes the pending approval recoverable across Manage
    # restarts and lets resolution return to the originating Node session.
    metadata: dict[str, Any] = Field(default_factory=dict)


class QueuedHumanRecord(BaseModel):
    queue_id: str = Field(pattern=_QH)
    workgroup_id: str = Field(pattern=_WG)
    text: str = Field(min_length=1)
    from_node_id: str = Field(min_length=1)
    client_message_id: str | None = None
    direct_member_id: str | None = None
    disable_tools: bool = False
    priority: int = 0
    created_at: str
    updated_at: str


class TurnCheckpoint(BaseModel):
    workgroup_id: str = Field(pattern=_WG)
    turn_token: str = Field(pattern=_TURN)
    mode: str = Field(min_length=1, max_length=64)
    metadata: dict[str, Any] = Field(default_factory=dict)
    updated_at: str


class HumanPostRequest(BaseModel):
    text: str = Field(min_length=1)
    client_message_id: str | None = None
    from_node_id: str = Field(min_length=1)
    # 调试/实验：可禁用 supervisor 工具，仅验证纯对话路径。
    disable_tools: bool = False
    # @直连：选择器写入的 member_id；有则跳过 Leader LLM，直接 Assign 该成员
    direct_member_id: str | None = None


class QueuedHumanPatchRequest(BaseModel):
    text: str = Field(min_length=1)


class TurnCancelRequest(BaseModel):
    client_message_id: str | None = None


class TurnCancelResponse(BaseModel):
    cancelled: bool
    mode: str = ""  # leader | direct | idle
    failed_assign_ids: list[str] = Field(default_factory=list)
    leader_run_id: str | None = None
    member_run_id: str | None = None
    member_run_ids: list[str] = Field(default_factory=list)


class ProvisionCompleteRequest(BaseModel):
    member_id: str
    provision_id: str
    workspace_path: str = ""
    tool_catalog_revision: str = ""
    status: Literal["ready", "error"] = "ready"
    error_code: str | None = None
    message: str | None = None


class ToolResultApplyRequest(BaseModel):
    command_id: str
    assign_id: str
    member_id: str
    status: Literal["succeeded", "failed", "indeterminate", "rejected", "canceled"]
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


_EN = r"^en_[0-9a-z]{26}$"


class WSEnvelope(BaseModel):
    envelope_id: str = Field(pattern=_EN)
    schema_version: Literal["0.5.0"] = "0.5.0"
    type: str
    workgroup_id: str | None = None
    delivery_seq: int = Field(ge=1)
    connection_generation: int | None = Field(default=None, ge=1)
    payload: dict[str, Any] = Field(default_factory=dict)
    sent_at: str


class SessionHello(BaseModel):
    node_id: str = Field(min_length=1)
    last_ack_delivery_seq: int = Field(ge=0, default=0)


class ResumeOffer(BaseModel):
    last_ack_delivery_seq: int = Field(ge=0)


class Subscription(BaseModel):
    workgroup_id: str = Field(pattern=_WG)
    node_id: str = Field(min_length=1, max_length=128)
    subscribed_at: str


class SubscribeRequest(BaseModel):
    node_id: str = Field(min_length=1, max_length=128)
