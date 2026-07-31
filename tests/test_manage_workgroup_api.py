"""Workgroup D1 HTTP API 测试。"""

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


class ManageWorkgroupAPITests(unittest.TestCase):
    def test_vertical_skeleton_happy_path(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            with TestClient(app) as client:
                created = client.post(
                    "/v1/workgroups",
                    json={
                        "display_name": "API Demo",
                        "created_by_node_id": "node-a",
                        "llm_profile_id": "default",
                        "llm_profile_revision": "1",
                    },
                    headers={"x-dagents-agent-id": "node-a"},
                )
                self.assertEqual(created.status_code, 200, created.text)
                body = created.json()
                wid = body["workgroup"]["workgroup_id"]
                self.assertTrue(wid.startswith("wg_"))
                self.assertEqual(body["acl"]["owners"], ["node-a"])

                acl = client.patch(
                    f"/v1/workgroups/{wid}/acl",
                    json={"collaborators": ["node-b"], "expected_revision": 1},
                )
                self.assertEqual(acl.status_code, 200, acl.text)

                member_resp = client.post(
                    f"/v1/workgroups/{wid}/members",
                    json={
                        "home_node_id": "node-b",
                        "display_name": "代码员",
                        "allow_tool_names": ["read_file"],
                    },
                )
                self.assertEqual(member_resp.status_code, 200, member_resp.text)
                member = member_resp.json()["member"]
                spec = member_resp.json()["spec"]
                mid = member["member_id"]

                # ACL 不足以派发
                denied = client.post(
                    f"/v1/workgroups/{wid}/assigns",
                    json={"member_id": mid, "instruction": "read README"},
                )
                self.assertEqual(denied.status_code, 403, denied.text)
                self.assertEqual(denied.json()["detail"]["code"], "not_authorized")

                grant_resp = client.post(
                    f"/v1/workgroups/{wid}/grants",
                    json={"member_id": mid},
                )
                self.assertEqual(grant_resp.status_code, 200, grant_resp.text)
                grant_id = grant_resp.json()["grant_id"]

                accepted = client.post(
                    f"/v1/workgroups/{wid}/grants/{grant_id}/accept",
                    json={"member_spec_digest": spec["digest"]},
                    headers={"x-dagents-agent-id": "node-b"},
                )
                self.assertEqual(accepted.status_code, 200, accepted.text)
                self.assertEqual(accepted.json()["status"], "accepted")

                assign = client.post(
                    f"/v1/workgroups/{wid}/assigns",
                    json={"member_id": mid, "instruction": "read README"},
                )
                self.assertEqual(assign.status_code, 200, assign.text)
                self.assertEqual(assign.json()["status"], "queued")

                proj = client.get(f"/v1/workgroups/{wid}/projector", params={"actor_id": "leader"})
                self.assertEqual(proj.status_code, 200, proj.text)
                self.assertEqual(proj.json()["actor_id"], "leader")
                self.assertIn("messages", proj.json())

                # D3：provision-complete → messages → timeline → HITL CAS
                ready = client.post(
                    f"/v1/workgroups/{wid}/provision-complete",
                    json={
                        "member_id": mid,
                        "provision_id": "pv_" + "0" * 26,
                        "workspace_path": str(Path(tmp) / "ws"),
                        "tool_catalog_revision": "rev_test",
                        "status": "ready",
                    },
                )
                self.assertEqual(ready.status_code, 200, ready.text)
                self.assertEqual(ready.json()["member"]["status"], "ready")

                msg = client.post(
                    f"/v1/workgroups/{wid}/messages",
                    json={"text": "读 README", "from_node_id": "node-a"},
                    headers={"x-dagents-agent-id": "node-a"},
                )
                self.assertEqual(msg.status_code, 200, msg.text)
                self.assertEqual(msg.json()["type"], "human_message")

                timeline = client.get(f"/v1/workgroups/{wid}/timeline")
                self.assertEqual(timeline.status_code, 200, timeline.text)
                self.assertEqual(len(timeline.json()), 1)

                hitl = client.post(
                    f"/v1/workgroups/{wid}/hitl",
                    json={"prompt": "继续？"},
                )
                self.assertEqual(hitl.status_code, 200, hitl.text)
                hid = hitl.json()["hitl_id"]
                resolved = client.post(
                    f"/v1/workgroups/{wid}/hitl/{hid}/resolve",
                    json={"resolution": {"answer": "yes"}},
                )
                self.assertEqual(resolved.status_code, 200, resolved.text)
                conflict = client.post(
                    f"/v1/workgroups/{wid}/hitl/{hid}/resolve",
                    json={"resolution": {"answer": "no"}},
                )
                self.assertEqual(conflict.status_code, 409, conflict.text)
                self.assertEqual(conflict.json()["detail"]["code"], "already_resolved")

                # 创建者已自动订阅；acl_member / subscribed_by 过滤
                sub = client.get("/v1/workgroups", params={"subscribed_by": "node-a"})
                self.assertEqual(sub.status_code, 200, sub.text)
                self.assertEqual(len(sub.json()), 1)
                acl_b = client.get("/v1/workgroups", params={"acl_member": "node-b"})
                self.assertEqual(acl_b.status_code, 200, acl_b.text)
                self.assertEqual(len(acl_b.json()), 1)
                join = client.post(
                    f"/v1/workgroups/{wid}/subscribe",
                    json={"node_id": "node-b"},
                    headers={"x-dagents-agent-id": "node-b"},
                )
                self.assertEqual(join.status_code, 200, join.text)

                # Grant accept 入队 provision outbox
                grant_id = grant_resp.json()["grant_id"]
                # 再建一个成员专测 accept→provision（上面 member 已有 grant）
                m2 = client.post(
                    f"/v1/workgroups/{wid}/members",
                    json={
                        "home_node_id": "node-b",
                        "display_name": "reader2",
                        "allow_tool_names": ["read_file"],
                    },
                )
                self.assertEqual(m2.status_code, 200, m2.text)
                mid2 = m2.json()["member"]["member_id"]
                digest2 = m2.json()["spec"]["digest"]
                g2 = client.post(f"/v1/workgroups/{wid}/grants", json={"member_id": mid2})
                self.assertEqual(g2.status_code, 200, g2.text)
                accepted2 = client.post(
                    f"/v1/workgroups/{wid}/grants/{g2.json()['grant_id']}/accept",
                    json={"member_spec_digest": digest2},
                    headers={"x-dagents-agent-id": "node-b"},
                )
                self.assertEqual(accepted2.status_code, 200, accepted2.text)
                outbox = client.get(f"/v1/workgroups/{wid}/outbox", params={"unacked_only": True})
                self.assertEqual(outbox.status_code, 200, outbox.text)
                types = [f["type"] for f in outbox.json()]
                self.assertIn("member.provision", types)

                pending = client.get(f"/v1/workgroups/{wid}/hitl", params={"pending_only": True})
                self.assertEqual(pending.status_code, 200, pending.text)
                self.assertEqual(len(pending.json()), 0)  # 已决议
                all_hitl = client.get(f"/v1/workgroups/{wid}/hitl", params={"pending_only": False})
                self.assertEqual(all_hitl.status_code, 200, all_hitl.text)
                self.assertEqual(len(all_hitl.json()), 1)

                _ = grant_id  # keep first grant path exercised above


if __name__ == "__main__":
    unittest.main()


