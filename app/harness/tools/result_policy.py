"""工具结果过滤与压缩策略。"""

from __future__ import annotations

import re
from dataclasses import dataclass
from pathlib import Path
from uuid import uuid4

from app.config.env import resolve_runtime_root

MODEL_CONTENT_MAX_CHARS = 12000
DISPLAY_CONTENT_MAX_CHARS = 40000

_SECRET_PATTERNS = [
    re.compile(r"(?i)(api[_-]?key|token|secret|password)\s*=\s*([^\s;&]+)"),
    re.compile(r"sk-[A-Za-z0-9_\-]{16,}"),
]


@dataclass(frozen=True, slots=True)
class ToolResultEnvelope:
    """工具结果三路产物。

    逻辑：
    1. `model_content` 写入 OpenAI `role=tool` 上下文；
    2. `display_content` 发送给 SSE 展示；
    3. `raw_ref` 指向落盘原文，供排障或后续分页读取。

    关键边界：
    - 敏感信息会在 model/display/raw_ref 三路脱敏；
    - raw_ref 仅在内容较长或脱敏发生时落盘，命中敏感信息时保存安全副本。
    """

    model_content: str
    display_content: str
    raw_ref: str
    truncated: bool
    sensitive_filtered: bool


def _tool_outputs_dir() -> Path:
    """返回工具原始输出目录并确保存在。"""
    path = (resolve_runtime_root() / ".runtime" / "tool_outputs").resolve()
    path.mkdir(parents=True, exist_ok=True)
    return path


def _filter_sensitive_text(text: str) -> tuple[str, bool]:
    """对常见敏感字段做确定性脱敏。

    逻辑：
    1. 依次应用内置正则；
    2. 命中 `key=value` 形式时保留 key，替换 value；
    3. 命中 OpenAI 风格密钥时整体替换。
    """
    filtered = str(text or "")
    changed = False

    def _mask_assignment(match: re.Match[str]) -> str:
        return f"{match.group(1)}=<redacted>"

    filtered_next = _SECRET_PATTERNS[0].sub(_mask_assignment, filtered)
    if filtered_next != filtered:
        changed = True
        filtered = filtered_next
    filtered_next = _SECRET_PATTERNS[1].sub("<redacted-openai-key>", filtered)
    if filtered_next != filtered:
        changed = True
        filtered = filtered_next
    return filtered, changed


def _clip_middle(text: str, max_chars: int) -> tuple[str, bool]:
    """保留首尾裁剪长文本。

    逻辑：
    1. 未超过上限时原样返回；
    2. 超限时保留前半与后半，中间插入截断提示；
    3. 返回文本与是否截断。
    """
    if len(text) <= max_chars:
        return text, False
    head_len = max_chars // 2
    tail_len = max_chars - head_len
    return (
        text[:head_len]
        + f"\n[TRUNCATED] 工具输出超过 {max_chars} 字符，已保留首尾；完整内容见 raw_ref。\n"
        + text[-tail_len:],
        True,
    )


def package_tool_result(*, tool_name: str, content: str) -> ToolResultEnvelope:
    """将工具原始输出打包为模型/SSE/原文三路结果。

    逻辑：
    1. 对原文做敏感信息脱敏，得到上下文安全版本；
    2. 分别按模型上下文与 SSE 展示上限裁剪；
    3. 当原文超长或发生脱敏时写入 `.runtime/tool_outputs/<id>.txt` 并返回引用；敏感命中时写入脱敏副本。

    Args:
        tool_name: 工具名，用于 raw 文件命名与摘要说明。
        content: 工具原始返回文本。
    """
    raw_text = str(content or "")
    filtered_text, sensitive_filtered = _filter_sensitive_text(raw_text)
    model_text, model_truncated = _clip_middle(filtered_text, MODEL_CONTENT_MAX_CHARS)
    display_text, display_truncated = _clip_middle(filtered_text, DISPLAY_CONTENT_MAX_CHARS)
    should_write_raw = sensitive_filtered or model_truncated or display_truncated
    raw_ref = ""
    if should_write_raw:
        safe_tool = re.sub(r"[^a-zA-Z0-9_.:-]+", "_", str(tool_name or "tool")).strip("_") or "tool"
        path = _tool_outputs_dir() / f"{safe_tool}-{uuid4().hex}.txt"
        persisted_text = filtered_text if sensitive_filtered else raw_text
        path.write_text(persisted_text, encoding="utf-8", errors="replace")
        raw_ref = str(path)
        suffix = f"\n[RAW_REF] {raw_ref}"
        if sensitive_filtered:
            suffix += "\n[SENSITIVE_FILTERED] model/display 内容已脱敏。"
        if model_truncated:
            model_text += suffix
        if display_truncated or sensitive_filtered:
            display_text += suffix
    return ToolResultEnvelope(
        model_content=model_text,
        display_content=display_text,
        raw_ref=raw_ref,
        truncated=model_truncated or display_truncated,
        sensitive_filtered=sensitive_filtered,
    )
