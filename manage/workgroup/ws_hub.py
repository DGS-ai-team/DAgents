"""Manage 侧工作组 WS 会话与 outbox 投递。

不依赖真实网络时可在单测中直接调用 Hub；HTTP 升级见 ws_routes。
"""

from __future__ import annotations

import threading
from dataclasses import dataclass, field
from queue import Empty as QueueEmpty, Full, Queue
from typing import Any, Callable

from manage.platform.metrics import record_workgroup_ws_event
from manage.workgroup import ids
from manage.workgroup.d3_models import OutboxFrame, WSEnvelope
from manage.workgroup.errors import WorkgroupError
from manage.workgroup.protocol import (
    MANAGE_CAPABILITIES,
    PROTOCOL_VERSION,
    SCHEMA_VERSION,
    normalize_capabilities,
    validate_schema_version,
    validate_protocol_version,
)
from manage.workgroup.store import WorkGroupStore, _now

SendFn = Callable[[dict[str, Any]], None]


@dataclass
class BrowserConnection:
    """单个浏览器工作组事件流。

    浏览器不是 Node WS 的对端，不参与 delivery ack；它以 Timeline seq
    作为恢复游标。队列有界，消费端失速时主动断开，让客户端带游标重连并
    从持久化 Timeline 补齐，而不是无限堆积内存。
    """

    connection_id: str
    workgroup_id: str
    queue: Queue[dict[str, Any] | None] = field(
        default_factory=lambda: Queue(maxsize=256), repr=False
    )
    closed: bool = False

    def enqueue(self, message: dict[str, Any]) -> bool:
        if self.closed:
            return False
        try:
            self.queue.put_nowait(message)
            return True
        except Full:
            # A reconnect will replay durable Timeline events. Closing is the
            # only safe response when a browser cannot keep up with the stream.
            self.closed = True
            while True:
                try:
                    self.queue.get_nowait()
                except QueueEmpty:
                    break
            try:
                self.queue.put_nowait(
                    {
                        "type": "workgroup.resync_required",
                        "payload": {
                            "workgroup_id": self.workgroup_id,
                            "reason": "browser_event_queue_full",
                        },
                    }
                )
            except Full:
                pass
            return False

    def close(self) -> None:
        self.closed = True
        try:
            self.queue.put_nowait(None)
        except Full:
            # The consumer will observe the resync marker or disconnect.
            pass


@dataclass
class NodeConnection:
    node_id: str
    connection_generation: int
    last_ack_delivery_seq: int = 0
    last_ack_by_workgroup: dict[str, int] = field(default_factory=dict)
    send: SendFn | None = None
    active: bool = True
    # resume replay and live outbox delivery can originate from different
    # request threads; keep their envelope order on one WebSocket.
    send_lock: threading.Lock = field(default_factory=threading.Lock, repr=False)
    # 旧连接被替换后仍保留世代，用于拒绝迟到帧
    fenced: bool = False


