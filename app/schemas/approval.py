"""工具审批：出站事件载荷与 resume 入站结构（Pydantic）。

与 `OpenAIImplicitReActRuntime.run_turn`、`AgentService._map_event_envelope_to_stream` 对齐；
客户端应对 SSE `approval_required` 的 `data` 与 `POST /v1/messages` 的 `resume_value` 按本模块类型构造/校验。
"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from pydantic import BaseModel, ConfigDict, Field, TypeAdapter

# --------------------------------------------------------------------------- #
# 出站：runtime 在 yield approval_required（及随后的 done）前使用的 payload（与 envelope payload 一致）
# --------------------------------------------------------------------------- #


class ToolCallApprovalItem(BaseModel):
    """审批卡片中单条待执行工具（与 `tool_call` 事件中列表项一致）。"""

    model_config = ConfigDict(extra="allow")

    id: str
    name: str
    arguments: dict[str, Any] = Field(default_factory=dict)
    raw_arguments: Any | None = None


class ApprovalToolCallsArgs(BaseModel):
    """`approval_required.payload.args` 的固定结构。"""

    model_config = ConfigDict(extra="forbid")

    tool_calls: list[ToolCallApprovalItem]


class ApprovalRequiredEnvelopePayload(BaseModel):
    """`AgentEventEnvelope(event_type='approval_required', payload=...)` 的 payload 形态。

    逻辑：
    - `args.tool_calls` 与前置 `tool_call` 事件中的 `tool_calls` 列表同源；
    - 经 `AgentService._map_event_envelope_to_stream` 映射为 SSE 扁平字段（见 **`ApprovalRequiredSseData`**）。
    """

    model_config = ConfigDict(extra="forbid")

    approval_type: Literal["execute_tool"] = "execute_tool"
    message: str
    args: ApprovalToolCallsArgs
    description: str = ""


# --------------------------------------------------------------------------- #
# 出站：SSE / AgentStreamEventData.data（_map_event_envelope_to_stream 之后）
# --------------------------------------------------------------------------- #


class ApprovalRequiredSseData(BaseModel):
    """`event: approval_required` 时 SSE `data` 内嵌的 `data` 字段（扁平）。"""

    model_config = ConfigDict(extra="allow")

    approval_type: str = "execute_tool"
    content: str = ""
    approval_args: dict[str, Any] = Field(default_factory=dict)
    description: str = ""
    approval_id: str | None = None


# --------------------------------------------------------------------------- #
# 入站：resume 时用户对工具执行的决策（写入 MessageEnvelope.resume_value）
# --------------------------------------------------------------------------- #


class ResumeToolApprove(BaseModel):
    """同意执行当前 pending 中的全部工具调用。"""

    model_config = ConfigDict(extra="ignore", frozen=True)

    type: Literal["approve"] = "approve"


class ResumeToolReject(BaseModel):
    """拒绝执行：runtime 为每条 pending 写占位 tool 消息。"""

    model_config = ConfigDict(extra="ignore", frozen=True)

    type: Literal["reject"] = "reject"


class ResumeToolSelection(BaseModel):
    """对当前 pending 中部分工具做逐条决策。

    - `approved`: 被允许执行的 call_id 列表；
    - `rejected`: 被显式拒绝的 call_id 列表；
    - 未出现在任一列表中的 pending 工具保持为 pending，等待后续 resume。
    """

    model_config = ConfigDict(extra="ignore", frozen=True)

    type: Literal["selection"] = "selection"
    approved: list[str] = Field(default_factory=list)
    rejected: list[str] = Field(default_factory=list)


ResumeToolUnion = ResumeToolApprove | ResumeToolReject | ResumeToolSelection

_resume_adapter: TypeAdapter[ResumeToolUnion] = TypeAdapter(
    Annotated[ResumeToolUnion, Field(discriminator="type")]
)


def parse_resume_tool_decision(resume_value: Any) -> ResumeToolUnion:
    """将任意 `resume_value` 校验为 **approve** / **reject** / **selection** 之一。

    逻辑：
    1. 若为 `dict`，尝试按 **`type` 判别式** 校验为 **`ResumeToolApprove`** / **`ResumeToolReject`**；
    2. 校验失败（缺 `type`、未知 `type`、类型错误）则视为 **`ResumeToolReject`**。

    关键边界：
    - 与历史行为一致：非 approve 即拒绝，不向外抛 `ValidationError`。
    """
    if not isinstance(resume_value, dict):
        return ResumeToolReject()
    try:
        return _resume_adapter.validate_python(resume_value)
    except Exception:
        return ResumeToolReject()


def is_tool_execution_approved(resume_value: Any) -> bool:
    """是否同意执行工具（等价于 `parse_resume_tool_decision` 为 **approve**）。"""
    return isinstance(parse_resume_tool_decision(resume_value), ResumeToolApprove)
