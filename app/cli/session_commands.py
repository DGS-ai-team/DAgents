from __future__ import annotations

import argparse
import asyncio
from typing import Any


def _format_cell(text: str, width: int) -> str:
    """按固定列宽截断单元格文本。"""
    raw = str(text or "")
    if len(raw) <= width:
        return raw.ljust(width)
    if width <= 3:
        return raw[:width]
    return f"{raw[: width - 3]}..."


def _print_table(headers: list[str], rows: list[list[str]]) -> None:
    """打印简单 ASCII 表格。"""
    if not rows:
        print("(none)")
        return
    widths = [len(header) for header in headers]
    for row in rows:
        for index, cell in enumerate(row):
            widths[index] = max(widths[index], len(cell))
    header_line = "  ".join(header.ljust(widths[index]) for index, header in enumerate(headers))
    print(header_line)
    print("  ".join("-" * width for width in widths))
    for row in rows:
        print("  ".join(cell.ljust(widths[index]) for index, cell in enumerate(row)))


def _session_rows(data: dict[str, Any], *, active: bool) -> list[list[str]]:
    """从 Node `sessions` 列表筛出 active 或 persisted 行。"""
    sessions = data.get("sessions")
    if not isinstance(sessions, list):
        return []
    rows: list[list[str]] = []
    for item in sessions:
        if not isinstance(item, dict):
            continue
        if bool(item.get("active")) != active:
            continue
        sid = str(item.get("session_id") or "")
        if not sid:
            continue
        if active:
            rows.append(
                [
                    sid,
                    str(item.get("agent_id") or "-"),
                    str(item.get("message_count") or 0),
                    str(item.get("queue_pending") or 0),
                    "yes" if item.get("has_active_turn") else "no",
                    str(item.get("run_turn_phase") or "idle"),
                ]
            )
        else:
            rows.append(
                [
                    sid,
                    str(item.get("agent_id") or "-"),
                    _format_cell(str(item.get("first_user_message") or ""), 48),
                    str(item.get("updated_at") or "-"),
                    str(item.get("message_count") or 0),
                ]
            )
    return rows


def _render_session_list(data: dict[str, Any]) -> None:
    """渲染 show session 输出（Node GET /v1/sessions）。"""
    print("Active sessions (in memory):")
    _print_table(
        ["session_id", "agent_id", "messages", "queue_pending", "processing", "phase"],
        _session_rows(data, active=True),
    )
    print()
    print("Persisted sessions (sqlite, not in memory):")
    _print_table(
        ["session_id", "agent_id", "first_message", "updated_at", "messages"],
        _session_rows(data, active=False),
    )


async def _run_show_session(api_base: str) -> int:
    """调用 Node API 列出 session 并打印。"""
    from app.cli.api_client import DAgentsApiClient

    client = DAgentsApiClient(api_base)
    try:
        if not await client.health():
            print(f"[dagents] backend health check failed: {api_base}/health", flush=True)
            return 1
        data = await client.list_sessions()
        _render_session_list(data)
        return 0
    finally:
        await client.close()


async def _run_delete_session(api_base: str, session_id: str) -> int:
    """释放 session：DELETE /v1/sessions/{id}（内存 + sqlite）。"""
    from app.cli.api_client import DAgentsApiClient

    sid = session_id.strip()
    if not sid:
        print("[dagents] session_id is required", flush=True)
        return 1
    client = DAgentsApiClient(api_base)
    try:
        if not await client.health():
            print(f"[dagents] backend health check failed: {api_base}/health", flush=True)
            return 1
        result = await client.delete_session(sid)
        if bool(result.get("released")):
            print(f"Released session: {sid}")
            return 0
        print(f"Session not found: {sid}")
        return 1
    except RuntimeError as exc:
        print(f"[dagents] {exc}", flush=True)
        return 1
    finally:
        await client.close()


def run_show_session(args: argparse.Namespace) -> int:
    """`dagents show session` 入口。"""
    return asyncio.run(_run_show_session(args.api))


def run_delete_session(args: argparse.Namespace) -> int:
    """`dagents delete session` 入口。"""
    return asyncio.run(_run_delete_session(args.api, args.session_id))
