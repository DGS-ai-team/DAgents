"""Register Center API 鉴权与角色模型。"""

from __future__ import annotations

import hmac
import json
import os
from dataclasses import dataclass
from typing import Literal

from fastapi import HTTPException, Request

_A2A_TOKEN_HEADER = "x-dagents-a2a-token"


@dataclass(frozen=True)
class AuthContext:
    token_id: str
    role: Literal["admin", "member"]
    discovery_groups: list[str]

    @property
    def is_admin(self) -> bool:
        return self.role == "admin"

    def allows_discovery_group(self, group: str) -> bool:
        if self.is_admin or "*" in self.discovery_groups:
            return True
        return group in self.discovery_groups

    def requires_group_on_list(self) -> bool:
        return not self.is_admin


def _shared_token() -> str:
    return os.environ.get("AGENT_PEER_SHARED_TOKEN", "").strip()


def _load_token_entries() -> list[dict[str, object]]:
    raw = os.environ.get("REGISTER_CENTER_TOKENS", "").strip()
    if not raw:
        return []
    try:
        parsed = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise RuntimeError("REGISTER_CENTER_TOKENS 不是合法 JSON") from exc
    if not isinstance(parsed, list):
        raise RuntimeError("REGISTER_CENTER_TOKENS 必须是 JSON 数组")
    return [item for item in parsed if isinstance(item, dict)]


def _normalize_groups(value: object) -> list[str]:
    if isinstance(value, str):
        items = [value]
    elif isinstance(value, list):
        items = value
    else:
        return []
    result: list[str] = []
    seen: set[str] = set()
    for item in items:
        if not isinstance(item, str):
            continue
        cleaned = item.strip()
        if not cleaned or cleaned in seen:
            continue
        seen.add(cleaned)
        result.append(cleaned)
    return result


def _extract_request_token(request: Request) -> str:
    header_token = (request.headers.get(_A2A_TOKEN_HEADER) or "").strip()
    if header_token:
        return header_token
    auth = (request.headers.get("authorization") or "").strip()
    if auth.lower().startswith("bearer "):
        return auth[7:].strip()
    return ""


def authenticate(request: Request) -> AuthContext:
    """校验请求 token 并返回调用方上下文。

    - 未配置任何 token：开放访问（admin）。
    - 仅 AGENT_PEER_SHARED_TOKEN：匹配则 admin。
    - REGISTER_CENTER_TOKENS：按条目匹配 member/admin。
    """

    entries = _load_token_entries()
    shared = _shared_token()
    if not entries and not shared:
        return AuthContext(token_id="anonymous", role="admin", discovery_groups=["*"])

    actual = _extract_request_token(request)
    if not actual:
        raise HTTPException(status_code=401, detail="invalid A2A token")

    for entry in entries:
        token_value = str(entry.get("token") or entry.get("secret") or "").strip()
        if not token_value or not hmac.compare_digest(actual, token_value):
            continue
        token_id = str(entry.get("id") or "token").strip() or "token"
        role_raw = str(entry.get("role") or "member").strip().lower()
        role: Literal["admin", "member"] = "admin" if role_raw == "admin" else "member"
        groups = _normalize_groups(entry.get("discovery_groups"))
        if role == "admin":
            groups = ["*"]
        elif not groups:
            raise HTTPException(status_code=500, detail="member token 缺少 discovery_groups")
        return AuthContext(token_id=token_id, role=role, discovery_groups=groups)

    if shared and hmac.compare_digest(actual, shared):
        return AuthContext(token_id="shared", role="admin", discovery_groups=["*"])

    raise HTTPException(status_code=401, detail="invalid A2A token")


def require_admin(auth: AuthContext) -> None:
    if not auth.is_admin:
        raise HTTPException(status_code=403, detail="admin role required")
