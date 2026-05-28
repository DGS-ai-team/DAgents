"""向用户询问补充信息的工具（由编排器特殊处理，不在同步路径直接执行）。"""

from __future__ import annotations

from typing import Optional

from pydantic import BaseModel, ConfigDict, Field

from app.context.models import OpenAIConversationContext
from app.harness.tools.tool import tool

ASK_USER_INFORMATION_TOOL = "ask_user_information"


class UserInformationOptionArgs(BaseModel):
    """选项字段。"""

    model_config = ConfigDict(extra="forbid")

    id: str = Field(min_length=1)
    label: str = Field(min_length=1)
    value: str = ""


class AskUserInformationArgs(BaseModel):
    """`ask_user_information` 参数 schema。"""

    model_config = ConfigDict(extra="forbid")

    question: str = Field(min_length=1)
    options: list[UserInformationOptionArgs] | None = None
    allow_multiple: bool = False
    placeholder: str = ""
    required: bool = True


def format_user_information_tool_result(
    *,
    answer: str,
    selected_options: list[str],
    cancelled: bool = False,
) -> str:
    """将用户回答格式化为写入 `role=tool` 的正文。"""
    if cancelled:
        return "[USER_INFORMATION_CANCELLED] 用户取消了信息补充。"
    lines = [
        "[USER_INFORMATION]",
        f"answer={answer!r}",
        f"selected_options={selected_options!r}",
    ]
    return "\n".join(lines)


@tool(ASK_USER_INFORMATION_TOOL)
def ask_user_information(
    question: str,
    options: Optional[list[dict[str, str]]] = None,
    allow_multiple: bool = False,
    placeholder: str = "",
    required: bool = True,
    context: OpenAIConversationContext | None = None,
) -> str:
    """使用场景：需要用户补充偏好、确认或选择时调用；不要用于可从上下文推断的信息。

    字段说明：
    - question: 向用户展示的问题（必填）
    - options: 可选项列表，每项含 `id`/`label`，可选 `value`；为空则收集自由文本
    - allow_multiple: 是否允许多选（仅 `options` 非空时有效，默认 false）
    - placeholder: 自由文本输入时的占位提示（可选）
    - required: 是否必须回答（默认 true；TUI 仍允许 Esc 取消整轮）

    返回说明：
    - 正常由编排器等待用户回答后回灌；若误入同步执行路径则返回 `ERROR:`。

    调用范例：
    - `ask_user_information({"question":"部署目标环境？"})`
    - `ask_user_information({"question":"选择数据库","options":[{"id":"pg","label":"PostgreSQL"},{"id":"mysql","label":"MySQL"}]})`
    """
    del context, question, options, allow_multiple, placeholder, required
    return (
        "ERROR: ask_user_information 应由编排器通过 user_information_required 等待用户回答，"
        "不应直接同步执行。"
    )


ask_user_information.args_schema = AskUserInformationArgs  # type: ignore[attr-defined]
