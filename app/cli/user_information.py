"""CLI 侧用户询问事件解析与 resume 构造。"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any


@dataclass(frozen=True, slots=True)
class UserInformationOption:
    """单个可选项。"""

    id: str
    label: str
    value: str = ""


@dataclass(frozen=True, slots=True)
class UserInformationRequest:
    """SSE `user_information_required` 解析结果。"""

    tool_call_id: str
    question: str
    options: list[UserInformationOption]
    allow_multiple: bool
    placeholder: str
    required: bool


@dataclass(frozen=True, slots=True)
class UserInformationAnswer:
    """用户回答，用于构造 resume_value。"""

    tool_call_id: str
    answer: str
    selected_options: list[str]
    cancelled: bool = False

    def to_resume_value(self) -> dict[str, Any]:
        """转为后端 `ResumeUserInformation` 兼容 dict。"""
        return {
            "type": "user_information",
            "tool_call_id": self.tool_call_id,
            "answer": self.answer,
            "selected_options": list(self.selected_options),
            "cancelled": self.cancelled,
        }


class UserInformationCancelled(Exception):
    """用户取消当前信息补充等待；调用方不应 submit resume。"""


def extract_user_information_request(data: dict[str, Any]) -> UserInformationRequest | None:
    """从 SSE `user_information_required.data` 提取询问请求。

    逻辑：
    1. 读取 `user_information_args` 字典；
    2. 解析 `tool_call_id` 与 `question`（`content` 为问题正文兜底）；
    3. 规范化 `options` 列表，过滤无效项。
    """
    args = data.get("user_information_args")
    if not isinstance(args, dict):
        return None
    tool_call_id = str(args.get("tool_call_id") or "").strip()
    question = str(args.get("question") or data.get("content") or "").strip()
    if not tool_call_id or not question:
        return None
    raw_options = args.get("options")
    options: list[UserInformationOption] = []
    if isinstance(raw_options, list):
        for raw in raw_options:
            if not isinstance(raw, dict):
                continue
            option_id = str(raw.get("id") or "").strip()
            label = str(raw.get("label") or "").strip()
            if not option_id or not label:
                continue
            options.append(
                UserInformationOption(
                    id=option_id,
                    label=label,
                    value=str(raw.get("value") or label).strip() or label,
                )
            )
    return UserInformationRequest(
        tool_call_id=tool_call_id,
        question=question,
        options=options,
        allow_multiple=bool(args.get("allow_multiple", False)),
        placeholder=str(args.get("placeholder") or ""),
        required=bool(args.get("required", True)),
    )


def build_answer_from_text(request: UserInformationRequest, text: str) -> UserInformationAnswer:
    """构造自由文本回答。"""
    return UserInformationAnswer(
        tool_call_id=request.tool_call_id,
        answer=str(text or "").strip(),
        selected_options=[],
    )


def build_answer_from_options(
    request: UserInformationRequest,
    selected_ids: list[str],
) -> UserInformationAnswer:
    """构造选项式回答，并生成可读 `answer` 文本。"""
    selected_set = {str(item).strip() for item in selected_ids if str(item).strip()}
    labels: list[str] = []
    for item in request.options:
        if item.id in selected_set:
            labels.append(item.label)
    answer = ", ".join(labels)
    return UserInformationAnswer(
        tool_call_id=request.tool_call_id,
        answer=answer,
        selected_options=sorted(selected_set),
    )
