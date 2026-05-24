from __future__ import annotations

import argparse
import asyncio
from datetime import datetime
from typing import Any


def _format_timestamp(value: float | None) -> str:
    """将 Unix 时间戳格式化为本地可读字符串。"""
    if value is None:
        return "-"
    try:
        return datetime.fromtimestamp(value).strftime("%Y-%m-%d %H:%M:%S")
    except (OverflowError, OSError, ValueError):
        return str(value)


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


def _render_session_list(data: dict[str, Any]) -> None:
    """渲染 show session 输出。"""
    active = data.get("active")
    persisted = data.get("persisted")
    active_rows: list[list[str]] = []
    if isinstance(active, list):
        for item in active:
            if not isinstance(item, dict):
                continue
            active_rows.append(
                [
                    str(item.get("session_id") or ""),
                    str(item.get("client_id") or "-"),
                    str(item.get("queue_pending") or 0),
                    "yes" if item.get("has_active_turn") else "no",
                    str(item.get("run_turn_phase") or "-"),
                    _format_timestamp(item.get("last_activity_at")),
                ]
            )
    print("Active sessions (in queue):")
    _print_table(
        ["session_id", "client_id", "queue_pending", "processing", "phase", "last_activity"],
        active_rows,
    )
    print()

    persisted_rows: list[list[str]] = []
    if isinstance(persisted, list):
        for item in persisted:
            if not isinstance(item, dict):
                continue
            persisted_rows.append(
                [
                    str(item.get("session_id") or ""),
                    _format_cell(str(item.get("first_request_message") or ""), 48),
                    str(item.get("updated_at") or "-"),
                    "yes" if item.get("in_queue") else "no",
                ]
            )
    print("Persisted sessions (sqlite):")
    _print_table(
        ["session_id", "first_request", "updated_at", "in_queue"],
        persisted_rows,
    )


async def _run_show_session(api_base: str) -> int:
    """调用 API 列出 session 并打印。"""
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
    """删除不在队列中的 sqlite session。"""
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
        result = await client.delete_persisted_session(sid)
        deleted = bool(result.get("deleted"))
        if deleted:
            print(f"Deleted persisted session: {sid}")
            return 0
        print(f"Session not found in sqlite: {sid}")
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
