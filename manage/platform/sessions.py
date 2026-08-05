"""Manage Console 浏览器会话（cookie）。"""

from __future__ import annotations

import secrets
import threading
import time
from dataclasses import dataclass
from typing import Literal

SESSION_COOKIE = "dagents_manage_session"
DEFAULT_TTL_SECONDS = 7 * 24 * 3600


@dataclass(frozen=True)
class SessionRecord:
    session_id: str
    kind: Literal["admin", "node"]
    subject: str  # admin username 或 node_id
    discovery_groups: tuple[str, ...]
    created_at_unix: int
    expires_at_unix: int

    @property
    def expired(self) -> bool:
        return int(time.time()) >= self.expires_at_unix


class SessionStore:
    def __init__(self, *, ttl_seconds: int = DEFAULT_TTL_SECONDS) -> None:
        self._ttl = max(60, int(ttl_seconds))
        self._lock = threading.RLock()
        self._sessions: dict[str, SessionRecord] = {}

    def create(
        self,
        *,
        kind: Literal["admin", "node"],
        subject: str,
        discovery_groups: list[str] | None = None,
    ) -> SessionRecord:
        sid = secrets.token_urlsafe(32)
        now = int(time.time())
        groups = tuple(g for g in (discovery_groups or []) if str(g).strip())
        if kind == "admin":
            groups = ("*",)
        rec = SessionRecord(
            session_id=sid,
            kind=kind,
            subject=str(subject or "").strip(),
            discovery_groups=groups,
            created_at_unix=now,
            expires_at_unix=now + self._ttl,
        )
        with self._lock:
            self._sessions[sid] = rec
        return rec

    def get(self, session_id: str) -> SessionRecord | None:
        sid = str(session_id or "").strip()
        if not sid:
            return None
        with self._lock:
            rec = self._sessions.get(sid)
            if rec is None:
                return None
            if rec.expired:
                self._sessions.pop(sid, None)
                return None
            return rec

    def revoke(self, session_id: str) -> None:
        sid = str(session_id or "").strip()
        if not sid:
            return
        with self._lock:
            self._sessions.pop(sid, None)

    def purge_expired(self) -> int:
        now = int(time.time())
        with self._lock:
            dead = [k for k, v in self._sessions.items() if v.expires_at_unix <= now]
            for k in dead:
                self._sessions.pop(k, None)
            return len(dead)
