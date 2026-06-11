"""Register Center 侧 A2A 调用近期摘要（内存环形缓冲）。"""

from __future__ import annotations

import os
import threading
import time
from collections import deque
from typing import Any, Literal

from pydantic import BaseModel, Field


class A2ARecentEntry(BaseModel):
    trace_id: str
    operation: Literal["broadcast", "relay"]
    delivery_mode: Literal["direct", "relay"]
    caller_groups: list[str] = Field(default_factory=list)
    target_agent_id: str | None = None
    target_session_id: str | None = None
    started_at_unix: int
    finished_at_unix: int
    latency_ms: int
    final_state: str
    error_summary: str | None = None


class A2ARecentLog:
    def __init__(self) -> None:
        self._lock = threading.RLock()
        self._entries: deque[A2ARecentEntry] = deque()
        max_raw = os.environ.get("REGISTER_CENTER_A2A_RECENT_MAX_ENTRIES", "500").strip()
        try:
            self._max_entries = max(1, int(max_raw))
        except ValueError:
            self._max_entries = 500

    def record(
        self,
        *,
        trace_id: str,
        operation: Literal["broadcast", "relay"],
        delivery_mode: Literal["direct", "relay"],
        caller_groups: list[str] | None = None,
        target_agent_id: str | None = None,
        target_session_id: str | None = None,
        started_at_unix: int,
        finished_at_unix: int | None = None,
        final_state: str,
        error_summary: str | None = None,
    ) -> A2ARecentEntry:
        finished = finished_at_unix if finished_at_unix is not None else int(time.time())
        latency_ms = max(0, int((finished - started_at_unix) * 1000))
        entry = A2ARecentEntry(
            trace_id=trace_id,
            operation=operation,
            delivery_mode=delivery_mode,
            caller_groups=list(caller_groups or []),
            target_agent_id=target_agent_id,
            target_session_id=target_session_id,
            started_at_unix=started_at_unix,
            finished_at_unix=finished,
            latency_ms=latency_ms,
            final_state=final_state,
            error_summary=error_summary,
        )
        with self._lock:
            self._entries.append(entry)
            while len(self._entries) > self._max_entries:
                self._entries.popleft()
        return entry

    def list_recent(self, *, limit: int = 100) -> list[A2ARecentEntry]:
        capped = max(1, min(limit, self._max_entries))
        with self._lock:
            items = list(self._entries)
        return list(reversed(items[-capped:]))
