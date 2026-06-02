"""临时子 Agent TUI 辅助（对齐 Go Client childagent / hitl scope）。"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any


def child_session_id_from_data(data: dict[str, Any]) -> str:
    """从 SSE data 提取 child_session_id。"""
    return str(data.get("child_session_id") or "").strip()


def is_child_agent_approval(data: dict[str, Any]) -> bool:
    """判断 approval_required 是否属于子 Agent。"""
    if str(data.get("hitl_scope") or "").strip() == "child_agent":
        return True
    return bool(child_session_id_from_data(data))


def should_skip_child_runtime_display(event_type: str, data: dict[str, Any]) -> bool:
    """子 Agent turn 产生的 SSE 是否应对用户隐藏（审批与生命周期除外）。"""
    if not child_session_id_from_data(data):
        return False
    if event_type in {
        "approval_required",
        "child_agent_created",
        "child_agent_completed",
        "child_agent_cancelled",
    }:
        return False
    return True


def format_child_lifecycle_line(event_type: str, data: dict[str, Any]) -> str:
    """格式化子 Agent 生命周期系统提示行。"""
    child_id = child_session_id_from_data(data)
    short = child_id[:16] + "…" if len(child_id) > 16 else child_id
    purpose = str(data.get("purpose") or "").strip()
    if event_type == "child_agent_created":
        if purpose:
            return f"子任务已创建 · {purpose} · {short}"
        return f"子任务已创建 · {short}"
    if event_type == "child_agent_completed":
        status = str(data.get("status") or "completed").strip() or "completed"
        return f"子任务已结束 · {short} · {status}"
    if event_type == "child_agent_cancelled":
        reason = str(data.get("reason") or "").strip()
        if reason:
            return f"子任务已取消 · {short} · {reason}"
        return f"子任务已取消 · {short}"
    return ""


def approval_header(data: dict[str, Any]) -> str:
    """子/父审批面板标题。"""
    if not is_child_agent_approval(data):
        return "工具审批"
    purpose = str(data.get("child_purpose") or "子任务").strip() or "子任务"
    child_id = child_session_id_from_data(data)
    short = child_id[:14] + "…" if len(child_id) > 14 else child_id
    if short:
        return f"子任务审批 · {purpose} · {short}"
    return f"子任务审批 · {purpose}"


def format_child_agents_list(items: list[dict[str, Any]], awaiting: dict[str, bool] | None = None) -> str:
    """格式化 /children 输出。"""
    if not items:
        return "活跃子 Agent: (无)"
    lines = [f"活跃子 Agent ({len(items)}):"]
    for index, item in enumerate(items, start=1):
        if not isinstance(item, dict):
            continue
        child_id = str(item.get("child_session_id") or "-")
        purpose = str(item.get("purpose") or "-")
        template = str(item.get("template_id") or "-")
        status = str(item.get("status") or "active")
        if awaiting and awaiting.get(child_id):
            status += " · 待审批"
        turns = item.get("turn_count", 0)
        max_turns = item.get("max_turns", 0)
        expires = str(item.get("expires_at") or "-")
        lines.append(f"  {index}. {child_id}")
        lines.append(f"     purpose={purpose} template={template} status={status}")
        lines.append(f"     turns={turns}/{max_turns} expires={expires}")
    return "\n".join(lines)


@dataclass
class ChildAgentEntry:
    purpose: str = ""
    template_id: str = ""
    awaiting_approval: bool = False


@dataclass
class ChildAgentTracker:
    """跟踪父 session 下活跃子 Agent（SSE + HTTP 对齐）。"""

    entries: dict[str, ChildAgentEntry] = field(default_factory=dict)

    def reset(self) -> None:
        self.entries.clear()

    def on_created(self, data: dict[str, Any]) -> None:
        child_id = child_session_id_from_data(data)
        if not child_id:
            return
        self.entries[child_id] = ChildAgentEntry(
            purpose=str(data.get("purpose") or "").strip(),
            template_id=str(data.get("template_id") or "").strip(),
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
                template_id=str(item.get("template_id") or "").strip(),
            )
        self.entries = next_entries

    def input_strip_text(self, queue_len: int = 0) -> str:
        """输入框上方状态条文案。"""
        active, pending = self.counts()
        if active == 0 and pending == 0:
            text = "子 Agent: —"
        else:
            text = f"子 Agent: {active} 活跃"
            if pending > 0:
                text += f" · {pending} 待审批"
        if queue_len > 1:
            text += f" （队列 {queue_len}）"
        return text