@dataclass
class WorkgroupWSHub:
    """按 node_id 维护强身份会话；outbox → WSEnvelope 投递与 resume。"""

    store: WorkGroupStore
    outbox_retention_from: int = 1
    _lock: threading.RLock = field(default_factory=threading.RLock)
    _conns: dict[str, NodeConnection] = field(default_factory=dict)
    # node_id → 曾用过的最大 generation（含已 fenced）
    _max_generation: dict[str, int] = field(default_factory=dict)
    _live_seq: dict[str, int] = field(default_factory=dict)
    # workgroup_id → browser connection id → BrowserConnection
    _browser_conns: dict[str, dict[str, BrowserConnection]] = field(default_factory=dict)

    def hello(
        self,
        node_id: str,
        *,
        last_ack_delivery_seq: int = 0,
        send: SendFn | None = None,
        protocol_version: str = PROTOCOL_VERSION,
        schema_version: str = SCHEMA_VERSION,
        capabilities: list[str] | None = None,
        client_time: str = "",
    ) -> dict[str, Any]:
        """session.hello：抬升 connection_generation，旧连接 fencing。"""
        node_id = node_id.strip()
        if not node_id:
            raise WorkgroupError("schema_mismatch", "node_id required")
        protocol_version = validate_protocol_version(protocol_version)
        schema_version = validate_schema_version(schema_version)
        normalize_capabilities(capabilities)
        with self._lock:
            old = self._conns.get(node_id)
            if old is not None:
                old.active = False
                old.fenced = True
                old.send = None
            gen = self._max_generation.get(node_id, 0) + 1
            self._max_generation[node_id] = gen
            conn = NodeConnection(
                node_id=node_id,
                connection_generation=gen,
                last_ack_delivery_seq=max(0, int(last_ack_delivery_seq)),
                last_ack_by_workgroup={},
                send=send,
                active=True,
                fenced=False,
            )
            self._conns[node_id] = conn
            welcome = {
                "type": "session.welcome",
                "payload": {
                    "node_id": node_id,
                    "connection_generation": gen,
                    "protocol_version": protocol_version,
                    "schema_version": schema_version,
                    "capabilities": list(MANAGE_CAPABILITIES),
                    "server_time": _now(),
                },
            }
            record_workgroup_ws_event(direction="lifecycle", event="session.hello")
            if send is not None:
                try:
                    send(welcome)
                    record_workgroup_ws_event(direction="outbound", event="session.welcome")
                except Exception:  # noqa: BLE001 - failed socket must not remain active
                    conn.active = False
                    conn.send = None
            return welcome

    def get_connection(self, node_id: str) -> NodeConnection | None:
        with self._lock:
            return self._conns.get(node_id)

    def assert_generation(self, node_id: str, connection_generation: int) -> NodeConnection:
        with self._lock:
            conn = self._conns.get(node_id)
            if conn is None or not conn.active:
                raise WorkgroupError(
                    "fencing_rejected",
                    "no active connection",
                    http_status=409,
                )
            if connection_generation != conn.connection_generation:
                raise WorkgroupError(
                    "fencing_rejected",
                    f"stale connection_generation {connection_generation}",
                    http_status=409,
                )
            return conn

    def handle_late_frame(
        self,
        node_id: str,
        connection_generation: int,
    ) -> dict[str, Any]:
        """旧连接迟到帧：返回 fencing_rejected（对齐 fixture）。"""
        try:
            self.assert_generation(node_id, connection_generation)
        except WorkgroupError as exc:
            return {"code": exc.code, "message": exc.message, "retryable": False}
        return {"code": "ok"}

    def disconnect(self, node_id: str, connection_generation: int) -> bool:
        """Close only the connection generation that owns this socket.

        An older WebSocket can finish its cleanup after a newer connection has
        already replaced it.  Never let that stale cleanup deactivate the new
        connection.
        """
        with self._lock:
            conn = self._conns.get(str(node_id or "").strip())
            if conn is None or conn.connection_generation != int(connection_generation):
                return False
            conn.active = False
            conn.send = None
            record_workgroup_ws_event(direction="lifecycle", event="disconnect")
            return True

    def _mark_send_failed(self, node_id: str, conn: NodeConnection) -> None:
        with self._lock:
            current = self._conns.get(node_id)
            if current is conn:
                current.active = False
                current.fenced = True
                current.send = None

    def wrap_outbox(self, frame: OutboxFrame, *, connection_generation: int | None = None) -> WSEnvelope:
        return WSEnvelope(
            envelope_id=ids.envelope_id(),
            type=frame.type,
            workgroup_id=frame.workgroup_id,
            delivery_seq=frame.delivery_seq,
            connection_generation=connection_generation,
            payload=dict(frame.payload),
            sent_at=_now(),
        )

    def _frame_belongs_to_node(self, frame: OutboxFrame, node_id: str) -> bool:
        """Only replay frames addressed to this node; unscoped frames broadcast."""
        home_node_id = str(frame.payload.get("home_node_id") or "").strip()
        if not home_node_id:
            member_id = str(frame.payload.get("member_id") or "").strip()
            if member_id:
                member = self.store.get_member(member_id)
                home_node_id = str(member.home_node_id if member is not None else "").strip()
        return not home_node_id or home_node_id == node_id

    def resume_offer(
        self,
        node_id: str,
        *,
        workgroup_id: str,
        last_ack_delivery_seq: int,
    ) -> dict[str, Any]:
        """resume.offer：gap-fill 或 cursor_too_old。"""
        conn = self.get_connection(node_id)
        if conn is None or not conn.active:
            raise WorkgroupError("not_authorized", "hello required first", http_status=403)
        wid = str(workgroup_id or "").strip()
        if not wid:
            raise WorkgroupError("schema_mismatch", "workgroup_id required")
        with self._lock:
            previous = int(conn.last_ack_by_workgroup.get(wid, 0))
        if last_ack_delivery_seq < previous:
            raise WorkgroupError(
                "conflict",
                f"delivery_seq regress {last_ack_delivery_seq} < {previous} for {wid}",
                http_status=409,
            )
        retention = self.outbox_retention_from
        if last_ack_delivery_seq + 1 < retention:
            err = {
                "type": "resume.error",
                "payload": {
                    "code": "cursor_too_old",
                    "action": "resync_snapshot_then_resume",
                    "outbox_retention_from": retention,
                    "last_ack_delivery_seq": last_ack_delivery_seq,
                },
            }
            if conn.send is not None:
                conn.send(err)
            return err

        frames = [
            frame
            for frame in self.store.frames_after(wid, after_seq=last_ack_delivery_seq)
            if self._frame_belongs_to_node(frame, node_id)
        ]
        batch: list[dict[str, Any]] = []
        with conn.send_lock:
            for frame in frames:
                env = self.wrap_outbox(frame, connection_generation=conn.connection_generation)
                batch.append(env.model_dump())
                if conn.send is not None:
                    try:
                        conn.send(env.model_dump())
                    except Exception:  # noqa: BLE001 - close and let Dialer reconnect
                        self._mark_send_failed(node_id, conn)
                        break
            complete = {
                "type": "resume.complete",
                "payload": {
                    "replayed": [f.delivery_seq for f in frames],
                    "from_delivery_seq": last_ack_delivery_seq,
                },
            }
            if conn.send is not None:
                try:
                    conn.send(complete)
                except Exception:  # noqa: BLE001 - close and let Dialer reconnect
                    self._mark_send_failed(node_id, conn)
        with self._lock:
            conn.last_ack_by_workgroup[wid] = last_ack_delivery_seq
            conn.last_ack_delivery_seq = max(
                int(conn.last_ack_delivery_seq or 0), last_ack_delivery_seq
            )
        return {"type": "resume.batch", "envelopes": batch, "complete": complete}

    def ack_delivery(
        self,
        node_id: str,
        delivery_seq: int,
        *,
        connection_generation: int,
        workgroup_id: str | None = None,
    ) -> None:
        conn = self.assert_generation(node_id, connection_generation)
        wid = str(workgroup_id or "").strip()
        with self._lock:
            previous = int(
                conn.last_ack_by_workgroup.get(wid, 0)
                if wid
                else conn.last_ack_delivery_seq
            )
        if delivery_seq < previous:
            scope = f" for {wid}" if wid else ""
            raise WorkgroupError(
                "conflict",
                f"delivery_seq regress {delivery_seq} < {previous}{scope}",
                http_status=409,
            )
        if workgroup_id:
            self.store.ack_outbox(workgroup_id, delivery_seq)
        with self._lock:
            if wid:
                conn.last_ack_by_workgroup[wid] = delivery_seq
            conn.last_ack_delivery_seq = max(int(conn.last_ack_delivery_seq or 0), delivery_seq)

    def push_to_node(self, node_id: str, frame: OutboxFrame) -> WSEnvelope | None:
        """向已连接 Node 推送一帧；未连接则仅落库等待 resume。"""
        with self._lock:
            conn = self._conns.get(node_id)
            if conn is None or not conn.active or conn.send is None:
                return None
            env = self.wrap_outbox(frame, connection_generation=conn.connection_generation)
            send = conn.send
        with conn.send_lock:
            try:
                send(env.model_dump())
                record_workgroup_ws_event(direction="outbound", event=env.type)
            except Exception:  # noqa: BLE001 - one stale Node must not block fan-out
                self._mark_send_failed(node_id, conn)
                return None
        return env

    def push_json_to_node(self, node_id: str, message: dict[str, Any]) -> bool:
        """Push an ephemeral JSON message to an active node session."""
        with self._lock:
            conn = self._conns.get(node_id)
            if conn is None or not conn.active or conn.send is None:
                return False
            send = conn.send
        with conn.send_lock:
            try:
                send(dict(message))
                record_workgroup_ws_event(
                    direction="outbound", event=str(message.get("type") or "other")
                )
            except Exception:  # noqa: BLE001 - one stale Node must not block fan-out
                self._mark_send_failed(node_id, conn)
                return False
        return True

    def publish_timeline_event(self, event: Any) -> OutboxFrame:
        """Persist a reliable Timeline frame and fan it out to current subscribers."""
        workgroup_id = str(getattr(event, "workgroup_id", "") or "").strip()
        if not workgroup_id:
            raise WorkgroupError("schema_mismatch", "timeline event workgroup_id required")
        payload = _jsonable(event.model_dump(mode="json") if hasattr(event, "model_dump") else event)
        frame = self.store.get_timeline_outbox(workgroup_id, str(payload.get("event_id") or ""))
        if frame is None:
            raise WorkgroupError("schema_mismatch", "timeline outbox missing")
        browser_event = self._timeline_event_for_browser(event)
        browser_message = {
            "type": "timeline.event",
            "payload": _jsonable(
                browser_event.model_dump(mode="json")
                if hasattr(browser_event, "model_dump")
                else browser_event
            ),
        }
        # The listener is called after the Timeline + outbox transaction has
        # committed. Fan out the same canonical event to Nodes and browsers.
        with self._lock:
            browser_conns = list((self._browser_conns.get(workgroup_id) or {}).values())
            for conn in browser_conns:
                conn.enqueue(browser_message)
        for sub in self.store.list_subscribers(workgroup_id):
            self.push_to_node(sub.node_id, frame)
        return frame

    def _timeline_event_for_browser(self, event: Any) -> Any:
        """Keep the live browser projection aligned with GET /timeline."""
        if (
            getattr(event, "type", None) != "assign_started"
            or not getattr(event, "assign_id", None)
            or str(getattr(event, "actor_id", "") or "").strip() != "leader"
        ):
            return event
        assign = self.store.get_assign(str(event.assign_id))
        instruction = str(getattr(assign, "instruction", "") or "").strip()
        if assign is None or not instruction:
            return event
        member = self.store.get_member(assign.member_id)
        display = (str(getattr(member, "display_name", "") or "") or assign.member_id).strip()
        return event.model_copy(update={"text": f"@{display}\n{instruction}"})

    def publish_realtime_event(
        self,
        workgroup_id: str,
        event_type: str,
        data: dict[str, Any] | None = None,
        *,
        client_message_id: str | None = None,
    ) -> dict[str, Any]:
        """Fan out transient turn state/deltas; durable messages use Timeline/outbox."""
        wid = str(workgroup_id or "").strip()
        if not wid:
            raise WorkgroupError("schema_mismatch", "workgroup_id required")
        with self._lock:
            seq = int(self._live_seq.get(wid, 0)) + 1
            self._live_seq[wid] = seq
        payload = {
            "workgroup_id": wid,
            "event_id": ids.new_id("rt"),
            "event_type": str(event_type or "realtime"),
            "stream_seq": seq,
            "client_message_id": str(client_message_id or "").strip() or None,
            "data": _jsonable(data or {}),
            "sent_at": _now(),
        }
        message = {"type": "workgroup.realtime", "payload": payload}
        # The request-scoped POST SSE owns token/delta/status rendering. Only
        # the durable queue projection belongs on the browser event stream;
        # forwarding every model delta here would duplicate a high-volume
        # short-lived stream after a page reload.
        if str(event_type or "") == "queue":
            with self._lock:
                browser_conns = list((self._browser_conns.get(wid) or {}).values())
                for conn in browser_conns:
                    conn.enqueue(message)
        for sub in self.store.list_subscribers(wid):
            self.push_json_to_node(sub.node_id, message)
        return payload

    def publish_hitl_change(self, hitl: Any) -> None:
        """Push the current HITL projection to browsers after its commit."""
        wid = str(getattr(hitl, "workgroup_id", "") or "").strip()
        if not wid:
            return
        pending = self.store.list_hitl(wid, pending_only=True)
        message = {
            "type": "hitl.changed",
            "payload": {
                "workgroup_id": wid,
                "hitl": _jsonable(hitl),
                "pending": [_jsonable(item) for item in pending],
                "sent_at": _now(),
            },
        }
        with self._lock:
            for conn in list((self._browser_conns.get(wid) or {}).values()):
                conn.enqueue(message)

    def subscribe_browser(
        self,
        workgroup_id: str,
        *,
        after_seq: int = 0,
    ) -> tuple[BrowserConnection, list[Any], list[Any]]:
        """Register a browser and atomically capture its replay snapshot.

        Registration and snapshot selection share the Hub lock. Therefore an
        event cannot land between the replay cut and the live queue: it is
        either included in ``replay`` or queued for the live consumer.
        """
        wid = str(workgroup_id or "").strip()
        if not wid:
            raise WorkgroupError("schema_mismatch", "workgroup_id required")
        cursor = max(0, int(after_seq))
        conn = BrowserConnection(connection_id=ids.new_id("bc"), workgroup_id=wid)
        with self._lock:
            replay = [
                self._timeline_event_for_browser(event)
                for event in self.store.list_timeline(wid)
                if int(getattr(event, "seq", 0) or 0) > cursor
            ]
            self._browser_conns.setdefault(wid, {})[conn.connection_id] = conn
            pending = self.store.list_hitl(wid, pending_only=True)
        return conn, replay, pending

    def unsubscribe_browser(self, connection: BrowserConnection) -> None:
        with self._lock:
            bucket = self._browser_conns.get(connection.workgroup_id) or {}
            bucket.pop(connection.connection_id, None)
            if not bucket:
                self._browser_conns.pop(connection.workgroup_id, None)
        connection.close()

    def deliver_outbox_frame(
        self,
        frame: OutboxFrame,
        *,
        home_node_id: str,
    ) -> WSEnvelope | None:
        return self.push_to_node(home_node_id, frame)

    def request_resume(self, node_id: str, workgroup_id: str) -> dict[str, Any] | None:
        """若 Node 已在线，按其连接游标对本组补发 resume（幂等 gap-fill）。"""
        nid = (node_id or "").strip()
        wid = (workgroup_id or "").strip()
        if not nid or not wid:
            return None
        conn = self.get_connection(nid)
        if conn is None or not conn.active:
            return None
        return self.resume_offer(
            nid,
            workgroup_id=wid,
            last_ack_delivery_seq=int(conn.last_ack_by_workgroup.get(wid, 0) or 0),
        )

    def reconcile_unacked(self, workgroup_id: str) -> list[OutboxFrame]:
        """Manage 重启后：列出未 ack outbox（不断言重复 assign）。"""
        return self.store.list_outbox(workgroup_id, unacked_only=True)


def _jsonable(value: Any) -> Any:
    if hasattr(value, "model_dump"):
        return _jsonable(value.model_dump(mode="json"))
    if isinstance(value, dict):
        return {str(k): _jsonable(v) for k, v in value.items()}
    if isinstance(value, (list, tuple)):
        return [_jsonable(v) for v in value]
    return value
