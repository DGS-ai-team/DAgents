from __future__ import annotations

import json
from typing import Any

CALL_PURPOSE_KEY = "call_purpose"
_RUN_IN_BACKGROUND_KEY = "run_in_background"
_USER_INFORMATION_TOOL_NAME = "ask_user_information"


def parse_tool_arguments(raw: Any) -> dict[str, Any]:
    """将 tool arguments 解析为 dict（支持 JSON 字符串或已是 dict）。"""
    if isinstance(raw, dict):
        return raw
    if isinstance(raw, str) and raw.strip():
        try:
            parsed = json.loads(raw)
        except json.JSONDecodeError:
            return {}
        return parsed if isinstance(parsed, dict) else {}
    return {}


def normalize_tool_call_item(item: dict[str, Any]) -> dict[str, Any]:
    """将 SSE tool_call 项规范为 UI 使用的 `{id, name, arguments}`。

    逻辑：
    1. 优先读取 Node approval 等已扁平字段（`name` / `arguments` dict）；
    2. 否则从 OpenAI 结构 `function.name` / `function.arguments` 提取；
    3. `arguments` 为 JSON 字符串时解析为 dict。

    关键边界：
    - 非 dict 输入退化为 unknown + 空参数；
    - 缺 name 时用 `unknown`。
    """
    if not isinstance(item, dict):
        return {"id": "", "name": "unknown", "arguments": {}}

    call_id = str(item.get("id") or "").strip()
    name = str(item.get("name") or "").strip()
    arguments_raw = item.get("arguments")

    fn = item.get("function")
    if isinstance(fn, dict):
        if not name:
            name = str(fn.get("name") or "").strip()
        if arguments_raw is None or (isinstance(arguments_raw, str) and not arguments_raw.strip()):
            arguments_raw = fn.get("arguments")

    return {
        "id": call_id,
        "name": name or "unknown",
        "arguments": parse_tool_arguments(arguments_raw),
    }


def tool_call_purpose(arguments: dict[str, Any]) -> str:
    """读取 call_purpose 字段（Client 首行展示）。"""
    value = str(arguments.get(CALL_PURPOSE_KEY) or "").strip()
    if len(value) > 48:
        return value[:47] + "…"
    return value


def _tool_display_base_name(name: str) -> str:
    if name == "bash_run":
        return "bash"
    return name


def tool_display_name(name: str, arguments: dict[str, Any]) -> str:
    """生成工具调用/审批首行短标题（优先 call_purpose，如 bash(检查端口)）。"""
    name = str(name or "").strip() or "unknown"
    if name == _USER_INFORMATION_TOOL_NAME:
        return "Agent 询问"
    purpose = tool_call_purpose(arguments)
    if purpose:
        return f"{_tool_display_base_name(name)}({purpose})"
    if name == "bash_run":
        cmd = str(arguments.get("command") or "").strip()
        if not cmd:
            return "bash(—)"
        from app.cli.render import sanitize_inline_tool_arg

        cmd = sanitize_inline_tool_arg(cmd)
        if len(cmd) > 48:
            cmd = cmd[:47] + "…"
        return f"bash({cmd})"
    if name == "trigger_create":
        trigger_name = str(arguments.get("name") or "").strip()
        return f"trigger_create({trigger_name or '—'})"
    if name in {"write_file", "read_file", "search_replace"}:
        path = str(arguments.get("path") or arguments.get("file_path") or "").strip()
        return f"{name}({path or '—'})"
    from app.cli.child_agent import format_temporary_agent_tool_title

    temp_title = format_temporary_agent_tool_title(name, arguments)
    if temp_title is not None:
        return temp_title
    if arguments:
        parts = []
        for key, value in arguments.items():
            if key in {CALL_PURPOSE_KEY, _RUN_IN_BACKGROUND_KEY}:
                continue
            parts.append(f"{key}={value!r}")
        if parts:
            return f"{name}({', '.join(parts)})"
    return f"{name}()"
