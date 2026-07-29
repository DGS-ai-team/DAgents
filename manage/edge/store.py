"""Edge session 内存存储（P2；后续可落库）。"""

from __future__ import annotations

import secrets
import threading
import time
from dataclasses import dataclass, field


@dataclass
class EdgeSession:
    session_id: str
    owner_node_id: str
    home_node_id: str
    agent_id: str
    scopes: list[str]
    expires_at_unix: int
    created_at_unix: int = field(default_factory=lambda: int(time.time()))

    def alive(self, now: int | None = None) -> bool:
        ts = int(time.time()) if now is None else now
        return ts < self.expires_at_unix


class EdgeSessionStore:
    def __init__(self) -> None:
        self._lock = threading.RLock()
        self._sessions: dict[str, EdgeSession] = {}

    def create(
        self,
        *,
        owner_node_id: str,
        home_node_id: str,
        agent_id: str,
        scopes: list[str],
        ttl_seconds: int,
    ) -> EdgeSession:
        now = int(time.time())
        sid = "edge_" + secrets.token_urlsafe(18)
        sess = EdgeSession(
            session_id=sid,
            owner_node_id=owner_node_id.strip(),
            home_node_id=home_node_id.strip(),
            agent_id=agent_id.strip(),
            scopes=list(scopes),
            expires_at_unix=now + max(60, int(ttl_seconds)),
            created_at_unix=now,
        )
        with self._lock:
            self._gc_locked(now)
            self._sessions[sid] = sess
        return sess

    def get(self, session_id: str) -> EdgeSession | None:
        sid = (session_id or "").strip()
        if not sid:
            return None
        now = int(time.time())
        with self._lock:
            self._gc_locked(now)
            sess = self._sessions.get(sid)
            if sess is None or not sess.alive(now):
                self._sessions.pop(sid, None)
                return None
            return sess

    def _gc_locked(self, now: int) -> None:
        dead = [k for k, v in self._sessions.items() if not v.alive(now)]
        for k in dead:
            self._sessions.pop(k, None)
