from __future__ import annotations

import json
from dataclasses import dataclass, field
from enum import Enum
from typing import Any

from app.cli.approval import ToolApprovalRequest


class TranscriptKind(str, Enum):
    """Transcript 更新类型，供 SessionController 与 TUI 消费。"""

    ASSISTANT_DELTA = "assistant_delta"
    LINE = "line"
    ERROR = "error"
    ASSISTANT_END = "assistant_end"
    TOOL_CALL = "tool_call"
    TOOL_RESULT = "tool_result"


@dataclass(frozen=True, slots=True)
class TranscriptUpdate:
    """单条 transcript 更新。"""

    kind: TranscriptKind
    text: str = ""
    data: dict[str, Any] = field(default_factory=dict)


def compact_json(value: Any, *, max_length: int = 500) -> str:
    """将对象压缩为 JSON 字符串，超长截断。"""
    try:
        text = json.dumps(value, ensure_ascii=False, indent=2)
    except TypeError:
        text = str(value)
    if len(text) <= max_length:
        return text
    return f"{text[:max_length]}..."


def tool_summary(item: ToolApprovalRequest, index: int) -> str:
    """格式化单条待审批工具摘要。"""
    risk = f" risk={item.risk_level}" if item.risk_level else ""
    reason = f"\n    reason: {item.approval_reason}" if item.approval_reason else ""
    args = compact_json(item.arguments, max_length=700)
    return f"  {index}. {item.name} ({item.call_id}){risk}\n    args: {args}{reason}"


def format_tool_result(data: dict[str, Any]) -> TranscriptUpdate:
    """将 tool_result SSE 载荷格式化为 transcript 行。"""
    name = data.get("tool_name") or "tool"
    call_id = data.get("tool_call_id") or ""
    status = "rejected" if data.get("rejected") else "done"
    content = str(data.get("content") or "").strip()
    lines = [f"[tool:{status}] {name} {call_id}".rstrip()]
    if content:
        lines.append(content)
    return TranscriptUpdate(kind=TranscriptKind.TOOL_RESULT, text="\n".join(lines), data=data)


def format_tool_call(data: dict[str, Any]) -> TranscriptUpdate | None:
    """将 tool_call SSE 载荷格式化为 transcript 行；无 tool_calls 时返回 None。"""
    tool_calls = data.get("tool_calls")
    if not isinstance(tool_calls, list) or not tool_calls:
        return None
    lines = ["[tool call]"]
    for index, item in enumerate(tool_calls, start=1):
        if not isinstance(item, dict):
            continue
        name = item.get("name") or "unknown"
        call_id = item.get("id") or ""
        args = item.get("arguments") if isinstance(item.get("arguments"), dict) else {}
        lines.append(f"  {index}. {name} ({call_id})")
        if args:
            lines.append(f"    args: {compact_json(args, max_length=500)}")
    return TranscriptUpdate(kind=TranscriptKind.TOOL_CALL, text="\n".join(lines), data=data)


def format_reasoning(content: str) -> TranscriptUpdate:
    """格式化 reasoning 流事件。"""
    return TranscriptUpdate(kind=TranscriptKind.LINE, text=f"[reasoning] {content}")


def format_error(message: str) -> TranscriptUpdate:
    """格式化 error 事件。"""
    return TranscriptUpdate(kind=TranscriptKind.ERROR, text=f"[error] {message}")


def format_assistant_delta(content: str) -> TranscriptUpdate:
    """格式化 assistant 流式增量。"""
    return TranscriptUpdate(kind=TranscriptKind.ASSISTANT_DELTA, text=content)


def format_assistant_end() -> TranscriptUpdate:
    """assistant 流式段结束（换行）。"""
    return TranscriptUpdate(kind=TranscriptKind.ASSISTANT_END)


def format_system_line(text: str) -> TranscriptUpdate:
    """系统提示行。"""
    return TranscriptUpdate(kind=TranscriptKind.LINE, text=text)
