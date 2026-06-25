from __future__ import annotations

import getpass
import json
import os
import urllib.error
import urllib.request


def fetch_node_health(api_base: str, *, timeout_seconds: float = 5.0) -> dict[str, str] | None:
    """同步 GET /health；成功且 status=ok 时返回摘要，否则 None。"""
    url = f"{api_base.rstrip('/')}/health"
    try:
        with urllib.request.urlopen(url, timeout=timeout_seconds) as resp:
            if resp.status != 200:
                return None
            raw = resp.read()
    except (OSError, urllib.error.URLError, TimeoutError):
        return None
    try:
        data = json.loads(raw.decode())
    except (UnicodeDecodeError, json.JSONDecodeError):
        return None
    if not isinstance(data, dict) or str(data.get("status") or "").strip().lower() != "ok":
        return None
    return {
        "status": str(data.get("status") or ""),
        "agent_id": str(data.get("agent_id") or ""),
        "version": str(data.get("version") or ""),
    }


def get_cli_username() -> str:
    """解析当前系统用户名，供 TUI 欢迎区展示。

    逻辑：
    1. 优先 `getpass.getuser()`；
    2. 失败时回退环境变量 `USER` / `USERNAME`；
    3. 仍无则返回 `guest`。
    """
    try:
        name = getpass.getuser().strip()
        if name:
            return name
    except OSError:
        pass
    for key in ("USER", "USERNAME"):
        value = os.environ.get(key, "").strip()
        if value:
            return value
    return "guest"
