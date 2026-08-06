"""Member 工作区可执行工具：共享 JSON 目录 + LLM schemas + side_effect。

权威源：仓库根 `shared/workgroup/member_tool_catalog.json`（与 Node go:embed 同一文件）。
Node 单机不连 Manage 时读嵌入副本；Manage/Console 读磁盘同一文件——禁止运行时互相 HTTP 拉目录。
仅包含能在 member workspace（Registry fs_root）内独立执行的工具（fs + bash）。
"""

from __future__ import annotations

import json
from functools import lru_cache
from pathlib import Path
from typing import Any


def _catalog_path() -> Path:
    # manage/workgroup/member_tools.py → repo root
    here = Path(__file__).resolve()
    candidates = [
        here.parents[2] / "shared" / "workgroup" / "member_tool_catalog.json",
        Path.cwd() / "shared" / "workgroup" / "member_tool_catalog.json",
    ]
    for p in candidates:
        if p.is_file():
            return p
    raise FileNotFoundError(
        "member_tool_catalog.json not found; expected at shared/workgroup/ "
        f"(tried: {', '.join(str(c) for c in candidates)})"
    )


@lru_cache(maxsize=1)
def _load_catalog() -> dict[str, Any]:
    raw = json.loads(_catalog_path().read_text(encoding="utf-8"))
    tools = raw.get("tools") or []
    if not tools:
        raise ValueError("member_tool_catalog.json: empty tools")
    return raw


def _catalog_entries() -> list[dict[str, Any]]:
    return list(_load_catalog()["tools"])


MEMBER_TOOL_SIDE_EFFECT: dict[str, str] = {}
MEMBER_EXECUTABLE_TOOL_NAMES: list[str] = []


def _refresh_module_exports() -> None:
    global MEMBER_TOOL_SIDE_EFFECT, MEMBER_EXECUTABLE_TOOL_NAMES
    entries = _catalog_entries()
    MEMBER_TOOL_SIDE_EFFECT = {e["id"]: e["side_effect"] for e in entries}
    MEMBER_EXECUTABLE_TOOL_NAMES = [e["id"] for e in entries]


_refresh_module_exports()


def default_allow_tool_names() -> list[str]:
    return [e["id"] for e in _catalog_entries() if e.get("default")]


def member_tool_catalog() -> dict[str, Any]:
    """GET catalog 响应体。"""
    data = _load_catalog()
    tools = [
        {
            "id": e["id"],
            "label": e["label"],
            "group": e["group"],
            "group_label": e["group_label"],
            "hint": e["hint"],
            "default": bool(e.get("default")),
            "side_effect": e["side_effect"],
        }
        for e in data["tools"]
    ]
    groups = data.get("groups") or [
        {"id": "fs", "label": "文件系统"},
        {"id": "bash", "label": "Shell"},
    ]
    return {
        "tools": tools,
        "default_allow_names": default_allow_tool_names(),
        "groups": groups,
    }


def _openai_from_entry(e: dict[str, Any]) -> dict[str, Any]:
    params = e.get("parameters") or {"type": "object", "properties": {}}
    # 确保 OpenAI function.parameters 形状
    parameters = {
        "type": params.get("type", "object"),
        "additionalProperties": params.get("additionalProperties", False),
        "properties": params.get("properties") or {},
    }
    if "required" in params:
        parameters["required"] = params["required"]
    return {
        "type": "function",
        "function": {
            "name": e["id"],
            "description": e.get("description") or e["id"],
            "parameters": parameters,
        },
    }


@lru_cache(maxsize=1)
def _by_name() -> dict[str, dict[str, Any]]:
    return {e["id"]: _openai_from_entry(e) for e in _catalog_entries()}


def member_openai_tools(allow_names: list[str] | None) -> list[dict[str, Any]]:
    """按 MemberSpec allowlist 组装 OpenAI tools（未知名忽略）。"""
    names = [str(n).strip() for n in (allow_names or []) if str(n).strip()]
    out: list[dict[str, Any]] = []
    seen: set[str] = set()
    catalog = _by_name()
    for name in names:
        if name in seen:
            continue
        tool = catalog.get(name)
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
        "Shell commands (if available) also run with the member workspace as the default cwd.",
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
