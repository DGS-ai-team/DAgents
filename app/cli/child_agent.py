"""同进程临时 Agent（temporary agent）TUI 辅助；与外部 A2A 区分。"""

from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Any

from app.cli.tool_calls import normalize_tool_call_item

HITL_SCOPE_TEMPORARY_AGENT = "temporary_agent"

EVENT_TEMPORARY_AGENT_CREATED = "temporary_agent_created"
EVENT_TEMPORARY_AGENT_COMPLETED = "temporary_agent_completed"
EVENT_TEMPORARY_AGENT_CANCELLED = "temporary_agent_cancelled"


def child_session_id_from_data(data: dict[str, Any]) -> str:
    """从 SSE data 提取 child_session_id。"""
    return str(data.get("child_session_id") or "").strip()


def approval_queue_key(data: dict[str, Any]) -> str:
    """HITL 队列中 approval 的去重键。

    逻辑：
    1. 临时 Agent：按 `child_session_id` 各占一条槽位，并行任务可并存；
    2. 父 Agent：无 child_id 时按 `approval_id` 区分，缺省共用 `parent:` 槽位；
    3. 同键新事件覆盖旧项（server 刷新同一 pending），不同键追加。

    关键边界：
    - 与 `user_information` 队列项无关，询问入队不清理审批项。
    """
    child_id = child_session_id_from_data(data)
    if child_id:
        return f"child:{child_id}"
    approval_id = str(data.get("approval_id") or "").strip()
    if approval_id:
        return f"parent:{approval_id}"
    return "parent:"


def is_temporary_agent_approval(data: dict[str, Any]) -> bool:
    """判断 approval_required 是否属于临时 Agent 工具审批（非 A2A）。"""
    if str(data.get("hitl_scope") or "").strip() == HITL_SCOPE_TEMPORARY_AGENT:
        return True
    return bool(child_session_id_from_data(data))


def should_skip_child_runtime_display(event_type: str, data: dict[str, Any]) -> bool:
    """临时 Agent turn 产生的 SSE 是否应对用户隐藏（审批与生命周期除外）。"""
    if not child_session_id_from_data(data):
        return False
    if event_type in {
        "approval_required",
        EVENT_TEMPORARY_AGENT_CREATED,
        EVENT_TEMPORARY_AGENT_COMPLETED,
        EVENT_TEMPORARY_AGENT_CANCELLED,
    }:
        return False
    return True


def format_child_lifecycle_line(event_type: str, data: dict[str, Any]) -> str:
    """格式化临时 Agent 生命周期系统提示行。"""
    child_id = child_session_id_from_data(data)
    short = child_id[:16] + "…" if len(child_id) > 16 else child_id
    purpose = str(data.get("purpose") or "").strip()
    if event_type == EVENT_TEMPORARY_AGENT_CREATED:
        if purpose:
            return f"临时 Agent 已创建 · {purpose} · {short}"
        return f"临时 Agent 已创建 · {short}"
    if event_type == EVENT_TEMPORARY_AGENT_COMPLETED:
        status = str(data.get("status") or "completed").strip() or "completed"
        summary = _first_nonempty_line(str(data.get("summary") or ""))
        if summary:
            return f"临时 Agent 已结束 · {short} · {_truncate_text(summary, 48)}"
        if purpose:
            return f"临时 Agent 已结束 · {purpose} · {short} · {status}"
        return f"临时 Agent 已结束 · {short} · {status}"
    if event_type == EVENT_TEMPORARY_AGENT_CANCELLED:
        reason = str(data.get("reason") or "").strip()
        if reason:
            return f"临时 Agent 已取消 · {short} · {reason}"
        return f"临时 Agent 已取消 · {short}"
    return ""


TEMPORARY_AGENT_TOOLS = frozenset(
    {
        "create_temporary_agent",
        "wait_temporary_agents",
        "temporary_agent_status",
        "cancel_temporary_agent",
    }
)


def format_temporary_agent_tool_title(name: str, arguments: dict[str, Any]) -> str | None:
    """生成临时 Agent 管理工具在 transcript 中的短标题。"""
    if name not in TEMPORARY_AGENT_TOOLS:
        return None
    args = arguments or {}
    if name == "create_temporary_agent":
        purpose = str(args.get("purpose") or "—").strip() or "—"
        if args.get("wait"):
            return f"创建临时 Agent · {purpose} (wait)"
        return f"创建临时 Agent · {purpose}"
    if name == "wait_temporary_agents":
        ids = _string_list(args.get("child_session_ids"))
        title = "等待临时 Agent" if not ids else f"等待 {len(ids)} 个临时 Agent"
        timeout = _int_value(args.get("timeout_seconds"))
        if timeout > 0:
            title += f" · {timeout}s"
        return title
    if name == "temporary_agent_status":
        ids = _string_list(args.get("child_session_ids"))
        return "查询临时 Agent 状态" if not ids else f"查询 {len(ids)} 个临时 Agent 状态"
    if name == "cancel_temporary_agent":
        short = _short_child_id(str(args.get("child_session_id") or ""))
        return "取消临时 Agent" if not short else f"取消临时 Agent · {short}"
    return name + "()"


