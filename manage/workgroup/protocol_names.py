"""Provider-safe protocol `name` 约定（对齐 workgroup-d05 §6 / 设计正文 §6.3）。"""

from __future__ import annotations

RESERVED = frozenset({"leader", "date", "human", "workgroup_assign"})


def protocol_name_for_actor(actor_id: str) -> str:
    aid = (actor_id or "").strip()
    if not aid:
        return "human"
    if aid == "leader":
        return "leader"
    if aid.startswith("mb_"):
        return f"member_{aid}"
    if aid.startswith("human_"):
        return aid
    if aid.startswith("hu_"):
        return f"human_{aid}"
    return f"human_{aid}"


def is_reserved_protocol_name(name: str) -> bool:
    return (name or "").strip() in RESERVED
