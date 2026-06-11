"""Agent 登记记录的在线状态派生逻辑。"""

from __future__ import annotations

import os
from typing import Literal

AgentStatus = Literal["online", "offline", "expired"]


def offline_grace_seconds() -> int:
    raw = os.environ.get("REGISTER_CENTER_OFFLINE_GRACE_SECONDS", "86400").strip()
    try:
        return max(0, int(raw))
    except ValueError:
        return 86400


def derive_status(*, now_unix: int, expires_at_unix: int, grace_seconds: int | None = None) -> AgentStatus:
    grace = offline_grace_seconds() if grace_seconds is None else grace_seconds
    if now_unix < expires_at_unix:
        return "online"
    if grace <= 0:
        return "expired"
    if now_unix < expires_at_unix + grace:
        return "offline"
    return "expired"


def is_deliverable(*, now_unix: int, expires_at_unix: int, grace_seconds: int | None = None) -> bool:
    return derive_status(now_unix=now_unix, expires_at_unix=expires_at_unix, grace_seconds=grace_seconds) == "online"
