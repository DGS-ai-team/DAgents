"""Workgroup 内置 Hook 行为（对齐 Node Agent：日期注入 + 工具结果压缩）。

不引入完整 Hook Registry / 插件；MemberSpec.hooks 仍为 disabled。
"""

from __future__ import annotations

import os
import re
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Any, Callable

# 与 node/internal/llm.UserNameDate / hooks.TodayDateMessagePrefix 对齐
TODAY_DATE_MESSAGE_PREFIX = "当天日期为："
TODAY_DATE_MESSAGE_NAME = "date"

DEFAULT_SPILL_THRESHOLD_TOKENS = 12000
DEFAULT_TOOL_RESULT_TOOLS = (
    "bash_run",
    "read_file",
    "grep_file",
    "grep_files",
    "search_replace",
    "glob_files",
    "write_file",
    "list_workgroup_members",
    "assign_workgroup_task",
)

# 与 node/internal/tokens 一致：汉字 0.6，其余 0.3
_WEIGHT_ASCII = 0.3
_WEIGHT_CJK = 0.6

_DATE_RE = re.compile(r"^当天日期为：(\d{8})$")


def _rune_weight(ch: str) -> float:
    o = ord(ch)
    if 0x4E00 <= o <= 0x9FFF or 0x3400 <= o <= 0x4DBF:
        return _WEIGHT_CJK
    return _WEIGHT_ASCII


def estimate_tokens(text: str) -> float:
    """粗算 token（对齐 Node tokens.Estimate）。"""
    if not text:
        return 0.0
    return sum(_rune_weight(ch) for ch in text)


def format_today_date_message(yyyymmdd: str) -> str:
    return TODAY_DATE_MESSAGE_PREFIX + str(yyyymmdd).strip()


def parse_today_date_message(content: str) -> str | None:
    m = _DATE_RE.match(str(content or "").strip())
    return m.group(1) if m else None


def has_today_date_message(messages: list[dict[str, Any]], today_yyyymmdd: str) -> bool:
    want = format_today_date_message(today_yyyymmdd)
    for msg in messages:
        if str(msg.get("role") or "").strip() != "user":
            continue
        if str(msg.get("content") or "").strip() == want:
            return True
    return False


def ensure_today_date_in_messages(
    messages: list[dict[str, Any]],
    *,
    now: Callable[[], datetime] | None = None,
) -> tuple[list[dict[str, Any]], dict[str, Any] | None]:
    """若尚无当天日期 user 消息则插入；返回 (messages, 新插入的消息或 None)。

    插入位置：若末条为非日期 user，则插在其前，避免盖住本轮用户问题；否则追加到末尾。
    """
    now_fn = now or datetime.now
    today = now_fn().strftime("%Y%m%d")
    if has_today_date_message(messages, today):
        return messages, None
    want = format_today_date_message(today)
    inserted: dict[str, Any] = {
        "role": "user",
        "name": TODAY_DATE_MESSAGE_NAME,
        "content": want,
    }
    out = list(messages)
    idx = len(out)
    if idx > 0 and str(out[idx - 1].get("role") or "").strip() == "user":
        if parse_today_date_message(str(out[idx - 1].get("content") or "")) is None:
            idx = idx - 1
    out.insert(idx, inserted)
    return out, inserted


@dataclass(frozen=True)
class PackagedToolResult:
    for_history: str
    spill_path: str | None = None
    spilled: bool = False


def default_spill_root() -> Path | None:
    """MANAGE_DB_PATH 同级目录下的 workgroup_tool_outputs。"""
    raw = os.environ.get("MANAGE_DB_PATH", "").strip()
    if not raw:
        return None
    return Path(raw).expanduser().resolve().parent / "workgroup_tool_outputs"


def _sanitize_path_segment(s: str) -> str:
    s = (s or "").strip()
    if not s:
        return ""
    out: list[str] = []
    for ch in s:
        if ch.isalnum() or ch in "-_.":
            out.append(ch)
        else:
            out.append("_")
    cleaned = "".join(out).strip("._")
    return cleaned or "x"


