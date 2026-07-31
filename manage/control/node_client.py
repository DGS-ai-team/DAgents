"""Manage → home Node 的 Placement HTTP 客户端（D5：仅保留 DELETE）。"""

from __future__ import annotations

from typing import Any

import httpx
from fastapi import HTTPException

from manage.platform.auth import lookup_node_token


def _join_url(base_url: str, path: str) -> str:
    base = base_url.rstrip("/")
    if not path.startswith("/"):
        path = "/" + path
    return base + path


def call_home_delete_agent(
    *,
    base_url: str,
    home_node_id: str,
    agent_id: str,
    owner_node_id: str,
    timeout_seconds: float = 30.0,
) -> dict[str, Any]:
    headers = {
        "Accept": "application/json",
        "x-dagents-placement-control": "1",
        "x-dagents-agent-id": home_node_id,
        "x-dagents-owner-node-id": owner_node_id,
    }
    token = lookup_node_token(home_node_id)
    if token:
        headers["x-dagents-a2a-token"] = token
    url = _join_url(base_url, f"/v1/internal/placement/agents/{agent_id}")
    try:
        with httpx.Client(timeout=timeout_seconds) as client:
            resp = client.delete(url, headers=headers)
    except httpx.HTTPError as exc:
        raise HTTPException(status_code=502, detail=f"home_unreachable: {exc}") from exc
    # home 已 D0.9/D5 对内部 placement DELETE 返回 410/404：视为远端实例已不存在。
    if resp.status_code in {404, 410}:
        return {"ok": True, "agent_id": agent_id, "home_deleted": False, "already_gone": True}
    if resp.status_code >= 400:
        detail: Any
        try:
            detail = resp.json()
        except Exception:
            detail = resp.text
        raise HTTPException(status_code=502, detail={"home_status": resp.status_code, "home_body": detail})
    try:
        data = resp.json()
    except Exception:
        data = {"ok": True, "agent_id": agent_id}
    if not isinstance(data, dict):
        data = {"ok": True, "agent_id": agent_id}
    data.setdefault("home_deleted", True)
    return data
