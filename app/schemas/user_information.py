"""用户询问工具：SSE 出站载荷与 resume 入站结构。"""

from __future__ import annotations

from typing import Annotated, Any, Literal

from pydantic import BaseModel, ConfigDict, Field, TypeAdapter


class UserInformationOption(BaseModel):
    """单个可选项。"""

    model_config = ConfigDict(extra="forbid")

    id: str = Field(min_length=1)
    label: str = Field(min_length=1)
    value: str = ""


class UserInformationRequiredEnvelopePayload(BaseModel):
    """`AgentEventEnvelope(event_type='user_information_required')` 的 payload。"""

    model_config = ConfigDict(extra="forbid")

    message: str
    args: dict[str, Any]
    description: str = "等待用户补充信息"
    display_type: str = "normal_text"


class ResumeUserInformation(BaseModel):
    """用户回答 `ask_user_information` 的 resume 载荷。"""

    model_config = ConfigDict(extra="ignore", frozen=True)

    type: Literal["user_information"] = "user_information"
    tool_call_id: str = Field(min_length=1)
    answer: str = ""
    selected_options: list[str] = Field(default_factory=list)
    cancelled: bool = False


ResumeUnion = ResumeUserInformation

_user_info_adapter: TypeAdapter[ResumeUserInformation] = TypeAdapter(ResumeUserInformation)


def parse_user_information_resume(resume_value: Any) -> ResumeUserInformation | None:
    """解析 `resume_value` 是否为用户信息回答。

    逻辑：
    1. 非 dict 或 `type != user_information` 时返回 `None`；
    2. 否则按 Pydantic 校验；失败返回 `None`。
    """
    if not isinstance(resume_value, dict):
        return None
    if str(resume_value.get("type") or "").strip() != "user_information":
        return None
    try:
        return _user_info_adapter.validate_python(resume_value)
    except Exception:
        return None


def is_user_information_resume(resume_value: Any) -> bool:
    """是否为 `user_information` resume。"""
    return parse_user_information_resume(resume_value) is not None
