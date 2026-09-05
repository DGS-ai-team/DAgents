"""ContextProjector：Timeline + ActorRunHistory → 合法 LLM messages。

规则由当前 Workgroup timeline/run-history 模型和
docs/design/workgroup-and-node-gateway.md 共同定义。
"""

from __future__ import annotations

from typing import Any

from manage.workgroup.d3_models import TimelineEvent
from manage.workgroup.context_compression import ActorContextSnapshot, context_messages
from manage.workgroup.history import (
    RunHistoryMessage,
    extract_assign_ids_from_tool_results,
    open_tool_call_ids,
)
from manage.workgroup.models import ActorRun, WorkGroupMember
from manage.workgroup.protocol_names import protocol_name_for_actor

_TIMELINE_WINDOW = 10


def project_actor_context(
    *,
    actor_id: str,
    run: ActorRun | None = None,
    member: WorkGroupMember | None = None,
    timeline_events: list[TimelineEvent] | list[dict[str, Any]] | None = None,
    own_run_history: list[RunHistoryMessage] | list[dict[str, Any]] | None = None,
    context_snapshot: ActorContextSnapshot | None = None,
) -> dict[str, Any]:
    """构造 Actor 相对 LLM 上下文。

    - 本 Actor RunHistory 原样保留（含未完成 tool 配对）
    - 其他 Actor 的 Timeline 事件投影为 user + protocol_name
    - open tool_calls 时新 Timeline 进 buffer，不注入
    - 同 assign_id 已有 tool result 时跳过 Timeline 最终文本（去重）
    """
    history = list(own_run_history or [])
    events = [_as_event(e) for e in (timeline_events or [])]
    watermark = run.timeline_watermark_seq if run is not None else 0
    open_ids = open_tool_call_ids(history)
    covered_assigns = extract_assign_ids_from_tool_results(history)
    # Persistent actor sessions already contain their own human/task inputs.
    # Do not project the corresponding public Timeline event a second time.
    history_event_seqs = {
        int(m.timeline_event_seq)
        for raw in history
        for m in [
            raw if isinstance(raw, RunHistoryMessage) else RunHistoryMessage.model_validate(raw)
        ]
        if m.timeline_event_seq is not None
    }

    buffered: list[dict[str, Any]] = []
    projected_timeline: list[dict[str, Any]] = []

    if open_ids:
        for ev in events:
            if ev.seq > watermark and _is_other_actor(ev, actor_id):
                buffered.append(_event_brief(ev))
    else:
        candidates = [
            ev
            for ev in events
            if ev.seq > watermark
            and ev.seq not in history_event_seqs
            and _is_other_actor(ev, actor_id)
            and ev.type in {"human_message", "actor_final_text"}
        ]
        candidates.sort(key=lambda e: e.seq)
        window = candidates[-_TIMELINE_WINDOW:]
        for ev in window:
            if ev.assign_id and ev.assign_id in covered_assigns:
                continue
            projected_timeline.append(_project_timeline_event(ev))

    messages = context_messages(history, context_snapshot) + projected_timeline
    return {
        "actor_id": actor_id,
        "run_id": run.run_id if run else None,
        "member_id": member.member_id if member else None,
        "messages": messages,
        "open_tool_calls": open_ids,
        "buffered_timeline": buffered,
        "timeline_event_count": len(events),
        "timeline_watermark_seq": watermark,
        "projected_timeline_seqs": [
            ev.seq for ev in window
            if not (ev.assign_id and ev.assign_id in covered_assigns)
        ] if not open_ids else [],
        "inject_into_llm_context": not bool(open_ids),
        "buffered": bool(open_ids and buffered),
    }


def project_while_tools_open(
    *,
    actor_id: str,
    open_tool_calls: list[str],
    incoming_timeline: TimelineEvent | dict[str, Any],
) -> dict[str, Any]:
    """open tool_calls 时 Timeline 不得注入 LLM 上下文。"""
    ev = _as_event(incoming_timeline)
    return {
        "actor_id": actor_id,
        "open_tool_calls": list(open_tool_calls),
        "inject_into_llm_context": False,
        "buffered": True,
        "buffered_event": _event_brief(ev),
    }


def count_assign_summary_mentions(messages: list[dict[str, Any]], assign_id: str) -> tuple[int, str | None]:
    """统计上下文中同一 assign 摘要出现次数，并标明保留侧。"""
    kept: str | None = None
    count = 0
    for m in messages:
        role = m.get("role")
        content = str(m.get("content") or "")
        if assign_id not in content:
            continue
        count += 1
        if role == "tool":
            kept = "tool_result"
        elif kept is None:
            kept = "timeline_user"
    return count, kept


def _project_timeline_event(ev: TimelineEvent) -> dict[str, Any]:
    name = (ev.protocol_name or "").strip() or protocol_name_for_actor(ev.actor_id)
    return {
        "role": "user",
        "name": name,
        "content": ev.text or "",
    }


def _is_other_actor(ev: TimelineEvent, actor_id: str) -> bool:
    return (ev.actor_id or "").strip() != (actor_id or "").strip()


def _event_brief(ev: TimelineEvent) -> dict[str, Any]:
    return {
        "seq": ev.seq,
        "type": ev.type,
        "actor_id": ev.actor_id,
        "assign_id": ev.assign_id,
        "protocol_name": ev.protocol_name or protocol_name_for_actor(ev.actor_id),
        "content_text": ev.text,
        "direct_member_id": ev.direct_member_id,
        "tool_call_id": ev.tool_call_id,
        "command_id": ev.command_id,
        "tool_name": ev.tool_name,
        "status": ev.status,
    }


def _as_event(raw: TimelineEvent | dict[str, Any]) -> TimelineEvent:
    if isinstance(raw, TimelineEvent):
        return raw
    # fixture 常用 content_text / protocol_name；补齐 TimelineEvent 必填字段
    data = dict(raw)
    if "text" not in data and "content_text" in data:
        data["text"] = data.pop("content_text")
    data.setdefault("event_id", data.get("event_id") or f"ev_{str(data.get('seq', 0)).zfill(26)}")
    # event_id must match ^ev_[0-9a-z]{26}$ — for fixtures use synthetic
    eid = str(data["event_id"])
    if not (eid.startswith("ev_") and len(eid) == 29):
        # synthesize valid-looking id from seq
        seq = int(data.get("seq") or 1)
        data["event_id"] = f"ev_{seq:026x}"[:3 + 26]
        # ensure exactly 26 hex after prefix
        data["event_id"] = f"ev_{format(seq, '026x')}"
    data.setdefault("workgroup_id", data.get("workgroup_id") or "wg_00000000000000000000000001")
    data.setdefault("created_at", data.get("created_at") or "2026-01-01T00:00:00Z")
    data.setdefault("actor_id", data.get("actor_id") or "unknown")
    data.setdefault("type", data.get("type") or "human_message")
    data.setdefault("seq", int(data.get("seq") or 1))
    return TimelineEvent.model_validate(data)
