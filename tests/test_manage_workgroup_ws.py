"""D3 WS：gap-fill、dup connection fence、cursor_too_old、Manage 重启 outbox。"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

from fastapi import FastAPI
from fastapi.testclient import TestClient

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.storage.sqlite import SQLiteDatabase  # noqa: E402
from manage.workgroup.d3_models import OutboxFrame  # noqa: E402
from manage.workgroup.models import (  # noqa: E402
    ACLPatchRequest,
    AssignCreateRequest,
    MemberCreateRequest,
    WorkGroupCreateRequest,
)
from manage.workgroup.store import WorkGroupStore  # noqa: E402
from manage.workgroup.ws_hub import WorkgroupWSHub  # noqa: E402
from manage.workgroup.ws_routes import build_workgroup_ws_router  # noqa: E402


class WorkgroupWSHubTests(unittest.TestCase):
    def test_timeline_and_realtime_fanout_to_subscribers(self) -> None:
        store = WorkGroupStore()
        hub = WorkgroupWSHub(store=store)
        group, _ = store.create_workgroup(
            WorkGroupCreateRequest(display_name="room", created_by_node_id="node_a")
        )
        wid = group.workgroup_id
        store.patch_acl(wid, ACLPatchRequest(collaborators=["node_b"], expected_revision=1))

        sent_a: list[dict] = []
        sent_b: list[dict] = []
        hub.hello("node_a", send=sent_a.append)
        hub.hello("node_b", send=sent_b.append)

        event = store.append_timeline(
            wid,
            type="human_message",
            actor_id="node_a",
            text="hello",
        )
        frame = hub.publish_timeline_event(event)
        self.assertEqual(frame.type, "timeline.event")
        self.assertEqual(len(store.list_outbox(wid)), 1)
        self.assertEqual(sent_a[-1]["type"], "timeline.event")
        self.assertEqual(sent_b[-1]["type"], "timeline.event")
        self.assertEqual(sent_a[-1]["payload"]["text"], "hello")

        live = hub.publish_realtime_event(
            wid,
            "status",
            {"phase": "thinking"},
            client_message_id="cm_1",
        )
        self.assertEqual(live["event_type"], "status")
        self.assertEqual(sent_a[-1]["type"], "workgroup.realtime")
        self.assertEqual(sent_b[-1]["payload"]["client_message_id"], "cm_1")

    def test_failed_node_send_does_not_block_other_subscribers(self) -> None:
        store = WorkGroupStore()
        hub = WorkgroupWSHub(store=store)
        group, _ = store.create_workgroup(
            WorkGroupCreateRequest(display_name="room", created_by_node_id="node_a")
        )
        wid = group.workgroup_id
        store.patch_acl(wid, ACLPatchRequest(collaborators=["node_b"], expected_revision=1))

        def fail_after_welcome(message: dict) -> None:
            if message.get("type") != "session.welcome":
                raise ConnectionError("gone")

        hub.hello("node_a", send=fail_after_welcome)
        sent_b: list[dict] = []
        hub.hello("node_b", send=sent_b.append)

        event = store.append_timeline(wid, type="system_notice", actor_id="leader", text="progress")
        hub.publish_timeline_event(event)

        self.assertEqual(sent_b[-1]["type"], "timeline.event")
        conn_a = hub.get_connection("node_a")
        self.assertIsNotNone(conn_a)
        assert conn_a is not None
        self.assertFalse(conn_a.active)

    def test_stale_disconnect_cannot_close_new_connection(self) -> None:
        store = WorkGroupStore()
        hub = WorkgroupWSHub(store=store)
        first = hub.hello("node_a", send=lambda _message: None)
        second = hub.hello("node_a", send=lambda _message: None)

        self.assertFalse(hub.disconnect("node_a", first["payload"]["connection_generation"]))
        current = hub.get_connection("node_a")
        self.assertIsNotNone(current)
        assert current is not None
        self.assertTrue(current.active)
        self.assertEqual(current.connection_generation, second["payload"]["connection_generation"])

    def test_resume_cursor_is_scoped_per_workgroup(self) -> None:
        store = WorkGroupStore()
        hub = WorkgroupWSHub(store=store)
        group_a, _ = store.create_workgroup(
            WorkGroupCreateRequest(display_name="room-a", created_by_node_id="node_a")
        )
        group_b, _ = store.create_workgroup(
            WorkGroupCreateRequest(display_name="room-b", created_by_node_id="node_a")
        )
        store.enqueue_outbox(group_a.workgroup_id, type="timeline.event", payload={"room": "a"})
        store.enqueue_outbox(group_b.workgroup_id, type="timeline.event", payload={"room": "b"})
        hub.hello("node_a", send=lambda _message: None)
        conn = hub.get_connection("node_a")
        self.assertIsNotNone(conn)
        assert conn is not None
        hub.ack_delivery(
            "node_a",
            1,
            connection_generation=conn.connection_generation,
            workgroup_id=group_a.workgroup_id,
        )

        replay = hub.request_resume("node_a", group_b.workgroup_id)
        self.assertIsNotNone(replay)
        assert replay is not None
        self.assertEqual(replay["complete"]["payload"]["replayed"], [1])

    def test_stale_ws_result_is_fenced_before_callback(self) -> None:
        app = FastAPI()
        store = WorkGroupStore()
        hub = WorkgroupWSHub(store=store)
        inbound: list[tuple[str, str, dict]] = []
        app.include_router(
            build_workgroup_ws_router(
                hub,
                on_inbound=lambda node_id, message_type, payload: inbound.append(
                    (node_id, message_type, payload)
                ),
            )
        )

        with TestClient(app) as client:
            with client.websocket_connect(
                "/v1/workgroups/ws", headers={"x-dagents-agent-id": "node_a"}
            ) as stale:
                stale.send_json({"type": "session.hello", "payload": {"node_id": "node_a"}})
                first_welcome = stale.receive_json()
                first_generation = first_welcome["payload"]["connection_generation"]

                with client.websocket_connect(
                    "/v1/workgroups/ws", headers={"x-dagents-agent-id": "node_a"}
                ) as current:
                    current.send_json(
                        {"type": "session.hello", "payload": {"node_id": "node_a"}}
                    )
                    current.receive_json()
                    stale.send_json(
                        {
                            "type": "tool.result",
                            "payload": {
                                "workgroup_id": "wg_01h00000000000000000000001",
                                "command_id": "cmd_stale",
                                "connection_generation": first_generation,
                            },
                        }
                    )
                    error = stale.receive_json()
                    self.assertEqual(error["type"], "session.error")
                    self.assertEqual(error["payload"]["code"], "fencing_rejected")

        self.assertEqual(inbound, [])

    def test_timeline_outbox_is_recovered_with_the_same_sqlite_commit(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            db_path = Path(tmp) / "m.db"
            store = WorkGroupStore(db=SQLiteDatabase(db_path))
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(display_name="room", created_by_node_id="node_a")
            )
            event = store.append_timeline(
                group.workgroup_id,
                type="human_message",
                actor_id="node_a",
                text="persisted",
            )

            reloaded = WorkGroupStore(db=SQLiteDatabase(db_path))
            self.assertIsNotNone(
                reloaded.get_timeline_outbox(group.workgroup_id, event.event_id)
            )
            self.assertEqual(len(reloaded.list_outbox(group.workgroup_id)), 1)

    def test_gap_fill_and_dup_connection_fence(self) -> None:
        """对齐 fixtures/workgroup-d05/ws/gap_fill_and_dup_connection_fence.json"""
        with TemporaryDirectory() as tmp:
            store = WorkGroupStore(db=SQLiteDatabase(Path(tmp) / "m.db"))
            hub = WorkgroupWSHub(store=store)
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(
                    display_name="ws",
                    created_by_node_id="node_a",
                )
            )
            wid = group.workgroup_id
            store._outbox[wid] = [  # noqa: SLF001
                OutboxFrame(
                    delivery_seq=41,
                    workgroup_id=wid,
                    type="tool.command",
                    payload={"command_id": "cmd_a"},
                    created_at="2026-07-31T00:00:00Z",
                ),
                OutboxFrame(
                    delivery_seq=42,
                    workgroup_id=wid,
                    type="tool.command",
                    payload={"command_id": "cmd_b"},
                    created_at="2026-07-31T00:00:01Z",
                ),
            ]

            # conn1 @ gen 5
            hub._max_generation["node_a"] = 5  # noqa: SLF001
            sent: list[dict] = []
            # 断线后以 gen 6 重连并 resume from 40
            hub._conns.clear()  # noqa: SLF001
            welcome = hub.hello("node_a", last_ack_delivery_seq=40, send=sent.append)
            self.assertEqual(welcome["payload"]["connection_generation"], 6)

            result = hub.resume_offer("node_a", workgroup_id=wid, last_ack_delivery_seq=40)
            self.assertEqual(result["complete"]["payload"]["replayed"], [41, 42])
            self.assertEqual([e["delivery_seq"] for e in result["envelopes"]], [41, 42])

            # 旧 conn1 gen=5 迟到帧
            late = hub.handle_late_frame("node_a", connection_generation=5)
            self.assertEqual(late["code"], "fencing_rejected")

    def test_cursor_too_old_resync(self) -> None:
        store = WorkGroupStore()
        hub = WorkgroupWSHub(store=store, outbox_retention_from=100)
        group, _ = store.create_workgroup(
            WorkGroupCreateRequest(display_name="ws", created_by_node_id="node_a")
        )
        hub.hello("node_a", last_ack_delivery_seq=1, send=lambda _m: None)
        err = hub.resume_offer(
            "node_a",
            workgroup_id=group.workgroup_id,
            last_ack_delivery_seq=1,
        )
        self.assertEqual(err["type"], "resume.error")
        self.assertEqual(err["payload"]["code"], "cursor_too_old")
        self.assertEqual(err["payload"]["action"], "resync_snapshot_then_resume")

    def test_resume_only_replays_frames_for_current_home_node(self) -> None:
        store = WorkGroupStore()
        hub = WorkgroupWSHub(store=store)
        group, _ = store.create_workgroup(
            WorkGroupCreateRequest(display_name="ws", created_by_node_id="node_a")
        )
        wid = group.workgroup_id
        store.patch_acl(wid, ACLPatchRequest(collaborators=["node_b"], expected_revision=1))
        member_b, _ = store.create_member(
            wid,
            MemberCreateRequest(
                home_node_id="node_b",
                display_name="member_b",
                allow_tool_names=["read_file"],
            ),
        )
        store._outbox[wid] = [  # noqa: SLF001
            OutboxFrame(
                delivery_seq=1,
                workgroup_id=wid,
                type="member.provision",
                payload={"home_node_id": "node_a", "member_id": "member_a"},
                created_at="2026-07-31T00:00:00Z",
            ),
            OutboxFrame(
                delivery_seq=2,
                workgroup_id=wid,
                type="tool.command",
                payload={"member_id": member_b.member_id, "command_id": "command_b"},
                created_at="2026-07-31T00:00:01Z",
            ),
            OutboxFrame(
                delivery_seq=3,
                workgroup_id=wid,
                type="workgroup.tombstone",
                payload={"workgroup_id": wid},
                created_at="2026-07-31T00:00:02Z",
            ),
        ]

        hub.hello("node_a")
        result = hub.resume_offer("node_a", workgroup_id=wid, last_ack_delivery_seq=0)

        self.assertEqual(result["complete"]["payload"]["replayed"], [1, 3])
        self.assertEqual(
            [envelope["delivery_seq"] for envelope in result["envelopes"]],
            [1, 3],
        )

        hub.hello("node_b")
        result_b = hub.resume_offer("node_b", workgroup_id=wid, last_ack_delivery_seq=0)
        self.assertEqual(result_b["complete"]["payload"]["replayed"], [2, 3])

    def test_manage_restart_pending_assign(self) -> None:
        with TemporaryDirectory() as tmp:
            db_path = Path(tmp) / "m.db"
            store = WorkGroupStore(db=SQLiteDatabase(db_path))
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(display_name="ws", created_by_node_id="node_a")
            )
            wid = group.workgroup_id
            store.patch_acl(wid, ACLPatchRequest(collaborators=["node_b"], expected_revision=1))
            member, spec = store.create_member(
                wid,
                MemberCreateRequest(
                    home_node_id="node_b",
                    display_name="reader",
                    allow_tool_names=["read_file"],
                ),
            )
            _ = spec
            store.mark_member_status(member.member_id, "ready", workgroup_id=wid)
            store.publish_workgroup(wid)
            assign = store.create_assign(
                wid, AssignCreateRequest(member_id=member.member_id, instruction="read")
            )
            store.set_assign_status(assign.assign_id, "running")
            frame = store.enqueue_outbox(
                wid,
                type="tool.command",
                payload={"command_id": "cmd_01h00000000000000000000008"},
            )
            self.assertFalse(frame.acked)

            store2 = WorkGroupStore(db=SQLiteDatabase(db_path))
            hub2 = WorkgroupWSHub(store=store2)
            reloaded = store2.get_assign(assign.assign_id)
            self.assertIsNotNone(reloaded)
            assert reloaded is not None
            self.assertEqual(reloaded.status, "running")
            unacked = hub2.reconcile_unacked(wid)
            self.assertEqual(len(unacked), 1)
            self.assertEqual(unacked[0].payload["command_id"], "cmd_01h00000000000000000000008")
            assigns = [a for a in store2._assigns.values() if a.workgroup_id == wid]  # noqa: SLF001
            self.assertEqual(len(assigns), 1)


if __name__ == "__main__":
    unittest.main()
