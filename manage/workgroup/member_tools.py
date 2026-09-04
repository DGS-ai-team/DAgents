"""Shared labels for AgentRef member progress events.

Member tools are registered and executed by the bound Node Agent.  Manage
does not mirror their catalog, schemas, prompts, or side-effect policy.
"""

from __future__ import annotations

import json
from typing import Any


CALL_PURPOSE_KEY = "call_purpose"

_TOOL_PURPOSES = {
    "read_file": "读取文件",
    "show_image": "展示图片",
    "read_image": "分析图片",
    "write_file": "写入文件",
    "glob_files": "查找文件",
    "grep_file": "搜索内容",
    "grep_files": "搜索内容",
    "search_replace": "替换内容",
    "bash_run": "执行命令",
}


def purpose_for_tool(tool_name: str) -> str:
    """Return a safe fallback label for a member tool progress bubble."""
    return _TOOL_PURPOSES.get((tool_name or "").strip(), "执行成员工具")

def call_purpose_from_arguments(arguments_json: str, fallback: str = "") -> str:
    """Read the optional progress label without inspecting other arguments."""
    try:
        raw: Any = json.loads(arguments_json or "{}")
    except (TypeError, json.JSONDecodeError):
        raw = {}
    if isinstance(raw, dict):
        purpose = str(raw.get(CALL_PURPOSE_KEY) or "").strip()
        if purpose:
            return purpose
    return str(fallback or "").strip()
