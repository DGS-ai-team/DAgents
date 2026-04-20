"""跨模块 Pydantic 模型（审批协议、resume 等）。

会话上下文模型见 `app.context.models`；本包侧重 **API/流式契约** 与可复用校验。
"""

from __future__ import annotations

from app.schemas.approval import (
    ApprovalRequiredEnvelopePayload,
    ApprovalRequiredSseData,
    ApprovalToolCallsArgs,
    ResumeToolApprove,
    ResumeToolReject,
    ResumeToolUnion,
    ToolCallApprovalItem,
    is_tool_execution_approved,
    parse_resume_tool_decision,
)
from app.schemas.agent_peer import (
    AgentPeerCaller,
    AgentPeerContentType,
    AgentPeerEnvelope,
    AgentPeerError,
    AgentPeerIntent,
    AgentPeerPayload,
    AgentPeerTarget,
    AgentPeerTask,
    AgentPeerTaskState,
    build_agent_peer_envelope,
    parse_agent_peer_envelope_from_text,
)

__all__ = [
    "ApprovalRequiredEnvelopePayload",
    "ApprovalRequiredSseData",
    "ApprovalToolCallsArgs",
    "ResumeToolApprove",
    "ResumeToolReject",
    "ResumeToolUnion",
    "ToolCallApprovalItem",
    "is_tool_execution_approved",
    "parse_resume_tool_decision",
    "AgentPeerCaller",
    "AgentPeerContentType",
    "AgentPeerEnvelope",
    "AgentPeerError",
    "AgentPeerIntent",
    "AgentPeerPayload",
    "AgentPeerTarget",
    "AgentPeerTask",
    "AgentPeerTaskState",
    "build_agent_peer_envelope",
    "parse_agent_peer_envelope_from_text",
]
