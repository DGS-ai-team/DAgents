"""触发器列表格式化（/triggers 命令面板）。"""

from __future__ import annotations

import json
from datetime import datetime, timezone
from typing import Any

from rich.console import Group
from rich.text import Text


def format_condition_summary(condition: Any) -> str:
    if not isinstance(condition, dict) or not condition:
        return "manual"
    interval = _int_from_any(condition.get("interval_seconds"))
    if interval > 0:
        return f"interval {interval}s"
    fire_at = _float_from_any(condition.get("fire_at"))
    if fire_at > 0:
        return f"once @ {format_unix_timestamp(fire_at)}"
    schedule = condition.get("schedule")
    if isinstance(schedule, dict) and schedule:
        kind = str(schedule.get("kind") or "calendar").strip() or "calendar"
        return f"schedule:{kind}"
    cmd = str(condition.get("cmd") or "").strip()
    if cmd:
        preview = cmd if len(cmd) <= 32 else cmd[:31] + "…"
        return f"cmd gate: {preview}"
    return "manual"


def format_unix_timestamp(ts: Any) -> str:
    value = _float_from_any(ts)
    if value <= 0:
        return "-"
    try:
        dt = datetime.fromtimestamp(value, tz=timezone.utc).astimezone()
        return dt.strftime("%Y-%m-%d %H:%M")
    except (OSError, OverflowError, ValueError):
        return str(int(value))


def format_triggers_panel(items: list[Any]) -> Group:
    """将 GET /v1/triggers 列表格式化为 Rich Group。"""
    if not items:
        return Group(Text("  (无已配置触发器)", style="bright_black"))
    parts: list[Text] = []
    for raw in items:
        if not isinstance(raw, dict):
            continue
        parts.extend(_format_trigger_block(raw))
        parts.append(Text(""))
    if parts and str(parts[-1]) == "":
        parts.pop()
    return Group(*parts)


def _format_trigger_block(item: dict[str, Any]) -> list[Text]:
    name = str(item.get("name") or "(未命名)").strip() or "(未命名)"
    trigger_id = str(item.get("trigger_id") or "-").strip() or "-"
    enabled = bool(item.get("enabled", True))
    state = "enabled" if enabled else "disabled"
    state_style = "green" if enabled else "bright_black"
    condition = format_condition_summary(item.get("condition"))
    next_fire = format_unix_timestamp(item.get("next_fire_at"))
    last_fire = format_unix_timestamp(item.get("last_fired_at"))
    fire_count = _int_from_any(item.get("fire_count"))
    session_mode = str(item.get("session_target_mode") or "").strip()
    target_session = item.get("target_session_id")
    session_hint = session_mode or "-"
    if isinstance(target_session, str) and target_session.strip():
        session_hint = f"{session_hint} · {target_session.strip()}"

    lines: list[Text] = [
        Text.assemble(
            ("- ", "bright_black"),
            (name, "bold cyan"),
            (f"  [{state}]", state_style),
        ),
        Text(f"    id: {trigger_id}", style="bright_black"),
        Text.assemble(
            ("    调度: ", "bright_black"),
            (condition, ""),
            ("    下次: ", "bright_black"),
            (next_fire, ""),
        ),
        Text(
            f"    触发 {fire_count} 次 · 上次 {last_fire} · 会话 {session_hint}",
            style="bright_black",
        ),
    ]
    task = str(item.get("task_template") or "").strip()
    if task:
        preview = task if len(task) <= 72 else task[:71] + "…"
        lines.append(Text(f"    任务: {preview}", style="bright_black"))
    return lines


def format_condition_json(condition: Any) -> str:
    if not isinstance(condition, dict):
        return "{}"
    try:
        return json.dumps(condition, ensure_ascii=False, sort_keys=True)
    except TypeError:
        return str(condition)


def _int_from_any(value: Any) -> int:
    if isinstance(value, bool):
        return int(value)
    if isinstance(value, int):
        return value
    if isinstance(value, float):
        return int(value)
    return 0


def _float_from_any(value: Any) -> float:
    if isinstance(value, (int, float)):
        return float(value)
    return 0.0
