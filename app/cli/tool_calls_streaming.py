from __future__ import annotations

import re
from typing import Any

from app.cli.tool_calls import parse_tool_arguments, tool_display_name


def _extract_partial_json_string(raw: str, key: str) -> str:
    """从不完整 JSON 字符串中提取某 key 的部分 string 值。"""
    marker = f'"{key}"'
    start = raw.find(marker)
    if start < 0:
        return ""
    colon = raw.find(":", start + len(marker))
    if colon < 0:
        return ""
    i = colon + 1
    while i < len(raw) and raw[i] in " \t\r\n":
        i += 1
    if i >= len(raw) or raw[i] != '"':
        return ""
    i += 1
    out: list[str] = []
    while i < len(raw):
        ch = raw[i]
        if ch == "\\" and i + 1 < len(raw):
            out.append(raw[i + 1])
            i += 2
            continue
        if ch == '"':
            break
        out.append(ch)
        i += 1
    return "".join(out)


def streaming_tool_call_preview(
    name: str,
    raw_arguments: Any,
) -> tuple[dict[str, Any], str | None, str]:
    """从不完整 arguments 提取流式预览。

    返回 `(parsed_args, code_content, code_lexer)`；无法预览时 code_content 为 None。
    """
    name = str(name or "").strip() or "unknown"
    raw = raw_arguments if isinstance(raw_arguments, str) else ""
    parsed = parse_tool_arguments(raw)
    if parsed:
        summary, code, lexer = _tool_call_parts_from_parsed(name, parsed)
        return parsed, code, lexer

    raw = raw.strip()
    if not raw:
        return {}, None, "bash"

    if name == "bash_run":
        command = _extract_partial_json_string(raw, "command")
        if command:
            args = {"command": command}
            return args, command, "bash"

    if name == "write_file":
        content = _extract_partial_json_string(raw, "content")
        if content:
            args = {"content": content}
            return args, content, "text"

    if name == "search_replace":
        path = _extract_partial_json_string(raw, "path")
        args: dict[str, Any] = {}
        if path:
            args["path"] = path
        if args:
            return args, None, "text"
        return {}, None, "text"

    return {}, raw, "json"


def _tool_call_parts_from_parsed(
    name: str,
    arguments: dict[str, Any],
) -> tuple[dict[str, Any], str | None, str]:
    """已解析 arguments 的预览（与 TUI `_tool_call_parts_from_call` 对齐）。"""
    from app.cli.tool_calls import tool_call_purpose

    if name == "bash_run":
        purpose = tool_call_purpose(arguments)
        command = str(arguments.get("command") or "")
        if purpose:
            title = tool_display_name(name, arguments)
            return arguments, command if command else None, "bash"
        title = tool_display_name(name, arguments)
        if command and len(command) <= 36:
            return arguments, None, "bash"
        return arguments, command if command else None, "bash"
    summary = tool_display_name(name, arguments)
    if name == "write_file":
        content = str(arguments.get("content") or "")
        return arguments, content if content else None, "text"
    return arguments, None, "bash"
