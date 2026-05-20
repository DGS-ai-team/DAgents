from __future__ import annotations

import time
from typing import Any
from urllib.parse import urljoin, urlparse

import httpx

from app.config.settings import get_settings
from app.harness.tools.agent_peer_common import DEFAULT_HTTP_TIMEOUT_SECONDS, a2a_auth_headers, stable_groups
from app.observability.metrics import record_a2a_operation

_AGENT_LIST_CACHE: list[dict[str, Any]] = []
_AGENT_LIST_CACHE_UPDATED_AT_UNIX_MS = 0
_TRANSIENT_READ_STATUS_CODES = {408, 429, 500, 502, 503, 504}


def http_read_retry_attempts() -> int:
    raw = getattr(get_settings(), "agent_peer_http_retry_attempts", 2)
    return max(1, min(5, int(raw)))


def get_with_retries(client: httpx.Client, url: str, **kwargs: Any) -> httpx.Response:
    attempts = http_read_retry_attempts()
    last_exc: Exception | None = None
    for attempt in range(attempts):
        try:
            resp = client.get(url, **kwargs)
            if resp.status_code not in _TRANSIENT_READ_STATUS_CODES or attempt == attempts - 1:
                return resp
            last_exc = httpx.HTTPStatusError("transient read status", request=resp.request, response=resp)
        except httpx.RequestError as exc:
            last_exc = exc
            if attempt == attempts - 1:
                raise
    if last_exc is not None:
        raise last_exc
    raise RuntimeError("A2A read retry failed without response")


def cache_agent_list(agents: list[dict[str, Any]]) -> None:
    by_id: dict[str, dict[str, Any]] = {}
    for item in agents:
        if not isinstance(item, dict):
            continue
        agent_id = str(item.get("agent_id") or "").strip()
        if not agent_id:
            continue
        by_id[agent_id] = dict(item)
    _AGENT_LIST_CACHE.clear()
    _AGENT_LIST_CACHE.extend(sorted(by_id.values(), key=lambda v: str(v.get("agent_id") or "")))
    global _AGENT_LIST_CACHE_UPDATED_AT_UNIX_MS
    _AGENT_LIST_CACHE_UPDATED_AT_UNIX_MS = int(time.time() * 1000)


def is_agent_list_cache_stale() -> bool:
    if not _AGENT_LIST_CACHE:
        return True
    settings = get_settings()
    ttl_seconds = max(1, int(settings.agent_peer_cache_ttl_seconds))
    now_ms = int(time.time() * 1000)
    ttl_ms = ttl_seconds * 1000
    return (now_ms - _AGENT_LIST_CACHE_UPDATED_AT_UNIX_MS) >= ttl_ms


def refresh_agent_list_for_visible_groups(visible_groups: list[str]) -> list[dict[str, Any]]:
    agents = discover_agents_by_groups(visible_groups)
    cache_agent_list(agents)
    return agents


def resolve_target_agent_from_cache(target_agent_id: str, visible_groups: list[str]) -> dict[str, Any] | None:
    target = target_agent_id.strip()
    visible = set(stable_groups(visible_groups))
    if not target or not visible:
        return None
    for item in _AGENT_LIST_CACHE:
        if str(item.get("agent_id") or "").strip() != target:
            continue
        groups = stable_groups(item.get("discovery_group") or [])
        if any(g in visible for g in groups):
            return item
    return None


def clear_agent_list_cache() -> None:
    _AGENT_LIST_CACHE.clear()
    global _AGENT_LIST_CACHE_UPDATED_AT_UNIX_MS
    _AGENT_LIST_CACHE_UPDATED_AT_UNIX_MS = 0


def resolve_registry_url() -> str:
    s = get_settings()
    return (s.registry_url or "").strip().rstrip("/")


def require_registry_url() -> str:
    registry_url = resolve_registry_url()
    if not registry_url:
        raise ValueError("未配置 REGISTRY_URL，无法进行 Agent 发现与转发")
    return registry_url


