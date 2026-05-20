from __future__ import annotations

import json
import sys
from typing import Any

from app.cli.approval import ToolApprovalRequest


def write(text: str = "", *, end: str = "\n") -> None:
    print(text, end=end, flush=True)


def write_error(text: str) -> None:
    print(text, file=sys.stderr, flush=True)


def compact_json(value: Any, *, max_length: int = 500) -> str:
    try:
        text = json.dumps(value, ensure_ascii=False, indent=2)
    except TypeError:
        text = str(value)
    if len(text) <= max_length:
        return text
    return f"{text[:max_length]}..."


def tool_summary(item: ToolApprovalRequest, index: int) -> str:
    risk = f" risk={item.risk_level}" if item.risk_level else ""
    reason = f"\n    reason: {item.approval_reason}" if item.approval_reason else ""
    args = compact_json(item.arguments, max_length=700)
    return f"  {index}. {item.name} ({item.call_id}){risk}\n    args: {args}{reason}"


def write_tool_result(data: dict[str, Any]) -> None:
    name = data.get("tool_name") or "tool"
    call_id = data.get("tool_call_id") or ""
    status = "rejected" if data.get("rejected") else "done"
    content = str(data.get("content") or "").strip()
    write(f"\n[tool:{status}] {name} {call_id}".rstrip())
    if content:
        write(content)


def write_tool_call(data: dict[str, Any]) -> None:
    tool_calls = data.get("tool_calls")
    if not isinstance(tool_calls, list) or not tool_calls:
        return
    write("\n[tool call]")
    for index, item in enumerate(tool_calls, start=1):
        if not isinstance(item, dict):
            continue
        name = item.get("name") or "unknown"
        call_id = item.get("id") or ""
        args = item.get("arguments") if isinstance(item.get("arguments"), dict) else {}
        write(f"  {index}. {name} ({call_id})")
        if args:
            write(f"    args: {compact_json(args, max_length=500)}")
