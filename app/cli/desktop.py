"""Shell localhost Desktop API helpers (F-ND2 / F-X8)."""

from __future__ import annotations

import aiohttp

DESKTOP_API_BASE = "http://127.0.0.1:18767"


async def get_desktop_update(session: aiohttp.ClientSession | None = None) -> dict:
    """GET /v1/desktop/update from running Shell."""
    close = session is None
    if session is None:
        session = aiohttp.ClientSession()
    try:
        async with session.get(f"{DESKTOP_API_BASE}/v1/desktop/update", headers={"Accept": "application/json"}) as resp:
            resp.raise_for_status()
            data = await resp.json()
            if not isinstance(data, dict):
                raise TypeError("desktop update response is not a JSON object")
            return data
    finally:
        if close:
            await session.close()


async def resolve_agent_update(api_client) -> dict:
    """Query Node update; follow delegate=shell to Shell desktop API when available."""
    status = await api_client.get_agent_update()
    if str(status.get("delegate") or "").strip().lower() != "shell":
        return status
    try:
        return await get_desktop_update()
    except Exception:
        return status
