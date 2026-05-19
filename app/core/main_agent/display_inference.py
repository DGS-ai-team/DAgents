"""SSE `display_type` 推断——覆盖 assistant / reasoning / tool_call / tool_result。"""

from __future__ import annotations

import re
from typing import Any, Literal

DisplayType = Literal["terminal", "code", "normal_text", "image", "reasoning", "markdown"]

# 调用方显式传入时的合法集合（含 reasoning / markdown，便于与推理流、AI 正文一致）。
VALID_DISPLAY_TYPES: frozenset[str] = frozenset(
    {"terminal", "code", "normal_text", "image", "reasoning", "markdown"}
)

_TERMINAL_TOOL_NAMES = frozenset({"bash_run", "shell_run", "terminal_run", "cmd_run", "powershell_run"})
_CODE_TOOL_NAMES = frozenset(
    {
        "read_file",
        "write_file",
        "search_replace",
        "search_file",
        "python_run",
        "javascript_run",
        "typescript_run",
        "code_run",
    },
)

_IMAGE_EXT_RE = re.compile(r"https?://\S+\.(png|jpg|jpeg|gif|webp|bmp|svg)(\?\S*)?$", re.IGNORECASE)


def _content_suggests_image(content: str) -> bool:
    fc = str(content or "").strip()
    if not fc:
        return False
    if fc.startswith("data:image/"):
        return True
    if "![](" in fc:
        return True
    return _IMAGE_EXT_RE.search(fc) is not None


def infer_assistant_delta_display_type(content: str) -> DisplayType:
    """流式 assistant 分片：仅依据正文片段推断（无工具名上下文）。

    逻辑：
    1. 命中图片 URL / data URI / markdown 图片语法 → **`image`**；
    2. 命中代码围栏 → **`code`**；
    3. 否则 **`markdown`**（模型正文默认按 Markdown 渲染）。
    """

    fc = str(content or "")
    if _content_suggests_image(fc):
        return "image"
    if "```" in fc:
        return "code"
    return "markdown"


def infer_reasoning_delta_display_type() -> DisplayType:
    """推理流统一标记为 **`reasoning`**，便于前端分区渲染。"""
    return "reasoning"


def infer_tool_call_display_type(assistant_content: str, tool_calls: list[dict[str, Any]]) -> DisplayType:
    """根据本轮 **`assistant_content`** 与 **`tool_calls`** 汇总展示类型。

    逻辑：
    1. 任一工具名为终端类 → **`terminal`**；
    2. 否则任一工具名为代码类 → **`code`**；
    3. 否则若旁白正文像图片/代码 → 对应类型；
    4. 默认 **`normal_text`**。
    """

    names: list[str] = []
    for tc in tool_calls:
        if not isinstance(tc, dict):
            continue
        n = str(tc.get("name") or "").strip().lower()
        if n:
            names.append(n)
    for n in names:
        if n in _TERMINAL_TOOL_NAMES:
            return "terminal"
    for n in names:
        if n in _CODE_TOOL_NAMES:
            return "code"
    ac = str(assistant_content or "")
    if _content_suggests_image(ac):
        return "image"
    if "```" in ac:
        return "code"
    return "normal_text"


def infer_tool_result_display_type(tool_name: str, content: str) -> DisplayType:
    """工具执行结果正文：与原编排层 `_infer_tool_result_display_type` 语义一致。

    逻辑：
    1. 空正文 → **`normal_text`**；
    2. 图片形态 → **`image`**；
    3. 终端类工具名 → **`terminal`**；
    4. 正文含围栏或代码类工具名 → **`code`**；
    5. 其余 **`normal_text`**。
    """

    final_tool_name = str(tool_name or "").strip().lower()
    final_content = str(content or "").strip()
    if not final_content:
        return "normal_text"
    if (
        final_content.startswith("data:image/")
        or "![](" in final_content
        or _IMAGE_EXT_RE.search(final_content) is not None
    ):
        return "image"
    if final_tool_name in _TERMINAL_TOOL_NAMES:
        return "terminal"
    if "```" in final_content or final_tool_name in _CODE_TOOL_NAMES:
        return "code"
    return "normal_text"
