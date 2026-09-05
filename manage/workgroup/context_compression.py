"""Replayable context compression for persistent Workgroup actor runs.

The actor run history is the source of truth.  This module only computes a
model-facing snapshot: a stable summary of a closed history prefix followed
by the uncompressed tail.  A stale snapshot is rejected by its source hash.
"""

from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any

from pydantic import BaseModel, Field

from manage.workgroup.builtin_hooks import estimate_tokens
from manage.workgroup.history import (
    RunHistoryMessage,
    open_tool_call_ids,
    to_provider_messages,
)


CONTEXT_SUMMARY_MESSAGE_NAME = "workgroup_context_summary"
DEFAULT_CONTEXT_COMPRESSION_TRIGGER_TOKENS = 24000
DEFAULT_CONTEXT_COMPRESSION_BLOCKING_TRIGGER_TOKENS = 32000
DEFAULT_CONTEXT_COMPRESSION_KEEP_TOKENS = 8000
MAX_CONTEXT_SUMMARY_CHARS = 16000


class ActorContextSnapshot(BaseModel):
    """Durable model-context projection metadata; never replaces RunHistory."""

    run_id: str
    workgroup_id: str
    actor_id: str
    context_epoch: int = Field(ge=0, default=0)
    compressed_until_ordinal: int = Field(ge=0, default=0)
    compressed_until_timeline_seq: int = Field(ge=0, default=0)
    summary_content: str = ""
    summary_source_hash: str = ""
    updated_at: str


@dataclass(frozen=True)
class CompressionPlan:
    start: int
    end: int
    source_hash: str


def estimate_provider_messages(messages: list[dict[str, Any]]) -> int:
    """Estimate message tokens using the same rough weighting as Workgroup hooks."""
    if not messages:
        return 0
    encoded = json.dumps(messages, ensure_ascii=False, separators=(",", ":"))
    return max(1, int(estimate_tokens(encoded)))


def history_source_hash(messages: list[RunHistoryMessage], end: int) -> str:
    """Hash the exact raw prefix, including tool metadata, for stale checking."""
    raw = json.dumps(
        [message.model_dump(mode="json") for message in messages[:end]],
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")
    return "sha256:" + hashlib.sha256(raw).hexdigest()


def snapshot_is_current(
    snapshot: ActorContextSnapshot | None,
    history: list[RunHistoryMessage],
) -> bool:
    if snapshot is None:
        return False
    end = int(snapshot.compressed_until_ordinal or 0)
    if end <= 0 or end > len(history) or not snapshot.summary_content.strip():
        return False
    return history_source_hash(history, end) == snapshot.summary_source_hash


def _closed_prefix_ends(
    history: list[RunHistoryMessage],
    start: int,
    *,
    max_end: int,
) -> list[int]:
    """Return prefix ends that do not split an assistant tool-call batch."""
    ends: list[int] = []
    for end in range(start + 1, min(len(history), max_end) + 1):
        if not open_tool_call_ids(history[:end]):
            ends.append(end)
    return ends


def build_compression_plan(
    history: list[RunHistoryMessage],
    *,
    snapshot: ActorContextSnapshot | None,
    trigger_tokens: int,
    keep_tokens: int,
) -> CompressionPlan | None:
    """Choose the largest safe prefix while retaining a recent tail.

    Compression is deliberately disabled while any tool call is open.  The
    caller additionally blocks pending HITL states before invoking this plan.
    """
    if trigger_tokens <= 0 or keep_tokens < 0 or not history:
        return None
    if open_tool_call_ids(history):
        return None

    start = 0
    if snapshot_is_current(snapshot, history):
        start = int(snapshot.compressed_until_ordinal)
    tail = to_provider_messages(history[start:])
    if estimate_provider_messages(tail) < trigger_tokens:
        return None

    target = max(0, estimate_provider_messages(tail) - keep_tokens)
    if target <= 0:
        return None

    # Keep at least one recent raw message.  In particular, a large current
    # user message must not disappear from the primary request entirely.
    candidates = _closed_prefix_ends(history, start, max_end=len(history) - 1)
    chosen: int | None = None
    for end in candidates:
        removed = estimate_provider_messages(to_provider_messages(history[start:end]))
        if removed <= target:
            chosen = end
        else:
            break
    if chosen is None or chosen <= start:
        return None
    return CompressionPlan(
        start=start,
        end=chosen,
        source_hash=history_source_hash(history, chosen),
    )


def build_summary_request(
    history: list[RunHistoryMessage],
    plan: CompressionPlan,
    *,
    previous_summary: str = "",
    actor_label: str,
) -> list[dict[str, Any]]:
    """Build an isolated sidecar request; it is never appended to RunHistory."""
    chunk = to_provider_messages(history[plan.start : plan.end])
    payload = json.dumps(chunk, ensure_ascii=False, separators=(",", ":"))
    previous = previous_summary.strip() or "(none)"
    return [
        {
            "role": "system",
            "content": (
                "You summarize an agent's prior workgroup context. "
                "Return only a concise factual summary, with no preamble and no tool calls. "
                "Preserve user constraints, decisions, completed work, member task outcomes, "
                "files/artifacts, unresolved blockers, and important unknowns. "
                "Do not invent goals or state that a task is complete without evidence."
            ),
        },
        {
            "role": "user",
            "name": CONTEXT_SUMMARY_MESSAGE_NAME,
            "content": (
                f"Actor: {actor_label}\n"
                f"Previous compact summary:\n{previous}\n\n"
                "Newly compressible closed history (JSON):\n"
                f"{payload}"
            ),
        },
    ]


def normalize_summary(text: str) -> str:
    value = str(text or "").strip()
    if len(value) <= MAX_CONTEXT_SUMMARY_CHARS:
        return value
    head = MAX_CONTEXT_SUMMARY_CHARS * 2 // 3
    tail = MAX_CONTEXT_SUMMARY_CHARS - head
    return value[:head].rstrip() + " … " + value[-tail:].lstrip()


def make_snapshot(
    *,
    run_id: str,
    workgroup_id: str,
    actor_id: str,
    history: list[RunHistoryMessage],
    plan: CompressionPlan,
    summary: str,
    previous: ActorContextSnapshot | None,
    timeline_seq: int,
) -> ActorContextSnapshot:
    return ActorContextSnapshot(
        run_id=run_id,
        workgroup_id=workgroup_id,
        actor_id=actor_id,
        context_epoch=(previous.context_epoch + 1) if previous else 1,
        compressed_until_ordinal=plan.end,
        compressed_until_timeline_seq=max(0, int(timeline_seq or 0)),
        summary_content=normalize_summary(summary),
        summary_source_hash=plan.source_hash,
        updated_at=datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z"),
    )


def context_messages(
    history: list[RunHistoryMessage],
    snapshot: ActorContextSnapshot | None,
) -> list[dict[str, Any]]:
    """Project a current snapshot, falling back to the complete history."""
    if not snapshot_is_current(snapshot, history):
        return to_provider_messages(history)
    assert snapshot is not None
    summary = {
        "role": "user",
        "name": CONTEXT_SUMMARY_MESSAGE_NAME,
        "content": (
            "以下是此前工作组协作历史的压缩摘要。它只用于恢复上下文；"
            "后面的消息是未压缩的近期历史。\n" + snapshot.summary_content
        ),
    }
    return [summary, *to_provider_messages(history[snapshot.compressed_until_ordinal :])]