def discover_agents_by_groups(groups: list[str]) -> list[dict[str, Any]]:
    started = time.monotonic()
    final_groups = stable_groups(groups)
    if not final_groups:
        record_a2a_operation(
            component="agent_peer",
            operation="discover_agents",
            status="empty_groups",
            elapsed_seconds=time.monotonic() - started,
        )
        return []
    try:
        registry_url = require_registry_url()
        by_id: dict[str, dict[str, Any]] = {}
        with httpx.Client(timeout=DEFAULT_HTTP_TIMEOUT_SECONDS) as client:
            for group_id in final_groups:
                resp = get_with_retries(
                    client,
                    f"{registry_url}/v1/agents",
                    params={"discovery_group": group_id},
                    headers=a2a_auth_headers(),
                )
                resp.raise_for_status()
                body = resp.json()
                for item in body.get("agents", []):
                    agent_id = str(item.get("agent_id") or "").strip()
                    if not agent_id:
                        continue
                    by_id[agent_id] = item
        agents = sorted(by_id.values(), key=lambda item: str(item.get("agent_id") or ""))
        record_a2a_operation(
            component="agent_peer",
            operation="discover_agents",
            status="ok",
            elapsed_seconds=time.monotonic() - started,
        )
        return agents
    except Exception:
        record_a2a_operation(
            component="agent_peer",
            operation="discover_agents",
            status="error",
            elapsed_seconds=time.monotonic() - started,
        )
        raise


def attach_agent_card_summary(agent: dict[str, Any]) -> dict[str, Any]:
    started = time.monotonic()
    enriched = dict(agent)
    base = str(enriched.get("base_url") or "").strip().rstrip("/")
    parsed = urlparse(base if "://" in base else f"http://{base}") if base else None
    if parsed is None:
        access_host = ""
        access_port = None
    else:
        access_host = (parsed.hostname or "").strip()
        if parsed.port is not None:
            access_port = parsed.port
        elif parsed.scheme == "https":
            access_port = 443
        elif parsed.scheme == "http":
            access_port = 80
        else:
            access_port = None
    card_url = urljoin(f"{base}/", ".well-known/agent-card.json") if base else ""
    card_info: dict[str, Any] = {
        "access_url": base or None,
        "access_host": access_host or None,
        "access_port": access_port,
        "card_url": card_url or None,
        "card_payload": None,
        "error": None,
    }
    if not base:
        card_info["error"] = "base_url 为空，无法读取 agent card"
        enriched["agent_card"] = card_info
        record_a2a_operation(
            component="agent_peer",
            operation="agent_card",
            status="missing_base_url",
            elapsed_seconds=time.monotonic() - started,
        )
        return enriched
    try:
        with httpx.Client(timeout=DEFAULT_HTTP_TIMEOUT_SECONDS) as client:
            resp = get_with_retries(client, card_url)
            resp.raise_for_status()
            card = resp.json()
        card_info["card_payload"] = card
        card_info["error"] = None
        enriched["agent_card"] = card_info
        record_a2a_operation(
            component="agent_peer",
            operation="agent_card",
            status="ok",
            elapsed_seconds=time.monotonic() - started,
        )
        return enriched
    except Exception as exc:
        card_info["card_payload"] = None
        card_info["error"] = str(exc)
        enriched["agent_card"] = card_info
        record_a2a_operation(
            component="agent_peer",
            operation="agent_card",
            status="error",
            elapsed_seconds=time.monotonic() - started,
        )
        return enriched


def resolve_target_agent(target_agent_id: str) -> dict[str, Any]:
    s = get_settings()
    visible_groups = stable_groups(s.discovery_groups)
    if not visible_groups:
        raise ValueError("未配置 DISCOVERY_GROUPS，无法解析目标 Agent")
    if is_agent_list_cache_stale():
        try:
            refresh_agent_list_for_visible_groups(visible_groups)
        except Exception:
            pass
    cached = resolve_target_agent_from_cache(target_agent_id, visible_groups)
    if cached is not None:
        return cached
    agents = refresh_agent_list_for_visible_groups(visible_groups)
    for item in agents:
        if str(item.get("agent_id") or "").strip() == target_agent_id.strip():
            return item
    raise ValueError(f"在当前可见分组内未找到目标 Agent: {target_agent_id!r}")
