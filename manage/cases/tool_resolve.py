"""案例库 tool 消息工具名解析与 orphan 过滤。"""

from __future__ import annotations

from typing import Any

from manage.cases.models import CaseMessage

def build_tool_call_map(messages: list[dict[str, Any]]) -> dict[str, dict[str, str]]:
    """从 assistant.tool_calls 构建 tool_call_id → {name, arguments}。"""
    out: dict[str, dict[str, str]] = {}
    for msg in messages:
        if str(msg.get("role") or "") != "assistant":
            continue
        tool_calls = msg.get("tool_calls")
        if not isinstance(tool_calls, list):
            continue
        for tc in tool_calls:
            if not isinstance(tc, dict):
                continue
            call_id = str(tc.get("id") or "").strip()
            if not call_id:
                continue
            fn = tc.get("function") if isinstance(tc.get("function"), dict) else {}
            name = str(fn.get("name") or tc.get("name") or "").strip()
            args = fn.get("arguments")
            if args is None:
                args = tc.get("arguments")
            out[call_id] = {
                "name": name,
                "arguments": "" if args is None else str(args),
            }
    return out


def resolve_tool_name(
    raw: dict[str, Any],
    tool_call_map: dict[str, dict[str, str]],
) -> str | None:
    """解析 tool 消息工具名；无法解析时返回 None。"""
    name = str(raw.get("name") or "").strip()
    if name:
        return name

    call_id = str(raw.get("tool_call_id") or "").strip()
    if call_id:
        matched = tool_call_map.get(call_id)
        if matched and matched.get("name"):
            return matched["name"]

    return None


def filter_unlinked_tool_messages(messages: list[CaseMessage]) -> list[CaseMessage]:
    """丢弃无法关联工具名的 role=tool 消息。"""
    raws = [m.raw or {} for m in messages]
    tool_call_map = build_tool_call_map(raws)
    out: list[CaseMessage] = []
    for msg in messages:
        if msg.role != "tool":
            out.append(msg)
            continue
        if resolve_tool_name(msg.raw or {}, tool_call_map):
            out.append(msg)
    return out