def _take_prefix_for_budget(text: str, max_tokens: float) -> str:
    if max_tokens <= 0 or not text:
        return ""
    used = 0.0
    buf: list[str] = []
    for ch in text:
        w = _rune_weight(ch)
        if used + w > max_tokens and used > 0:
            break
        buf.append(ch)
        used += w
    return "".join(buf)


def _take_suffix_for_budget(text: str, max_tokens: float) -> str:
    if max_tokens <= 0 or not text:
        return ""
    used = 0.0
    buf: list[str] = []
    for ch in reversed(text):
        w = _rune_weight(ch)
        if used + w > max_tokens and used > 0:
            break
        buf.append(ch)
        used += w
    buf.reverse()
    return "".join(buf)


def _format_head_tail_with_hint(text: str, max_tokens: int, spill_rel: str) -> str:
    total = estimate_tokens(text)
    limit = float(max_tokens)
    if max_tokens <= 0 or total <= limit:
        return text

    def make_hint(omitted: int) -> str:
        return f"…（已省略约 {omitted} tokens，完整输出已写入 Manage 侧 {spill_rel!r}）…"

    placeholder = make_hint(0)
    hint_tokens = estimate_tokens(placeholder) + 4
    budget = limit - hint_tokens
    if budget < 1:
        return make_hint(int(total + 0.5))
    head_budget = budget / 2
    tail_budget = budget - head_budget
    head = _take_prefix_for_budget(text, head_budget)
    tail = _take_suffix_for_budget(text, tail_budget)
    omitted = total - estimate_tokens(head) - estimate_tokens(tail)
    if omitted < 0:
        omitted = 0
    return head + make_hint(int(omitted + 0.5)) + tail


def package_tool_result(
    raw: str,
    *,
    tool_name: str,
    run_id: str = "",
    tool_call_id: str = "",
    enabled: bool = True,
    spill_threshold_tokens: int = DEFAULT_SPILL_THRESHOLD_TOKENS,
    tools: tuple[str, ...] | list[str] | None = None,
    spill_root: Path | None = None,
) -> PackagedToolResult:
    """超长工具结果落盘 + history 头尾摘要（对齐 Node toolresult.Package）。"""
    text = raw if raw else ""
    trimmed = text.strip()
    if not trimmed and not text:
        text = "（空输出）"
    elif not trimmed:
        text = raw
    else:
        text = trimmed

    allowed = list(tools) if tools is not None else list(DEFAULT_TOOL_RESULT_TOOLS)
    name = (tool_name or "").strip()
    if not enabled or not name or name not in {t.strip() for t in allowed if t.strip()}:
        return PackagedToolResult(for_history=text)

    threshold = spill_threshold_tokens if spill_threshold_tokens > 0 else DEFAULT_SPILL_THRESHOLD_TOKENS
    if estimate_tokens(text) <= float(threshold):
        return PackagedToolResult(for_history=text)

    root = spill_root if spill_root is not None else default_spill_root()
    session = _sanitize_path_segment(run_id) or "unknown-run"
    call = _sanitize_path_segment(tool_call_id) or "unknown-call"
    rel = f"{session}/{call}.txt"

    if root is None:
        summary = _format_head_tail_with_hint(text, threshold, f"(unspilled)/{rel}")
        return PackagedToolResult(for_history=summary, spilled=False)

    abs_path = root / session / f"{call}.txt"
    abs_path.parent.mkdir(parents=True, exist_ok=True)
    abs_path.write_text(text, encoding="utf-8")
    spill_display = f"workgroup_tool_outputs/{rel}"
    summary = _format_head_tail_with_hint(text, threshold, spill_display)
    return PackagedToolResult(for_history=summary, spill_path=str(abs_path), spilled=True)