def parse_temporary_agent_tool_result(tool_name: str, content: str) -> tuple[str, str] | None:
    """将临时 Agent 工具 JSON 结果解析为 (summary, detail)。"""
    if tool_name not in TEMPORARY_AGENT_TOOLS:
        return None
    text = str(content or "").strip()
    if text.startswith("ERROR:"):
        return text, text
    try:
        payload: Any = json.loads(text)
    except json.JSONDecodeError:
        return None
    if tool_name == "create_temporary_agent" and isinstance(payload, dict):
        return _format_create_temporary_agent_result(payload)
    if tool_name == "wait_temporary_agents" and isinstance(payload, dict):
        return _format_wait_temporary_agents_result(payload)
    if tool_name == "temporary_agent_status":
        if isinstance(payload, list):
            return _format_temporary_agent_batch_result("temporary_agent_status", payload, timed_out=False)
        if isinstance(payload, dict):
            results = payload.get("results")
            if isinstance(results, list):
                return _format_temporary_agent_batch_result("temporary_agent_status", results, timed_out=False)
    if tool_name == "cancel_temporary_agent" and isinstance(payload, dict):
        short = _short_child_id(str(payload.get("child_session_id") or ""))
        status = str(payload.get("status") or "cancelled").strip() or "cancelled"
        summary = "✓ 已取消临时 Agent"
        if short:
            summary += f" · {short}"
        summary += f" · {status}"
        return summary, ""
    return None


def _format_create_temporary_agent_result(payload: dict[str, Any]) -> tuple[str, str]:
    kind = str(payload.get("kind") or "").strip()
    if kind == "result":
        return _format_single_temporary_agent_result(payload, headline_prefix="✓ 临时 Agent 完成")
    parts = ["✓ 已创建临时 Agent"]
    short = _short_child_id(str(payload.get("child_session_id") or ""))
    purpose = str(payload.get("purpose") or "").strip()
    if short:
        parts.append(short)
    if purpose:
        parts.append(purpose)
    max_turns = _int_value(payload.get("max_turns"))
    if max_turns > 0:
        parts.append(f"max_turns={max_turns}")
    skills = _string_list(payload.get("loaded_skills"))
    if skills:
        parts.append("skills=" + ",".join(skills))
    return " · ".join(parts), ""


def _format_wait_temporary_agents_result(payload: dict[str, Any]) -> tuple[str, str]:
    results = payload.get("results")
    if not isinstance(results, list):
        return "✓ wait_temporary_agents · 无结果", ""
    timed_out = bool(payload.get("timed_out"))
    return _format_temporary_agent_batch_result("wait_temporary_agents", results, timed_out=timed_out)


def _format_temporary_agent_batch_result(
    tool_name: str,
    results: list[Any],
    *,
    timed_out: bool,
) -> tuple[str, str]:
    parsed = [_normalize_temporary_agent_result(item) for item in results if isinstance(item, dict)]
    total = len(parsed)
    if total == 0:
        return f"✓ {tool_name} · 无结果", ""
    completed = sum(1 for item in parsed if item.get("status") == "completed")
    failed = sum(
        1
        for item in parsed
        if str(item.get("status") or "") in {"failed", "cancelled", "expired"}
    )
    summary = f"✓ {tool_name} · {completed + failed}/{total} 已结束"
    if completed > 0:
        summary += f"（{completed} 成功"
        if failed > 0:
            summary += f"，{failed} 异常"
        summary += "）"
    elif failed > 0:
        summary += f"（{failed} 异常）"
    if timed_out:
        summary += " · 超时"
    lines: list[str] = []
    for index, item in enumerate(parsed, start=1):
        short = _short_child_id(str(item.get("child_session_id") or ""))
        status = str(item.get("status") or "unknown").strip() or "unknown"
        prefix = f"[{index}] {short} · {status}"
        hint = _temporary_agent_result_hint(item)
        lines.append(f"{prefix} · {hint}" if hint else prefix)
    return summary, "\n".join(lines)


def _format_single_temporary_agent_result(payload: dict[str, Any], *, headline_prefix: str) -> tuple[str, str]:
    item = _normalize_temporary_agent_result(payload)
    short = _short_child_id(str(item.get("child_session_id") or ""))
    status = str(item.get("status") or "unknown").strip() or "unknown"
    summary = f"{headline_prefix} · {short} · {status}" if short else f"{headline_prefix} · {status}"
    detail = _temporary_agent_result_detail(item, verbose=True)
    return summary, detail


