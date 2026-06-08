from __future__ import annotations

import json
from dataclasses import dataclass, field
from enum import Enum
from typing import Any

from app.cli.approval import ToolApprovalRequest
from app.cli.child_agent import parse_temporary_agent_tool_result
from app.cli.tool_calls import normalize_tool_call_item


class TranscriptKind(str, Enum):
    """Transcript 更新类型，供 SessionController 与 TUI 消费。"""

    ASSISTANT_DELTA = "assistant_delta"
    LINE = "line"
    ERROR = "error"
    ASSISTANT_END = "assistant_end"
    TOOL_CALL = "tool_call"
    TOOL_RESULT = "tool_result"
    COMPRESSION = "compression"


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


def format_user_information_required(data: dict[str, Any]) -> TranscriptUpdate:
    """将 user_information_required SSE 载荷格式化为 transcript 行。"""
    args = data.get("user_information_args")
    question = str(data.get("content") or "").strip()
    if isinstance(args, dict):
        question = str(args.get("question") or question).strip()
    lines = ["[user question]", question or "(empty question)"]
    if isinstance(args, dict):
        options = args.get("options")
        if isinstance(options, list) and options:
            lines.append("options:")
            for index, raw in enumerate(options, start=1):
                if not isinstance(raw, dict):
                    continue
                label = str(raw.get("label") or raw.get("id") or f"option-{index}")
                lines.append(f"  {index}. {label}")
    return TranscriptUpdate(kind=TranscriptKind.LINE, text="\n".join(lines), data=data)


def format_tool_result(data: dict[str, Any]) -> TranscriptUpdate:
    """将 tool_result SSE 载荷格式化为 transcript 行。"""
    name = data.get("tool_name") or "tool"
    call_id = data.get("tool_call_id") or ""
    status = "rejected" if data.get("rejected") else "done"
    content = str(data.get("content") or "").strip()
    parsed = parse_temporary_agent_tool_result(str(name), content)
    lines = [f"[tool:{status}] {name} {call_id}".rstrip()]
    if parsed is not None:
        summary, detail = parsed
        lines[0] = f"[tool:{status}] {summary}"
        if detail:
            lines.append(detail)
    elif content:
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
        normalized = normalize_tool_call_item(item)
        name = normalized["name"]
        call_id = normalized["id"]
        args = normalized["arguments"]
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


def format_context_compression(event_type: str, data: dict[str, Any]) -> TranscriptUpdate:
    """将 blocking/silent 上下文压缩 SSE 转为 TUI 可消费的 transcript 更新。"""
    mode = "silent" if event_type == "context_compression_silent" else "blocking"
    return TranscriptUpdate(
        kind=TranscriptKind.COMPRESSION,
        text="",
        data={
            "mode": mode,
            "phase": str(data.get("phase") or ""),
            "status": str(data.get("status") or ""),
            "compressed_message_count": data.get("compressed_message_count"),
            "compression_start": data.get("compression_start"),
            "compression_end": data.get("compression_end"),
        },
    )


def format_compact_token_count(count: int | None) -> str:
    """将 context token 估算值格式化为 input strip 右侧短文案（无 usage 时的回退）。

    逻辑：
    1. 未拉取过 context 时返回空串（不占位）；
    2. >= 10000 时用一位小数的 k 后缀；
    3. 其余用千分位整数。

    关键边界：
    - 负数按 0 展示。
    """
    if count is None:
        return ""
    if count < 0:
        count = 0
    if count >= 10_000:
        return f"ctx {count / 1000:.1f}k"
    return f"ctx {count:,}"


@dataclass(frozen=True, slots=True)
class UsageStripSnapshot:
    """input strip 右侧最近一次 SSE usage 快照（与 Go client 对齐）。"""

    prompt_tokens: int = 0
    completion_tokens: int = 0
    cache_hit_tokens: int = 0
    cache_hit_rate: float = -1.0
    reasoning_tokens: int = 0
    has_data: bool = False


def parse_usage_strip(data: dict[str, Any]) -> UsageStripSnapshot:
    """从 SSE usage 事件 data 解析 strip 快照。"""
    prompt = _int_from_event_data(data.get("prompt_tokens"))
    completion = _int_from_event_data(data.get("completion_tokens"))
    hit = _int_from_event_data(data.get("prompt_cache_hit_tokens"))
    cached = _int_from_event_data(data.get("prompt_cached_tokens"))
    if hit <= 0 and cached > 0:
        hit = cached
    rate = -1.0
    raw_rate = data.get("prompt_cache_hit_rate")
    if raw_rate is not None:
        try:
            rate = float(raw_rate)
        except (TypeError, ValueError):
            rate = -1.0
    elif prompt > 0 and hit > 0:
        rate = min(1.0, hit / prompt)
    reasoning = _int_from_event_data(data.get("reasoning_tokens"))
    if reasoning <= 0:
        details = data.get("completion_tokens_details")
        if isinstance(details, dict):
            reasoning = _int_from_event_data(details.get("reasoning_tokens"))
    if prompt <= 0 and completion <= 0:
        return UsageStripSnapshot()
    return UsageStripSnapshot(
        prompt_tokens=prompt,
        completion_tokens=completion,
        cache_hit_tokens=hit,
        cache_hit_rate=rate,
        reasoning_tokens=reasoning,
        has_data=True,
    )


def format_input_strip_usage(snapshot: UsageStripSnapshot) -> str:
    """格式化 ↑上行 ↓下行 与 cache hit（无数据时返回空串）。"""
    if not snapshot.has_data:
        return ""
    text = f"↑{_format_compact_count(snapshot.prompt_tokens)} ↓{_format_compact_count(snapshot.completion_tokens)}"
    if snapshot.cache_hit_tokens > 0:
        if snapshot.cache_hit_rate >= 0:
            text += f" · hit {_format_compact_count(snapshot.cache_hit_tokens)} ({snapshot.cache_hit_rate * 100:.0f}%)"
        else:
            text += f" · hit {_format_compact_count(snapshot.cache_hit_tokens)}"
    if snapshot.reasoning_tokens > 0:
        text += f" · think {_format_compact_count(snapshot.reasoning_tokens)}"
    return text


def _int_from_event_data(value: Any) -> int:
    if value is None:
        return 0
    try:
        return max(0, int(value))
    except (TypeError, ValueError):
        return 0


def _format_compact_count(count: int) -> str:
    if count < 0:
        count = 0
    if count >= 10_000:
        rounded = round(count / 1000, 1)
        if rounded == int(rounded):
            return f"{int(rounded)}k"
        return f"{rounded:.1f}k"
    return f"{count:,}"
