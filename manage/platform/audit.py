"""Manage 审计事件（内存环形缓冲；M1 仅 Registry 操作）。"""

from __future__ import annotations

import json
import os
import threading
import time
from collections import deque
from pathlib import Path
from typing import Any

from pydantic import BaseModel, Field


class AuditEvent(BaseModel):
    at_unix: int
    actor: str
    action: str
    target_agent_id: str | None = None
    discovery_group: str | None = None
    detail: dict[str, Any] = Field(default_factory=dict)


class AuditLog:
    def __init__(self, *, max_entries: int = 500) -> None:
        self._lock = threading.RLock()
        self._entries: deque[AuditEvent] = deque()
        self._max_entries = max(1, max_entries)
        path_raw = os.environ.get("MANAGE_AUDIT_PATH", "").strip()
        self._path = Path(path_raw) if path_raw else None

    def record(
        self,
        *,
        actor: str,
        action: str,
        target_agent_id: str | None = None,
        discovery_group: str | None = None,
        detail: dict[str, Any] | None = None,
    ) -> AuditEvent:
        event = AuditEvent(
            at_unix=int(time.time()),
            actor=actor,
            action=action,
            target_agent_id=target_agent_id,
            discovery_group=discovery_group,
            detail=detail or {},
        )
        with self._lock:
            self._entries.append(event)
            while len(self._entries) > self._max_entries:
                self._entries.popleft()
            if self._path is not None:
                self._path.parent.mkdir(parents=True, exist_ok=True)
                with self._path.open("a", encoding="utf-8") as handle:
                    handle.write(json.dumps(event.model_dump(mode="json"), ensure_ascii=False) + "\n")
        return event

    def list_recent(self, *, limit: int = 100) -> list[AuditEvent]:
        capped = max(1, min(limit, self._max_entries))
        with self._lock:
            items = list(self._entries)
        return list(reversed(items[-capped:]))
