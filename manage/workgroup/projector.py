"""ContextProjector 骨架（D1：结构就绪，不做完整 LLM 投影）。"""

from __future__ import annotations

from typing import Any

from manage.workgroup.models import ActorRun, WorkGroupMember


def project_actor_context(
    *,
    actor_id: str,
    run: ActorRun | None = None,
    member: WorkGroupMember | None = None,
    timeline_events: list[dict[str, Any]] | None = None,
) -> dict[str, Any]:
    """返回供 Leader/Member LLM 使用的投影信封（D1 最小字段）。

    D3 前不读取真实 Timeline；调用方传入的 events 原样计入 watermark。
    """
    events = timeline_events or []
    watermark = run.timeline_watermark_seq if run is not None else 0
    return {
        "actor_id": actor_id,
        "run_id": run.run_id if run else None,
        "member_id": member.member_id if member else None,
        "messages": [],
        "open_tool_calls": [],
        "timeline_event_count": len(events),
        "timeline_watermark_seq": watermark,
        "note": "D1 skeleton: ContextProjector structure only; full projection in D3+",
    }
