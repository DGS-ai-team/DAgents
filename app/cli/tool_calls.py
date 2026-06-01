from __future__ import annotations

import json
from typing import Any


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
