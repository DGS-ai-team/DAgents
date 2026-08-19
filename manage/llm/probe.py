"""OpenAI-compatible GET /models probe for Manage Console."""

from __future__ import annotations

import json
import urllib.error
import urllib.request
from typing import Any


def normalize_openai_base_url(base_url: str) -> str:
    base = (base_url or "").strip().rstrip("/")
    if not base:
        return ""
    if base.endswith("/chat/completions"):
        base = base[: -len("/chat/completions")]
    if not base.endswith("/v1") and "/v1/" not in base:
        # keep as-is; callers usually include /v1
        pass
    return base


def suggest_provider_from_base_url(base_url: str) -> str:
    lower = (base_url or "").strip().lower()
    if "deepseek.com" in lower:
        return "deepseek"
    if "dashscope.aliyuncs.com" in lower or ".maas.aliyuncs.com" in lower:
        return "qwen"
    if "api.openai.com" in lower or "openai.com" in lower:
        return "openai"
    if "bigmodel.cn" in lower or "api.z.ai" in lower:
        return "glm"
    if "minimaxi.com" in lower or "minimax.io" in lower:
        return "minimax"
    if "xiaomimimo.com" in lower or "mimo-v2.com" in lower:
        return "mimo"
    if "127.0.0.1" in lower or "localhost" in lower:
        return "vllm"
    return ""


def probe_models(base_url: str, api_key: str = "", *, timeout: float = 20.0) -> dict[str, Any]:
    """Request OpenAI-compatible GET {base}/models.

    Returns ``{"models": [{"id": "..."}], "suggested_provider": "..."}``.
    Raises ``ValueError`` on client/HTTP/parse failures.
    """
    base = normalize_openai_base_url(base_url)
    if not base:
        raise ValueError("base_url is required")
    endpoint = f"{base}/models"
    headers = {"Accept": "application/json"}
    key = (api_key or "").strip()
    if key:
        headers["Authorization"] = f"Bearer {key}"
    req = urllib.request.Request(endpoint, headers=headers, method="GET")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read(2 << 20)
            status = getattr(resp, "status", 200)
    except urllib.error.HTTPError as exc:
        body = ""
        try:
            body = exc.read(400).decode("utf-8", errors="replace").strip()
        except Exception:
            body = ""
        msg = body or (exc.reason or str(exc))
        if len(msg) > 400:
            msg = msg[:400] + "…"
        raise ValueError(f"HTTP {exc.code}: {msg}") from exc
    except urllib.error.URLError as exc:
        raise ValueError(f"请求 /models 失败: {exc.reason}") from exc
    except TimeoutError as exc:
        raise ValueError("请求 /models 超时") from exc

    if status < 200 or status >= 300:
        raise ValueError(f"HTTP {status}")

    try:
        parsed = json.loads(raw.decode("utf-8"))
    except json.JSONDecodeError as exc:
        raise ValueError("解析 /models 响应失败") from exc

    data = parsed.get("data") if isinstance(parsed, dict) else None
    if not isinstance(data, list):
        raise ValueError("/models 响应缺少 data 列表")

    models: list[dict[str, str]] = []
    seen: set[str] = set()
    for item in data:
        if not isinstance(item, dict):
            continue
        mid = str(item.get("id") or "").strip()
        if not mid or mid in seen:
            continue
        seen.add(mid)
        models.append({"id": mid})

    if not models:
        raise ValueError("/models 未返回可用模型")

    return {
        "models": models,
        "suggested_provider": suggest_provider_from_base_url(base),
    }
