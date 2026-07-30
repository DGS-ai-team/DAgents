"""Workgroup WS 路由：session.hello / resume / inbound ack。"""

from __future__ import annotations

import json
from typing import Any

from fastapi import APIRouter, WebSocket, WebSocketDisconnect

from manage.platform.auth import AGENT_ID_HEADER, AuthContext, is_open_mode
from manage.workgroup.errors import WorkgroupError
from manage.workgroup.ws_hub import WorkgroupWSHub


def _auth_ws(websocket: WebSocket) -> AuthContext:
    """WebSocket 鉴权：开放模式放行；否则校验 token（与 HTTP 同源 header）。"""
    if is_open_mode():
        return AuthContext(token_id="anonymous", role="admin", discovery_groups=["*"])
    # 延迟导入避免循环
    from manage.platform.auth import authenticate
    from starlette.requests import Request

    scope = dict(websocket.scope)
    # 构造最小 Request 供 authenticate 读 header
    receive = websocket._receive  # noqa: SLF001
    req = Request(scope, receive)
    return authenticate(req)


def build_workgroup_ws_router(hub: WorkgroupWSHub) -> APIRouter:
    router = APIRouter(tags=["workgroups-ws"])

    @router.websocket("/v1/workgroups/ws")
    async def workgroup_ws(websocket: WebSocket) -> None:
        await websocket.accept()
        try:
            _auth_ws(websocket)
        except Exception as exc:  # noqa: BLE001
            await websocket.send_json({"type": "session.error", "payload": {"code": "not_authorized", "message": str(exc)}})
            await websocket.close(code=4401)
            return

        node_id = (websocket.headers.get(AGENT_ID_HEADER) or "").strip()
        if not node_id:
            await websocket.send_json(
                {"type": "session.error", "payload": {"code": "schema_mismatch", "message": "x-dagents-agent-id required"}}
            )
            await websocket.close(code=4400)
            return

        async def send(msg: dict[str, Any]) -> None:
            await websocket.send_json(msg)

        # 同步 send 适配 Hub（Hub 单测用同步回调；WS 路径用队列）
        pending: list[dict[str, Any]] = []

        def sync_send(msg: dict[str, Any]) -> None:
            pending.append(msg)

        try:
            while True:
                raw = await websocket.receive_text()
                try:
                    msg = json.loads(raw)
                except json.JSONDecodeError:
                    await websocket.send_json(
                        {"type": "session.error", "payload": {"code": "schema_mismatch", "message": "invalid json"}}
                    )
                    continue
                mtype = str(msg.get("type") or "")
                payload = msg.get("payload") if isinstance(msg.get("payload"), dict) else msg

                try:
                    if mtype == "session.hello" or (not mtype and "node_id" in payload):
                        nid = str(payload.get("node_id") or node_id).strip()
                        last_ack = int(payload.get("last_ack_delivery_seq") or 0)
                        hub.hello(nid, last_ack_delivery_seq=last_ack, send=sync_send)
                        for item in pending:
                            await send(item)
                        pending.clear()
                    elif mtype == "resume.offer":
                        wid = str(payload.get("workgroup_id") or msg.get("workgroup_id") or "").strip()
                        last_ack = int(payload.get("last_ack_delivery_seq") or 0)
                        if not wid:
                            raise WorkgroupError("schema_mismatch", "workgroup_id required")
                        hub.resume_offer(node_id, workgroup_id=wid, last_ack_delivery_seq=last_ack)
                        for item in pending:
                            await send(item)
                        pending.clear()
                    elif mtype in {"tool.ack", "delivery.ack"}:
                        seq = int(payload.get("delivery_seq") or msg.get("delivery_seq") or 0)
                        gen = int(payload.get("connection_generation") or msg.get("connection_generation") or 0)
                        wid = payload.get("workgroup_id") or msg.get("workgroup_id")
                        hub.ack_delivery(
                            node_id,
                            seq,
                            connection_generation=gen,
                            workgroup_id=str(wid) if wid else None,
                        )
                        await send({"type": "delivery.acked", "payload": {"delivery_seq": seq}})
                    else:
                        await send(
                            {
                                "type": "session.error",
                                "payload": {"code": "schema_mismatch", "message": f"unknown type {mtype}"},
                            }
                        )
                except WorkgroupError as exc:
                    await send({"type": "session.error", "payload": exc.as_body()})
        except WebSocketDisconnect:
            conn = hub.get_connection(node_id)
            if conn is not None:
                conn.active = False
                conn.send = None

    return router
