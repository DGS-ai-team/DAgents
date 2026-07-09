from __future__ import annotations

import asyncio
from typing import TYPE_CHECKING, Any, Callable

from app.cli.hitl_batch import expand_hitl_required
from app.cli.render import (
    TranscriptKind,
    TranscriptUpdate,
    format_assistant_end,
    format_tool_call,
    format_tool_result,
)
from app.cli.session_controller import PendingHITL

if TYPE_CHECKING:
    from app.cli.session_controller import SessionController

TranscriptClearCallback = Callable[[], None]


def transcript_updates_from_hydrate(
    entries: Any,
    *,
    show_reasoning: bool,
) -> list[TranscriptUpdate]:
    """将 hydrate transcript 转为 TranscriptUpdate 列表。"""
    if not isinstance(entries, list):
        return []
    updates: list[TranscriptUpdate] = []
    for raw in entries:
        if not isinstance(raw, dict):
            continue
        kind = str(raw.get("kind") or "").strip()
        if kind == "user":
            text = str(raw.get("text") or "").strip()
            if text:
                updates.append(TranscriptUpdate(kind=TranscriptKind.LINE, text=text))
        elif kind == "assistant":
            text = str(raw.get("text") or "").strip()
            if text:
                updates.append(TranscriptUpdate(kind=TranscriptKind.LINE, text=text))
                updates.append(format_assistant_end())
        elif kind == "reasoning":
            if not show_reasoning:
                continue
            text = str(raw.get("text") or "").strip()
            if text:
                updates.append(
                    TranscriptUpdate(kind=TranscriptKind.LINE, text=f"[reasoning] {text}")
                )
        elif kind == "tool_call":
            data = raw.get("data")
            if isinstance(data, dict):
                formatted = format_tool_call(data)
                if formatted is not None:
                    updates.append(formatted)
        elif kind == "tool_result":
            data = raw.get("data")
            if isinstance(data, dict):
                updates.append(format_tool_result(data))
    return updates


def apply_hydrate_seq_hint(controller: SessionController, seq: Any) -> None:
    hint = int(seq or 0)
    if hint > 0:
        controller._last_event_seq = hint
        controller._turn_seq_fence = hint
    else:
        controller._turn_seq_fence = 0


def apply_hydrate_turn_state(controller: SessionController, data: dict[str, Any]) -> None:
    phase = str(data.get("run_turn_phase") or "").strip()
    pending = data.get("pending_hitl")
    items = pending.get("items") if isinstance(pending, dict) else None
    has_pending = isinstance(items, list) and len(items) > 0
    if has_pending or phase == "awaiting_hitl":
        controller._awaiting_user_turn = False
        controller._user_turn_started = False
        controller._user_turn_done.set()
        return
    active = {
        "model_streaming",
        "awaiting_tool_execution",
        "tool_loop",
        "open_batch",
        "other",
    }
    if data.get("has_active_turn") and phase in active:
        controller._awaiting_user_turn = True
        controller._user_turn_started = True
        controller._user_turn_done.clear()
        return
    controller._awaiting_user_turn = False
    controller._user_turn_done.set()


async def apply_session_hydrate(controller: SessionController, data: dict[str, Any]) -> None:
    """灌入 hydrate 快照：transcript、pending HITL、SSE 水位与 turn 状态。"""
    controller.clear_hitl_queue()
    for update in transcript_updates_from_hydrate(
        data.get("transcript"),
        show_reasoning=controller.show_reasoning,
    ):
        controller._emit_transcript(update)
    pending = data.get("pending_hitl")
    if isinstance(pending, dict):
        user_infos, approval = expand_hitl_required(pending)
        for ui in user_infos:
            controller._enqueue_hitl(PendingHITL(kind="user_information", data=ui))
        if approval:
            controller._enqueue_hitl(PendingHITL(kind="approval", data=approval))
    apply_hydrate_seq_hint(controller, data.get("sse_seq_hint"))
    apply_hydrate_turn_state(controller, data)
    seq = int(data.get("sse_seq_hint") or 0)
    if seq > 0:
        asyncio.create_task(controller._post_session_ack(seq))
    await controller._sync_child_agents_from_api()
    if controller._hitl_pending_cb is not None:
        controller._hitl_pending_cb()
