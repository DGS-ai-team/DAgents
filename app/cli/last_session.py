from __future__ import annotations

import json
import os
from pathlib import Path
from typing import Any

import yaml

from app.cli.config_file import resolve_config_path

_LAST_SESSION_FILE = "last_session.json"


def _runtime_dir(config_path: str | None) -> Path:
    resolved = resolve_config_path(config_path)
    if resolved:
        raw = Path(resolved).read_text(encoding="utf-8")
        expanded = os.path.expandvars(raw)
        data = yaml.safe_load(expanded)
        if isinstance(data, dict):
            fs_root = str(data.get("fs_root") or "").strip() or "./.runtime"
            return Path(fs_root.rstrip("/"))
    return Path("./.runtime")


def last_session_store_path(config_path: str | None = None) -> Path:
    """Client 上次 session 落盘路径（`<runtime>/client/last_session.json`）。"""
    return _runtime_dir(config_path) / "client" / _LAST_SESSION_FILE


def _normalize_api_base(api_base: str) -> str:
    return str(api_base or "").strip().rstrip("/")


def save_last_session(
    api_base: str,
    session_id: str,
    *,
    config_path: str | None = None,
) -> None:
    """记录当前 endpoint 下最近使用的 session_id。"""
    sid = str(session_id or "").strip()
    endpoint = _normalize_api_base(api_base)
    if not sid or not endpoint:
        return
    path = last_session_store_path(config_path)
    path.parent.mkdir(parents=True, exist_ok=True)
    payload = {"api_base": endpoint, "session_id": sid}
    path.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def load_last_session(
    api_base: str,
    *,
    config_path: str | None = None,
) -> str | None:
    """读取与当前 endpoint 匹配的上次 session_id；不匹配或文件损坏时返回 None。"""
    endpoint = _normalize_api_base(api_base)
    if not endpoint:
        return None
    path = last_session_store_path(config_path)
    if not path.is_file():
        return None
    try:
        raw = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None
    if not isinstance(raw, dict):
        return None
    stored_api = _normalize_api_base(str(raw.get("api_base") or ""))
    sid = str(raw.get("session_id") or "").strip()
    if stored_api != endpoint or not sid:
        return None
    return sid


def read_last_session_record(config_path: str | None = None) -> dict[str, Any] | None:
    """读取落盘记录（诊断用）；不校验 endpoint。"""
    path = last_session_store_path(config_path)
    if not path.is_file():
        return None
    try:
        raw = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None
    return raw if isinstance(raw, dict) else None
