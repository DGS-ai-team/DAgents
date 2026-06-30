"""文本清理（JSON / API 序列化安全）。"""

from __future__ import annotations

from typing import Any


def sanitize_unicode(text: str) -> str:
    """移除无法 UTF-8 编码的孤立 surrogate，避免 JSON 序列化 500。"""
    if not text:
        return text
    return text.encode("utf-8", errors="surrogatepass").decode("utf-8", errors="replace")


def sanitize_json_value(value: Any) -> Any:
    if isinstance(value, str):
        return sanitize_unicode(value)
    if isinstance(value, dict):
        return {k: sanitize_json_value(v) for k, v in value.items()}
    if isinstance(value, list):
        return [sanitize_json_value(item) for item in value]
    return value