def _normalize_temporary_agent_result(item: dict[str, Any]) -> dict[str, Any]:
    artifacts = item.get("artifacts")
    if not isinstance(artifacts, list):
        artifacts = []
    return {
        "child_session_id": str(item.get("child_session_id") or "").strip(),
        "status": str(item.get("status") or "").strip(),
        "summary": str(item.get("summary") or "").strip(),
        "error": str(item.get("error") or "").strip(),
        "turn_count": _int_value(item.get("turn_count")),
        "artifacts": [str(x).strip() for x in artifacts if str(x).strip()],
    }


def _temporary_agent_result_hint(item: dict[str, Any]) -> str:
    err = str(item.get("error") or "").strip()
    if err:
        return _truncate_text(err, 72)
    summary = str(item.get("summary") or "").strip()
    if summary:
        return _truncate_text(_first_nonempty_line(summary), 72)
    return ""


def _temporary_agent_result_detail(item: dict[str, Any], *, verbose: bool) -> str:
    parts: list[str] = []
    err = str(item.get("error") or "").strip()
    if err:
        parts.append(f"error: {err}")
    summary = str(item.get("summary") or "").strip()
    if summary:
        parts.append(summary if verbose else _first_nonempty_line(summary))
    artifacts = item.get("artifacts") or []
    if artifacts:
        parts.append("artifacts: " + ", ".join(str(x) for x in artifacts))
    turn_count = _int_value(item.get("turn_count"))
    if turn_count > 0:
        parts.append(f"turn_count={turn_count}")
    return "\n".join(parts)


def _short_child_id(child_id: str) -> str:
    child_id = str(child_id or "").strip()
    if len(child_id) <= 16:
        return child_id
    return child_id[:16] + "…"


def _first_nonempty_line(text: str) -> str:
    for line in str(text).splitlines():
        line = line.strip()
        if line:
            return line
    return str(text).strip()


def _truncate_text(text: str, max_len: int) -> str:
    text = str(text)
    if len(text) <= max_len:
        return text
    return text[: max_len - 1] + "…"


def _string_list(raw: Any) -> list[str]:
    if not isinstance(raw, list):
        return []
    out: list[str] = []
    for item in raw:
        text = str(item).strip()
        if text:
            out.append(text)
    return out


def _int_value(raw: Any) -> int:
    try:
        return max(0, int(raw))
    except (TypeError, ValueError):
        return 0


def _terminal_child_statuses() -> frozenset[str]:
    return frozenset({"completed", "failed", "cancelled", "expired"})


def child_session_ids_in_wait_tool_result(content: str) -> list[str]:
    """从 wait_temporary_agents 工具结果提取已汇总展示的 child_session_id。"""
    text = str(content or "").strip()
    if not text or text.startswith("ERROR:"):
        return []
    try:
        payload = json.loads(text)
    except json.JSONDecodeError:
        return []
    if not isinstance(payload, dict):
        return []
    results = payload.get("results")
    if not isinstance(results, list):
        return []
    ids: list[str] = []
    terminal = _terminal_child_statuses()
    for item in results:
        if not isinstance(item, dict):
            continue
        child_id = str(item.get("child_session_id") or "").strip()
        status = str(item.get("status") or "").strip()
        if child_id and status in terminal:
            ids.append(child_id)
    return ids


@dataclass
class ChildLifecycleSuppress:
    """抑制 wait_temporary_agents 工具结果已覆盖的 lifecycle 系统行。"""

    pending_wait: set[str] = field(default_factory=set)
    shown_in_tool: set[str] = field(default_factory=set)

    def reset(self) -> None:
        self.pending_wait.clear()
        self.shown_in_tool.clear()

    def note_tool_call(self, data: dict[str, Any]) -> None:
        tool_calls = data.get("tool_calls")
        if not isinstance(tool_calls, list):
            calls = [data]
        else:
            calls = [item for item in tool_calls if isinstance(item, dict)]
        for item in calls:
            normalized = normalize_tool_call_item(item)
            if normalized["name"] != "wait_temporary_agents":
                continue
            for child_id in _string_list(normalized["arguments"].get("child_session_ids")):
                self.pending_wait.add(child_id)

    def note_tool_result(self, tool_name: str, content: str) -> None:
        if str(tool_name or "").strip() != "wait_temporary_agents":
            return
        for child_id in child_session_ids_in_wait_tool_result(content):
            self.shown_in_tool.add(child_id)
            self.pending_wait.discard(child_id)

    def should_suppress_lifecycle(self, child_id: str, event_type: str) -> bool:
        if event_type not in {EVENT_TEMPORARY_AGENT_COMPLETED, EVENT_TEMPORARY_AGENT_CANCELLED}:
            return False
        child_id = str(child_id or "").strip()
        if not child_id:
            return False
        if child_id in self.shown_in_tool:
            self.shown_in_tool.discard(child_id)
            return True
        if child_id in self.pending_wait:
            return True
        return False


