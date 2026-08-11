"""Manage `/v1/workgroups/ws` TestClient 拨号：hello / resume / live push。"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

from fastapi.testclient import TestClient

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.config import ManageSettings  # noqa: E402
from manage.manage_app import create_app  # noqa: E402
from manage.workgroup.d3_models import OutboxFrame  # noqa: E402
from manage.workgroup.models import WorkGroupCreateRequest  # noqa: E402


class WorkgroupWSRouteTests(unittest.TestCase):
    def test_hello_rejects_node_id_mismatch(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)

            with TestClient(app) as client:
                with client.websocket_connect(
                    "/v1/workgroups/ws",
                    headers={"x-dagents-agent-id": "node_a"},
                ) as ws:
                    ws.send_json(
                        {
                            "type": "session.hello",
                            "payload": {
                                "node_id": "node_b",
                                "last_ack_delivery_seq": 0,
                            },
                        }
                    )
                    error = ws.receive_json()

                    self.assertEqual(error["type"], "session.error")
                    self.assertEqual(error["payload"]["code"], "not_authorized")

    def test_hello_resume_and_live_push(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            store = app.state.workgroup_store
            hub = app.state.workgroup_ws_hub
            group, _ = store.create_workgroup(
                WorkGroupCreateRequest(display_name="ws", created_by_node_id="node_b")
            )
            wid = group.workgroup_id
            store._outbox[wid] = [  # noqa: SLF001
                OutboxFrame(
                    delivery_seq=1,
                    workgroup_id=wid,
                    type="tool.command",
                    payload={"command_id": "cmd_a"},
                    created_at="2026-07-31T00:00:00Z",
                )
            ]

            with TestClient(app) as client:
                with client.websocket_connect(
                    "/v1/workgroups/ws",
                    headers={"x-dagents-agent-id": "node_b"},
                ) as ws:
                    ws.send_json(
                        {
                            "type": "session.hello",
                            "payload": {
                                "node_id": "node_b",
                                "last_ack_delivery_seq": 0,
                            },
                        }
                    )
                    welcome = ws.receive_json()
                    self.assertEqual(welcome["type"], "session.welcome")
                    gen = welcome["payload"]["connection_generation"]
                    self.assertGreaterEqual(gen, 1)

                    ws.send_json(
                        {
                            "type": "resume.offer",
                            "payload": {
                                "workgroup_id": wid,
                                "last_ack_delivery_seq": 0,
                            },
                        }
                    )
                    env = ws.receive_json()
                    self.assertEqual(env["type"], "tool.command")
                    self.assertEqual(env["delivery_seq"], 1)
                    complete = ws.receive_json()
                    self.assertEqual(complete["type"], "resume.complete")

                    # live push：enqueue 后经 hub 推送
                    frame = store.enqueue_outbox(
                        wid, type="workgroup.tombstone", payload={"workgroup_id": wid}
                    )
                    hub.deliver_outbox_frame(frame, home_node_id="node_b")
                    live = ws.receive_json()
                    self.assertEqual(live["type"], "workgroup.tombstone")
                    self.assertEqual(live["delivery_seq"], frame.delivery_seq)

                    ws.send_json(
                        {
                            "type": "delivery.ack",
                            "payload": {
                                "delivery_seq": frame.delivery_seq,
                                "connection_generation": gen,
                                "workgroup_id": wid,
                            },
                        }
                    )
                    acked = ws.receive_json()
                    self.assertEqual(acked["type"], "delivery.acked")


if __name__ == "__main__":
    unittest.main()
