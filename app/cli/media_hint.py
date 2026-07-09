"""TUI 媒体 path / URL 提示（F-M8，不渲染像素）。"""

from __future__ import annotations

import json
from typing import Any


def _clean(value: Any) -> str:
    text = str(value or "").strip()
    return "" if text.lower() == "none" else text


def _media_items(data: dict[str, Any]) -> list[dict[str, Any]]:
    raw = data.get("media")
    if not isinstance(raw, list):
        return []
    return [item for item in raw if isinstance(item, dict)]


def _format_media_item(item: dict[str, Any]) -> str:
    url = _clean(item.get("url"))
    if not url:
        return ""
    label = _clean(item.get("label")) or "image"
    caption = _clean(item.get("caption"))
    if caption:
        return f"{label}: {url} ({caption})"
    return f"{label}: {url}"


def _path_from_prefixed_content(content: str) -> str:
    for line in str(content or "").splitlines():
        line = line.strip()
        if line.startswith("path="):
            return line.removeprefix("path=").strip()
    return ""


def _path_from_args(data: dict[str, Any]) -> str:
    for key in ("arguments", "args"):
        raw = data.get(key)
        if isinstance(raw, dict):
            for field in ("path", "file_path"):
                path = _clean(raw.get(field))
                if path:
                    return path
        elif isinstance(raw, str) and raw.strip():
            try:
                parsed = json.loads(raw)
            except json.JSONDecodeError:
                continue
            if isinstance(parsed, dict):
                for field in ("path", "file_path"):
                    path = _clean(parsed.get(field))
                    if path:
                        return path
    return ""


def _screenshot_path_from_json(content: str) -> str:
    text = str(content or "").strip()
    if not text.startswith("{"):
        return ""
    try:
        payload = json.loads(text)
    except json.JSONDecodeError:
        return ""
    if not isinstance(payload, dict):
        return ""
    return _clean(payload.get("screenshot_path"))


def _show_image_path_hint(tool_name: str, content: str, data: dict[str, Any]) -> str:
    name = _clean(tool_name)
    if name in {"show_image", "read_image"}:
        return _path_from_args(data) or _path_from_prefixed_content(content)
    if name.startswith("browser_"):
        return _screenshot_path_from_json(content)
    return ""


def media_hint_lines(data: dict[str, Any] | None) -> list[str]:
    """从 tool_result / hydrate data 提取图片 URL 或 path 行。"""
    if not isinstance(data, dict):
        return []
    seen: set[str] = set()
    lines: list[str] = []
    for item in _media_items(data):
        hint = _format_media_item(item)
        if hint and hint not in seen:
            seen.add(hint)
            lines.append(hint)
    if lines:
        return lines
    tool_name = _clean(data.get("tool_name") or data.get("name"))
    content = _clean(data.get("content") or data.get("output"))
    path = _show_image_path_hint(tool_name, content, data)
    if path:
        return [f"image path: {path}"]
    return []


def user_media_hint_lines(entry: dict[str, Any] | None) -> list[str]:
    if not isinstance(entry, dict):
        return []
    return media_hint_lines({"media": entry.get("media"), "images": entry.get("images")})
