"""Workgroup WS 路由：session.hello / resume / inbound ack + 实时 outbox 推送。"""

from __future__ import annotations

import asyncio
import json
from typing import Any, Callable

from fastapi import APIRouter, WebSocket, WebSocketDisconnect

from manage.platform.auth import AGENT_ID_HEADER, AuthContext, is_open_mode
from manage.workgroup.errors import WorkgroupError
from manage.workgroup.ws_hub import WorkgroupWSHub

# 可选：Node 回传 tool.result / provision_result 时的业务回调
InboundHandler = Callable[[str, str, dict[str, Any]], None]


def _auth_ws(websocket: WebSocket) -> AuthContext:
    """WebSocket 鉴权：开放模式放行；否则校验 token（与 HTTP 同源 header）。"""
    if is_open_mode():
        return AuthContext(token_id="anonymous", role="admin", discovery_groups=["*"])
    from manage.platform.auth import authenticate
    from starlette.requests import Request

    scope = dict(websocket.scope)
    receive = websocket._receive  # noqa: SLF001
    req = Request(scope, receive)
    return authenticate(req)


def build_workgroup_ws_router(
    hub: WorkgroupWSHub,
    *,
    on_inbound: InboundHandler | None = None,
) -> APIRouter:
    router = APIRouter(tags=["workgroups-ws"])

    @router.websocket("/v1/workgroups/ws")
    async def workgroup_ws(websocket: WebSocket) -> None:
        await websocket.accept()
        try:
            _auth_ws(websocket)
        except Exception as exc:  # noqa: BLE001
            await websocket.send_json(
                {"type": "session.error", "payload": {"code": "not_authorized", "message": str(exc)}}
            )
            await websocket.close(code=4401)
            return

        node_id = (websocket.headers.get(AGENT_ID_HEADER) or "").strip()
        if not node_id:
            await websocket.send_json(
                {
                    "type": "session.error",
                    "payload": {
                        "code": "schema_mismatch",
                        "message": "x-dagents-agent-id required",
                    },
                }
            )
            await websocket.close(code=4400)
            return

        loop = asyncio.get_running_loop()
        outbound: asyncio.Queue[dict[str, Any] | None] = asyncio.Queue()

        def sync_send(msg: dict[str, Any]) -> None:
            loop.call_soon_threadsafe(outbound.put_nowait, msg)

        async def writer() -> None:
            while True:
                item = await outbound.get()
                if item is None:
                    return
                await websocket.send_json(item)

        writer_task = asyncio.create_task(writer())
        try:
            while True:
                raw = await websocket.receive_text()
                try:
                    msg = json.loads(raw)
                except json.JSONDecodeError:
                    await outbound.put(
                        {
                            "type": "session.error",
                            "payload": {"code": "schema_mismatch", "message": "invalid json"},
                        }
                    )
                    continue
                mtype = str(msg.get("type") or "")
                payload = msg.get("payload") if isinstance(msg.get("payload"), dict) else msg
                if not isinstance(payload, dict):
                    payload = {}

                try:
                    if mtype == "session.hello" or (not mtype and "node_id" in payload):
                        nid = str(payload.get("node_id") or node_id).strip()
                        last_ack = int(payload.get("last_ack_delivery_seq") or 0)
                        hub.hello(nid, last_ack_delivery_seq=last_ack, send=sync_send)
                    elif mtype == "resume.offer":
                        wid = str(payload.get("workgroup_id") or msg.get("workgroup_id") or "").strip()
                        last_ack = int(payload.get("last_ack_delivery_seq") or 0)
                        if not wid:
                            raise WorkgroupError("schema_mismatch", "workgroup_id required")
                        hub.resume_offer(node_id, workgroup_id=wid, last_ack_delivery_seq=last_ack)
                    elif mtype in {"tool.ack", "delivery.ack", "tool.result", "member.provision_result"}:
                        seq = int(
                            payload.get("delivery_seq")
                            or msg.get("delivery_seq")
                            or 0
                        )
                        gen = int(
                            payload.get("connection_generation")
                            or msg.get("connection_generation")
                            or 0
                        )
                        wid = payload.get("workgroup_id") or msg.get("workgroup_id")
                        # 先落业务状态，避免 ack 校验失败时丢掉 provision/tool 结果
                        if on_inbound is not None and mtype in {
                            "tool.result",
                            "member.provision_result",
                        }:
                            on_inbound(node_id, mtype, dict(payload))
                        if seq > 0 and gen > 0:
                            hub.ack_delivery(
                                node_id,
                                seq,
                                connection_generation=gen,
                                workgroup_id=str(wid) if wid else None,
                            )
                        await outbound.put(
                            {"type": "delivery.acked", "payload": {"delivery_seq": seq, "type": mtype}}
                        )
                    else:
                        await outbound.put(
                            {
                                "type": "session.error",
                                "payload": {
                                    "code": "schema_mismatch",
                                    "message": f"unknown type {mtype}",
                                },
                            }
                        )
                except WorkgroupError as exc:
                    await outbound.put({"type": "session.error", "payload": exc.as_body()})
        except WebSocketDisconnect:
            pass
        finally:
            conn = hub.get_connection(node_id)
            if conn is not None:
                conn.active = False
                conn.send = None
            await outbound.put(None)
            try:
                await writer_task
            except Exception:  # noqa: BLE001
                writer_task.cancel()

    return router
