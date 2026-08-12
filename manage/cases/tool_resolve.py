"""案例库 tool 消息工具名解析与 orphan 过滤（对齐 Node async-job 语义）。"""

from __future__ import annotations

from typing import Any

from manage.cases.models import CaseMessage

ASYNC_JOB_PREFIX = "async-job-"


def _content_to_str(content: Any) -> str:
    if content is None:
        return ""
    if isinstance(content, str):
        return content
    return str(content)


def _parse_kv_line(content: str, key: str) -> str:
    prefix = key + "="
    for line in content.splitlines():
        stripped = line.strip()
        if stripped.startswith(prefix):
            return stripped[len(prefix) :].strip()
    return ""


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
    prior: list[dict[str, Any]],
    tool_call_map: dict[str, dict[str, str]],
) -> str | None:
    """解析 tool 消息工具名；无法解析时返回 None。"""
    name = str(raw.get("name") or "").strip()
    if name:
        return name

    call_id = str(raw.get("tool_call_id") or "").strip()
    content = _content_to_str(raw.get("content"))

    if call_id.startswith(ASYNC_JOB_PREFIX):
        async_name = _parse_kv_line(content, "tool_name")
        if async_name:
            return async_name
        src_id = _parse_kv_line(content, "source_tool_call_id")
        if src_id:
            matched = tool_call_map.get(src_id)
            if matched and matched.get("name"):
                return matched["name"]
        job_id = call_id[len(ASYNC_JOB_PREFIX) :].strip()
        if job_id:
            for prev in reversed(prior):
                if str(prev.get("role") or "") != "tool":
                    continue
                prev_content = _content_to_str(prev.get("content"))
                if job_id not in prev_content:
                    continue
                prev_call = str(prev.get("tool_call_id") or "").strip()
                if not prev_call:
                    continue
                matched = tool_call_map.get(prev_call)
                if matched and matched.get("name"):
                    return matched["name"]

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
    for i, msg in enumerate(messages):
        if msg.role != "tool":
            out.append(msg)
            continue
        if resolve_tool_name(msg.raw or {}, raws[:i], tool_call_map):
            out.append(msg)
    return out
