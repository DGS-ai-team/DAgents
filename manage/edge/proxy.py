"""Manage → home Node 的 Edge 反代（流式）。"""

from __future__ import annotations

from typing import Any, Iterable

import httpx
from fastapi import HTTPException, Request, Response
from fastapi.responses import StreamingResponse

from manage.platform.auth import lookup_node_token

HOP_BY_HOP = {
    "connection",
    "keep-alive",
    "proxy-authenticate",
    "proxy-authorization",
    "te",
    "trailers",
    "transfer-encoding",
    "upgrade",
    "host",
    "content-length",
}


def join_url(base_url: str, path: str) -> str:
    base = base_url.rstrip("/")
    if not path.startswith("/"):
        path = "/" + path
    return base + path


def path_allowed(path: str, *, agent_id: str, scopes: Iterable[str]) -> bool:
    """按 scope 限制可转发路径前缀。"""
    p = path if path.startswith("/") else "/" + path
    scope_set = {str(s).strip().lower() for s in scopes}
    agent_prefix = f"/v1/agents/{agent_id}"
    # screen 路径必须显式 screen 权限（不因 agent 前缀放行）
    if p.startswith(f"{agent_prefix}/screen") or p.startswith("/v1/screen/"):
        return "screen" in scope_set or "screen:view" in scope_set
    if "agent" in scope_set or "agent:read" in scope_set or "agent:write" in scope_set:
        if p == agent_prefix or p.startswith(agent_prefix + "/"):
            return True
    if "messages" in scope_set and (p == "/v1/messages" or p.startswith("/v1/messages?")):
        return True
    if "streams" in scope_set and (p == "/v1/streams" or p.startswith("/v1/streams?")):
        return True
    return False


async def forward_to_home(
    *,
    request: Request,
    base_url: str,
    target_path: str,
    home_node_id: str,
    owner_node_id: str,
    edge_session_id: str,
    body: bytes | None = None,
) -> Response:
    url = join_url(base_url, target_path)
    if request.url.query:
        url = url + ("&" if "?" in url else "?") + request.url.query

    headers: dict[str, str] = {
        "Accept": request.headers.get("accept") or "*/*",
        "x-dagents-agent-id": home_node_id,
        "x-dagents-edge-audience": home_node_id,
        "x-dagents-edge-session": edge_session_id,
        "x-dagents-owner-node-id": owner_node_id,
    }
    token = lookup_node_token(home_node_id)
    if token:
        headers["x-dagents-a2a-token"] = token
    ctype = request.headers.get("content-type")
    if ctype:
        headers["Content-Type"] = ctype

    if body is None:
        body = await request.body()
    timeout = httpx.Timeout(connect=10.0, read=None, write=30.0, pool=10.0)
    try:
        client = httpx.AsyncClient(timeout=timeout)
        req = client.build_request(request.method, url, headers=headers, content=body or None)
        upstream = await client.send(req, stream=True)
    except httpx.HTTPError as exc:
        raise HTTPException(status_code=502, detail=f"home_unreachable: {exc}") from exc

    out_headers: dict[str, str] = {}
    for k, v in upstream.headers.multi_items():
        if k.lower() in HOP_BY_HOP:
            continue
        out_headers[k] = v

    async def body_iter():
        try:
            async for chunk in upstream.aiter_raw():
                yield chunk
        finally:
            await upstream.aclose()
            await client.aclose()

    return StreamingResponse(
        body_iter(),
        status_code=upstream.status_code,
        headers=out_headers,
        media_type=upstream.headers.get("content-type"),
    )


def sync_forward_smoke_headers(
    *,
    home_node_id: str,
    owner_node_id: str,
    edge_session_id: str,
) -> dict[str, Any]:
    """单测辅助：构造将发往 home 的头。"""
    headers: dict[str, Any] = {
        "x-dagents-agent-id": home_node_id,
        "x-dagents-edge-audience": home_node_id,
        "x-dagents-edge-session": edge_session_id,
        "x-dagents-owner-node-id": owner_node_id,
    }
    token = lookup_node_token(home_node_id)
    if token:
        headers["x-dagents-a2a-token"] = token
    return headers
