"""Register Center 审计事件（内存环形缓冲 + 可选 JSONL）。"""

from __future__ import annotations

import json
import os
import threading
import time
from collections import deque
from dataclasses import dataclass
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


@dataclass
class _AuditConfig:
    max_entries: int
    path: Path | None


class AuditLog:
    def __init__(self) -> None:
        self._lock = threading.RLock()
        self._entries: deque[AuditEvent] = deque()
        self._config = self._load_config()

    def _load_config(self) -> _AuditConfig:
        max_raw = os.environ.get("REGISTER_CENTER_AUDIT_MAX_ENTRIES", "500").strip()
        try:
            max_entries = max(1, int(max_raw))
        except ValueError:
            max_entries = 500
        path_raw = os.environ.get("REGISTER_CENTER_AUDIT_PATH", "").strip()
        path = Path(path_raw) if path_raw else None
        return _AuditConfig(max_entries=max_entries, path=path)

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
            while len(self._entries) > self._config.max_entries:
                self._entries.popleft()
            self._append_jsonl_locked(event)
        return event

    def _append_jsonl_locked(self, event: AuditEvent) -> None:
        if self._config.path is None:
            return
        self._config.path.parent.mkdir(parents=True, exist_ok=True)
        with self._config.path.open("a", encoding="utf-8") as handle:
            handle.write(json.dumps(event.model_dump(mode="json"), ensure_ascii=False) + "\n")

    def list_recent(self, *, limit: int = 100) -> list[AuditEvent]:
        capped = max(1, min(limit, self._config.max_entries))
        with self._lock:
            items = list(self._entries)
        return list(reversed(items[-capped:]))
