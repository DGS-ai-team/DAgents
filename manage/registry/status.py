"""Registry Agent 在线状态派生。"""

from __future__ import annotations

from typing import Literal

AgentStatus = Literal["online", "offline", "expired"]


def derive_status(*, now_unix: int, expires_at_unix: int, grace_seconds: int = 86400) -> AgentStatus:
    grace = max(0, grace_seconds)
    if now_unix < expires_at_unix:
        return "online"
    if grace <= 0:
        return "expired"
    if now_unix < expires_at_unix + grace:
        return "offline"
    return "expired"


def is_deliverable(*, now_unix: int, expires_at_unix: int, grace_seconds: int = 86400) -> bool:
    return derive_status(now_unix=now_unix, expires_at_unix=expires_at_unix, grace_seconds=grace_seconds) == "online"