def approval_header(data: dict[str, Any]) -> str:
    """子/父审批面板标题。"""
    if not is_temporary_agent_approval(data):
        return "工具审批"
    purpose = str(data.get("child_purpose") or "临时 Agent").strip() or "临时 Agent"
    child_id = child_session_id_from_data(data)
    short = child_id[:14] + "…" if len(child_id) > 14 else child_id
    if short:
        return f"临时 Agent 审批 · {purpose} · {short}"
    return f"临时 Agent 审批 · {purpose}"


def format_child_agents_list(items: list[dict[str, Any]], awaiting: dict[str, bool] | None = None) -> str:
    """格式化 /children 输出。"""
    if not items:
        return "活跃临时 Agent: (无)"
    lines = [f"活跃临时 Agent ({len(items)}):"]
    for index, item in enumerate(items, start=1):
        if not isinstance(item, dict):
            continue
        child_id = str(item.get("child_session_id") or "-")
        purpose = str(item.get("purpose") or "-")
        raw_tools = item.get("allowed_tools")
        if isinstance(raw_tools, list):
            tools = ",".join(str(t) for t in raw_tools if str(t).strip())
        else:
            tools = ""
        if not tools:
            tools = "-"
        status = str(item.get("status") or "active")
        if awaiting and awaiting.get(child_id):
            status += " · 待审批"
        turns = item.get("turn_count", 0)
        max_turns = item.get("max_turns", 0)
        expires = str(item.get("expires_at") or "-")
        lines.append(f"  {index}. {child_id}")
        lines.append(f"     purpose={purpose} tools={tools} status={status}")
        lines.append(f"     turns={turns}/{max_turns} expires={expires}")
    return "\n".join(lines)


@dataclass
class ChildAgentEntry:
    purpose: str = ""
    awaiting_approval: bool = False


@dataclass
class ChildAgentTracker:
    """跟踪父 session 下活跃临时 Agent（SSE + HTTP 对齐）。"""

    entries: dict[str, ChildAgentEntry] = field(default_factory=dict)
    lifecycle_suppress: ChildLifecycleSuppress = field(default_factory=ChildLifecycleSuppress)

    def reset(self) -> None:
        self.entries.clear()
        self.lifecycle_suppress.reset()

    def on_created(self, data: dict[str, Any]) -> None:
        child_id = child_session_id_from_data(data)
        if not child_id:
            return
        self.entries[child_id] = ChildAgentEntry(
            purpose=str(data.get("purpose") or "").strip(),
        )

    def on_finished(self, child_id: str) -> None:
        child_id = str(child_id or "").strip()
        if child_id:
            self.entries.pop(child_id, None)

    def set_awaiting_approval(self, child_id: str, on: bool) -> None:
        child_id = str(child_id or "").strip()
        if not child_id:
            return
        entry = self.entries.get(child_id)
        if entry is None:
            if not on:
                return
            entry = ChildAgentEntry()
            self.entries[child_id] = entry
        entry.awaiting_approval = on

    def counts(self) -> tuple[int, int]:
        active = len(self.entries)
        pending = sum(1 for e in self.entries.values() if e.awaiting_approval)
        return active, pending

    def awaiting_map(self) -> dict[str, bool]:
        return {cid: e.awaiting_approval for cid, e in self.entries.items() if e.awaiting_approval}

    def replace_from_api(self, items: list[dict[str, Any]]) -> None:
        next_entries: dict[str, ChildAgentEntry] = {}
        for item in items:
            if not isinstance(item, dict):
                continue
            child_id = str(item.get("child_session_id") or "").strip()
            if not child_id:
                continue
            next_entries[child_id] = ChildAgentEntry(
                purpose=str(item.get("purpose") or "").strip(),
            )
        self.entries = next_entries

    def note_tool_call(self, data: dict[str, Any]) -> None:
        self.lifecycle_suppress.note_tool_call(data)

    def note_tool_result(self, tool_name: str, content: str) -> None:
        self.lifecycle_suppress.note_tool_result(tool_name, content)

    def should_suppress_lifecycle(self, child_id: str, event_type: str) -> bool:
        return self.lifecycle_suppress.should_suppress_lifecycle(child_id, event_type)

    def input_strip_text(self, queue_len: int = 0) -> str:
        """输入框上方状态条文案。"""
        active, pending = self.counts()
        if active == 0 and pending == 0:
            text = "临时 Agent: —"
        else:
            text = f"临时 Agent: {active} 活跃"
            if pending > 0:
                text += f" · {pending} 待审批"
        if queue_len > 1:
            text += f" （队列 {queue_len}）"
        return text
