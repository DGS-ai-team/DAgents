"""同进程临时 Agent（temporary agent）TUI 辅助；与外部 A2A 区分。"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

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
        return f"临时 Agent 已结束 · {short} · {status}"
    if event_type == EVENT_TEMPORARY_AGENT_CANCELLED:
        reason = str(data.get("reason") or "").strip()
        if reason:
            return f"临时 Agent 已取消 · {short} · {reason}"
        return f"临时 Agent 已取消 · {short}"
    return ""


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

    def reset(self) -> None:
        self.entries.clear()

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
