from __future__ import annotations

DEBUG_PORT_RANGE = 200


def allocate_debug_port(
    session_key: str,
    *,
    base_port: int,
    used_ports: set[int] | None = None,
    cdp_url: str = "",
) -> int:
    """为 session 分配 remote-debugging-port；attach 模式固定返回 base_port。"""
    if (cdp_url or "").strip():
        return int(base_port or 9222)
    base = int(base_port or 9222)
    used = used_ports or set()
    preferred = base + (abs(hash(session_key)) % DEBUG_PORT_RANGE)
    for i in range(DEBUG_PORT_RANGE):
        port = base + ((preferred - base + i) % DEBUG_PORT_RANGE)
        if port not in used:
            return port
    raise RuntimeError("no free debug port in range")
