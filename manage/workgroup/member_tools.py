"""Member Node-executable 工具定义（Manage 侧 LLM tools + side_effect）。"""

from __future__ import annotations

from typing import Any

# 与 Node workgroup Executor 对齐的 v1 工具集
MEMBER_TOOL_SIDE_EFFECT: dict[str, str] = {
    "read_file": "fs_read",
    "glob_files": "fs_read",
    "write_file": "fs_write",
}

_READ_FILE = {
    "type": "function",
    "function": {
        "name": "read_file",
        "description": (
            "Read a text file from the member workspace. "
            "path must be relative to the workspace root (e.g. README); "
            "absolute host paths are denied."
        ),
        "parameters": {
            "type": "object",
            "additionalProperties": False,
            "properties": {
                "path": {
                    "type": "string",
                    "description": "Relative path inside the member workspace",
                },
            },
            "required": ["path"],
        },
    },
}

_GLOB_FILES = {
    "type": "function",
    "function": {
        "name": "glob_files",
        "description": (
            "List paths under the member workspace matching a glob. "
            "Does not read file contents. directory is relative (use . for workspace root)."
        ),
        "parameters": {
            "type": "object",
            "additionalProperties": False,
            "properties": {
                "directory": {
                    "type": "string",
                    "description": "Start directory relative to workspace (required; use . for root)",
                },
                "glob_pattern": {
                    "type": "string",
                    "description": "Glob relative to directory, e.g. *.md or **/*.txt",
                },
                "offset": {"type": "integer", "minimum": 0},
                "max_results": {"type": "integer", "minimum": 1},
                "include_dirs": {"type": "boolean"},
            },
            "required": ["directory", "glob_pattern"],
        },
    },
}

_WRITE_FILE = {
    "type": "function",
    "function": {
        "name": "write_file",
        "description": (
            "Write (overwrite) a text file inside the member workspace. "
            "path must be relative; prefer read_file first when editing existing files."
        ),
        "parameters": {
            "type": "object",
            "additionalProperties": False,
            "properties": {
                "path": {"type": "string", "description": "Relative path inside the member workspace"},
                "content": {"type": "string", "description": "Full file contents to write"},
            },
            "required": ["path", "content"],
        },
    },
}

_BY_NAME: dict[str, dict[str, Any]] = {
    "read_file": _READ_FILE,
    "glob_files": _GLOB_FILES,
    "write_file": _WRITE_FILE,
}


def member_openai_tools(allow_names: list[str] | None) -> list[dict[str, Any]]:
    """按 MemberSpec allowlist 组装 OpenAI tools（未知名忽略）。"""
    names = [str(n).strip() for n in (allow_names or []) if str(n).strip()]
    out: list[dict[str, Any]] = []
    seen: set[str] = set()
    for name in names:
        if name in seen:
            continue
        tool = _BY_NAME.get(name)
        if tool is None:
            continue
        seen.add(name)
        out.append(tool)
    return out


def side_effect_for_tool(tool_name: str) -> str:
    return MEMBER_TOOL_SIDE_EFFECT.get((tool_name or "").strip(), "other")


def build_member_system_prompt(*, soul_md: str = "", user_md: str = "", custom_md: str = "") -> str:
    parts = [
        "You are a Workgroup Member agent. Complete the assigned instruction using only the tools provided.",
        "All file paths are relative to your member workspace (never host absolute paths).",
        "When the task is done, reply with a concise final answer and do not call tools.",
    ]
    soul = (soul_md or "").strip()
    user = (user_md or "").strip()
    custom = (custom_md or "").strip()
    if soul:
        parts.append("## Soul\n" + soul)
    if user:
        parts.append("## User\n" + user)
    if custom:
        parts.append("## Custom\n" + custom)
    return "\n\n".join(parts)
