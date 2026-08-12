"""Console 登录 / 会话 API。"""

from __future__ import annotations

from typing import Literal

from fastapi import APIRouter, HTTPException, Request, Response
from pydantic import BaseModel, Field

from manage.platform.auth import (
    auth_from_session,
    default_admin_username,
    resolve_session,
    verify_admin_password,
)
from manage.platform.sessions import SESSION_COOKIE, SessionStore
from manage.registry.store import AgentRegistryStore, record_node_id


class PasswordLoginRequest(BaseModel):
    username: str = Field(min_length=1)
    password: str = Field(min_length=1)


class NodeLoginRequest(BaseModel):
    node_id: str = Field(min_length=1)


class AuthMeResponse(BaseModel):
    authenticated: bool
    kind: Literal["admin", "node"] | None = None
    subject: str | None = None
    role: Literal["admin", "member", "node"] | None = None
    discovery_groups: list[str] = Field(default_factory=list)
    agent_id: str | None = None
    default_admin_username: str | None = None


def _set_session_cookie(response: Response, session_id: str) -> None:
    response.set_cookie(
        key=SESSION_COOKIE,
        value=session_id,
        httponly=True,
        samesite="lax",
        path="/",
        max_age=7 * 24 * 3600,
    )


def _clear_session_cookie(response: Response) -> None:
    response.delete_cookie(key=SESSION_COOKIE, path="/")


def build_auth_router(sessions: SessionStore, registry: AgentRegistryStore) -> APIRouter:
    router = APIRouter(prefix="/v1/auth", tags=["auth"])

    @router.get("/me", response_model=AuthMeResponse)
    def me(request: Request) -> AuthMeResponse:
        rec = resolve_session(request)
        if rec is None:
            return AuthMeResponse(
                authenticated=False,
                default_admin_username=default_admin_username(),
            )
        auth = auth_from_session(rec)
        return AuthMeResponse(
            authenticated=True,
            kind=rec.kind,
            subject=rec.subject,
            role=auth.role,
            discovery_groups=list(auth.discovery_groups),
            agent_id=auth.agent_id,
            default_admin_username=default_admin_username() if rec.kind == "admin" else None,
        )

    @router.post("/login", response_model=AuthMeResponse)
    def login_password(body: PasswordLoginRequest, response: Response) -> AuthMeResponse:
        if not verify_admin_password(body.username, body.password):
            raise HTTPException(status_code=401, detail="用户名或密码错误")
        rec = sessions.create(kind="admin", subject=default_admin_username())
        _set_session_cookie(response, rec.session_id)
        auth = auth_from_session(rec)
        return AuthMeResponse(
            authenticated=True,
            kind=rec.kind,
            subject=rec.subject,
            role=auth.role,
            discovery_groups=list(auth.discovery_groups),
            agent_id=None,
            default_admin_username=default_admin_username(),
        )

    @router.post("/login/node", response_model=AuthMeResponse)
    def login_node(body: NodeLoginRequest, response: Response) -> AuthMeResponse:
        node_id = str(body.node_id or "").strip()
        if not node_id:
            raise HTTPException(status_code=400, detail="node_id 不能为空")
        record = registry.get(node_id)
        if record is None:
            raise HTTPException(status_code=401, detail="未知 node_id：请先让 Node 连接并注册到 Manage")
        resolved = record_node_id(record) or node_id
        groups = list(getattr(record, "discovery_group", None) or [])
        rec = sessions.create(kind="node", subject=resolved, discovery_groups=groups)
        _set_session_cookie(response, rec.session_id)
        auth = auth_from_session(rec)
        return AuthMeResponse(
            authenticated=True,
            kind=rec.kind,
            subject=rec.subject,
            role=auth.role,
            discovery_groups=list(auth.discovery_groups),
            agent_id=auth.agent_id,
        )

    @router.post("/logout", response_model=AuthMeResponse)
    def logout(request: Request, response: Response) -> AuthMeResponse:
        raw = (request.cookies.get(SESSION_COOKIE) or "").strip()
        if raw:
            sessions.revoke(raw)
        _clear_session_cookie(response)
        return AuthMeResponse(
            authenticated=False,
            default_admin_username=default_admin_username(),
        )

    return router
