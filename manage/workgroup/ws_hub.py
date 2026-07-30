"""Manage 侧工作组 WS 会话与 outbox 投递（D3）。

不依赖真实网络时可在单测中直接调用 Hub；HTTP 升级见 ws_routes。
"""

from __future__ import annotations

import threading
from dataclasses import dataclass, field
from typing import Any, Callable

from manage.workgroup import ids
from manage.workgroup.d3_models import OutboxFrame, WSEnvelope
from manage.workgroup.errors import WorkgroupError
from manage.workgroup.store import WorkGroupStore, _now

SendFn = Callable[[dict[str, Any]], None]


@dataclass
class NodeConnection:
    node_id: str
    connection_generation: int
    last_ack_delivery_seq: int = 0
    send: SendFn | None = None
    active: bool = True
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

    def hello(
        self,
        node_id: str,
        *,
        last_ack_delivery_seq: int = 0,
        send: SendFn | None = None,
    ) -> dict[str, Any]:
        """session.hello：抬升 connection_generation，旧连接 fencing。"""
        node_id = node_id.strip()
        if not node_id:
            raise WorkgroupError("schema_mismatch", "node_id required")
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
                    "schema_version": "0.5.0",
                },
            }
            if send is not None:
                send(welcome)
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

        frames = self.store.frames_after(workgroup_id, after_seq=last_ack_delivery_seq)
        batch: list[dict[str, Any]] = []
        for frame in frames:
            env = self.wrap_outbox(frame, connection_generation=conn.connection_generation)
            batch.append(env.model_dump())
            if conn.send is not None:
                conn.send(env.model_dump())
        complete = {
            "type": "resume.complete",
            "payload": {
                "replayed": [f.delivery_seq for f in frames],
                "from_delivery_seq": last_ack_delivery_seq,
            },
        }
        if conn.send is not None:
            conn.send(complete)
        with self._lock:
            conn.last_ack_delivery_seq = last_ack_delivery_seq
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
        if delivery_seq < conn.last_ack_delivery_seq:
            raise WorkgroupError("conflict", "delivery_seq regress", http_status=409)
        if workgroup_id:
            self.store.ack_outbox(workgroup_id, delivery_seq)
        with self._lock:
            conn.last_ack_delivery_seq = delivery_seq

    def push_to_node(self, node_id: str, frame: OutboxFrame) -> WSEnvelope | None:
        """向已连接 Node 推送一帧；未连接则仅落库等待 resume。"""
        with self._lock:
            conn = self._conns.get(node_id)
            if conn is None or not conn.active or conn.send is None:
                return None
            env = self.wrap_outbox(frame, connection_generation=conn.connection_generation)
            conn.send(env.model_dump())
            return env

    def deliver_outbox_frame(
        self,
        frame: OutboxFrame,
        *,
        home_node_id: str,
    ) -> WSEnvelope | None:
        return self.push_to_node(home_node_id, frame)

    def reconcile_unacked(self, workgroup_id: str) -> list[OutboxFrame]:
        """Manage 重启后：列出未 ack outbox（不断言重复 assign）。"""
        return self.store.list_outbox(workgroup_id, unacked_only=True)
