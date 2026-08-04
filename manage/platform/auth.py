"""Manage API 鉴权与角色模型。"""

from __future__ import annotations

import hmac
import json
import os
from dataclasses import dataclass
from typing import Literal

from fastapi import HTTPException, Request

from manage.platform.sessions import SESSION_COOKIE, SessionRecord, SessionStore

TOKEN_HEADER = "x-dagents-a2a-token"
AGENT_ID_HEADER = "x-dagents-agent-id"


@dataclass(frozen=True)
class AuthContext:
    token_id: str
    role: Literal["admin", "member", "node"]
    discovery_groups: list[str]
    agent_id: str | None = None
    session_kind: Literal["admin", "node"] | None = None

    @property
    def is_admin(self) -> bool:
        return self.role == "admin"

    @property
    def is_node(self) -> bool:
        return self.role == "node"

    def allows_discovery_group(self, group: str) -> bool:
        if self.is_admin or "*" in self.discovery_groups:
            return True
        return group in self.discovery_groups

    def allows_resource_groups(self, allowed_groups: list[str]) -> bool:
        """资源（如 LLM 配置）按 `allowed_groups` 限定可见/可用范围：空 = 全部可见；
        非空时调用方 `discovery_groups` 须与之有交集（admin / `*` 始终可见）。
        与 Registry `discovery_group` 同一命名空间，仅作 Manage 侧可见性约束。"""
        if not allowed_groups:
            return True
        return any(self.allows_discovery_group(group) for group in allowed_groups)

    def requires_group_on_list(self) -> bool:
        if self.is_admin or "*" in self.discovery_groups:
            return False
        return True


def _shared_token() -> str:
    return os.environ.get("MANAGE_SHARED_TOKEN", "").strip()


def _load_token_entries() -> list[dict[str, object]]:
    raw = os.environ.get("MANAGE_TOKENS", "").strip()
    if not raw:
        return []
    try:
        parsed = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise RuntimeError("MANAGE_TOKENS 不是合法 JSON") from exc
    if not isinstance(parsed, list):
        raise RuntimeError("MANAGE_TOKENS 必须是 JSON 数组")
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


def extract_request_token(request: Request) -> str:
    header_token = (request.headers.get(TOKEN_HEADER) or "").strip()
    if header_token:
        return header_token
    auth = (request.headers.get("authorization") or "").strip()
    if auth.lower().startswith("bearer "):
        return auth[7:].strip()
    return ""


def extract_agent_id(request: Request) -> str:
    return (request.headers.get(AGENT_ID_HEADER) or "").strip()


def is_open_mode() -> bool:
    """未配置 MANAGE_TOKENS / MANAGE_SHARED_TOKEN 时为开放模式（暂不做角色鉴权）。"""
    return not _load_token_entries() and not _shared_token()


def default_admin_username() -> str:
    return os.environ.get("MANAGE_ADMIN_USERNAME", "admin").strip() or "admin"


def default_admin_password() -> str:
    return os.environ.get("MANAGE_ADMIN_PASSWORD", "admin").strip() or "admin"


def verify_admin_password(username: str, password: str) -> bool:
    want_user = default_admin_username()
    want_pass = default_admin_password()
    user_ok = hmac.compare_digest(str(username or "").strip(), want_user)
    pass_ok = hmac.compare_digest(str(password or ""), want_pass)
    return user_ok and pass_ok


def resolve_session(request: Request) -> SessionRecord | None:
    store: SessionStore | None = getattr(request.app.state, "session_store", None)
    if store is None:
        return None
    raw = (request.cookies.get(SESSION_COOKIE) or "").strip()
    if not raw:
        return None
    return store.get(raw)


def auth_from_session(rec: SessionRecord) -> AuthContext:
    if rec.kind == "admin":
        return AuthContext(
            token_id=f"session:admin:{rec.subject}",
            role="admin",
            discovery_groups=["*"],
            agent_id=None,
            session_kind="admin",
        )
    groups = list(rec.discovery_groups) if rec.discovery_groups else ["*"]
    return AuthContext(
        token_id=f"session:node:{rec.subject}",
        role="member",
        discovery_groups=groups,
        agent_id=rec.subject,
        session_kind="node",
    )


def authenticate(request: Request) -> AuthContext:
    session = resolve_session(request)
    if session is not None:
        return auth_from_session(session)

    entries = _load_token_entries()
    shared = _shared_token()
    if not entries and not shared:
        return AuthContext(token_id="anonymous", role="admin", discovery_groups=["*"])

    actual = extract_request_token(request)
    if not actual:
        raise HTTPException(status_code=401, detail="invalid token")

    for entry in entries:
        token_value = str(entry.get("token") or entry.get("secret") or "").strip()
        if not token_value or not hmac.compare_digest(actual, token_value):
            continue
        token_id = str(entry.get("id") or "token").strip() or "token"
        role_raw = str(entry.get("role") or "member").strip().lower()
        if role_raw == "admin":
            role: Literal["admin", "member", "node"] = "admin"
            groups = ["*"]
        elif role_raw == "node":
            role = "node"
            groups = _normalize_groups(entry.get("discovery_groups"))
            agent_id = str(entry.get("agent_id") or "").strip() or None
            return AuthContext(token_id=token_id, role=role, discovery_groups=groups, agent_id=agent_id)
        else:
            role = "member"
            groups = _normalize_groups(entry.get("discovery_groups"))
            if not groups:
                raise HTTPException(status_code=500, detail="member token 缺少 discovery_groups")
        return AuthContext(token_id=token_id, role=role, discovery_groups=groups)

    if shared and hmac.compare_digest(actual, shared):
        return AuthContext(token_id="shared", role="admin", discovery_groups=["*"])

    raise HTTPException(status_code=401, detail="invalid token")


def ensure_node_identity(request: Request, agent_id: str, auth: AuthContext) -> None:
    """校验 Node 写操作身份：Header agent_id 须与路径/体一致；token 角色后续再启用。"""
    header_id = extract_agent_id(request)
    if header_id and header_id != agent_id:
        raise HTTPException(status_code=403, detail="x-dagents-agent-id 与 agent_id 不一致")
    if auth.is_admin:
        return
    if auth.is_node and auth.agent_id and auth.agent_id != agent_id:
        raise HTTPException(status_code=403, detail="node token 只能操作自身 agent_id")
    if auth.session_kind == "node" and auth.agent_id and auth.agent_id != agent_id:
        raise HTTPException(status_code=403, detail="node 会话只能操作自身 node_id")


def audit_actor(request: Request, auth: AuthContext, *, fallback_agent_id: str | None = None) -> str:
    header_id = extract_agent_id(request)
    if header_id:
        return header_id
    if auth.agent_id:
        return auth.agent_id
    if fallback_agent_id:
        return fallback_agent_id
    return auth.token_id


def require_admin(auth: AuthContext) -> None:
    if not auth.is_admin:
        raise HTTPException(status_code=403, detail="admin role required")


def lookup_node_token(node_id: str) -> str:
    """查找 Manage 调 home Node 时使用的 token（与该 Node 注册时相同）。

    - MANAGE_TOKENS 中 role=node 且 agent_id 匹配 → 用该 token
    - 否则若配置了 MANAGE_SHARED_TOKEN → 用共享 token
    - 开放模式 → 空字符串（home Node 侧 NodeToken 为空时放行）
    """
    want = (node_id or "").strip()
    for entry in _load_token_entries():
        token_value = str(entry.get("token") or entry.get("secret") or "").strip()
        if not token_value:
            continue
        role_raw = str(entry.get("role") or "member").strip().lower()
        agent_id = str(entry.get("agent_id") or "").strip()
        if role_raw == "node" and agent_id and agent_id == want:
            return token_value
    shared = _shared_token()
    if shared:
        return shared
    return ""
