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
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
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
                self.assertEqual(body["workgroup"]["status"], "configuring")
                self.assertEqual(body["acl"]["owners"], ["node-a"])

                patched = client.patch(
                    f"/v1/workgroups/{wid}",
                    json={"llm_profile_id": "deepseek-v4-flash"},
                )
                self.assertEqual(patched.status_code, 200, patched.text)
                self.assertEqual(patched.json()["llm_profile_id"], "deepseek-v4-flash")
                self.assertEqual(patched.json()["llm_profile_revision"], "2")

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

                spec_get = client.get(f"/v1/workgroups/{wid}/members/{mid}/spec")
                self.assertEqual(spec_get.status_code, 200, spec_get.text)
                self.assertEqual(spec_get.json()["digest"], spec["digest"])
                self.assertEqual(spec_get.json()["tools"]["allow_names"], ["read_file"])

                # 未发布不可派发 / 对话
                denied_assign = client.post(
                    f"/v1/workgroups/{wid}/assigns",
                    json={"member_id": mid, "instruction": "read README"},
                )
                self.assertEqual(denied_assign.status_code, 409, denied_assign.text)
                self.assertEqual(denied_assign.json()["detail"]["code"], "workgroup_not_published")

                denied_msg = client.post(
                    f"/v1/workgroups/{wid}/messages",
                    json={"text": "读 README", "from_node_id": "node-a"},
                    headers={"x-dagents-agent-id": "node-a"},
                )
                self.assertEqual(denied_msg.status_code, 409, denied_msg.text)
                self.assertEqual(denied_msg.json()["detail"]["code"], "workgroup_not_published")

                published = client.post(f"/v1/workgroups/{wid}/publish")
                self.assertEqual(published.status_code, 200, published.text)
                self.assertEqual(published.json()["status"], "active")

                # 创建成员后即可派发（无需 Grant）
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

                llm_list = client.get(f"/v1/workgroups/{wid}/llm-configs")
                self.assertEqual(llm_list.status_code, 200, llm_list.text)
                self.assertIsInstance(llm_list.json(), list)

                msg = client.post(
                    f"/v1/workgroups/{wid}/messages",
                    json={"text": "读 README", "from_node_id": "node-a"},
                    headers={"x-dagents-agent-id": "node-a"},
                )
                self.assertEqual(msg.status_code, 200, msg.text)
                body = msg.json()
                self.assertEqual(body["timeline_event"]["type"], "human_message")
                self.assertIn("leader_run", body)

                timeline = client.get(f"/v1/workgroups/{wid}/timeline")
                self.assertEqual(timeline.status_code, 200, timeline.text)
                # human + Leader mock 终态（至少 1 条 human）
                self.assertGreaterEqual(len(timeline.json()), 1)
                self.assertEqual(timeline.json()[0]["type"], "human_message")
                latest_timeline = client.get(f"/v1/workgroups/{wid}/timeline", params={"limit": 1})
                self.assertEqual(latest_timeline.status_code, 200, latest_timeline.text)
                self.assertEqual(len(latest_timeline.json()), 1)

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

                # 创建成员即入队 provision outbox
                m2 = client.post(
                    f"/v1/workgroups/{wid}/members",
                    json={
                        "home_node_id": "node-b",
                        "display_name": "reader2",
                        "allow_tool_names": ["read_file"],
                    },
                )
                self.assertEqual(m2.status_code, 200, m2.text)
                self.assertEqual(m2.json()["member"]["status"], "provisioning")
                outbox = client.get(f"/v1/workgroups/{wid}/outbox", params={"unacked_only": True})
                self.assertEqual(outbox.status_code, 200, outbox.text)
                types = [f["type"] for f in outbox.json()]
                self.assertIn("member.provision", types)

                failed = client.post(
                    f"/v1/workgroups/{wid}/provision-complete",
                    json={
                        "member_id": m2.json()["member"]["member_id"],
                        "provision_id": "pv_" + "1" * 26,
                        "status": "error",
                        "error_code": "not_authorized",
                        "message": "home_node_id must be this node",
                    },
                )
                self.assertEqual(failed.status_code, 200, failed.text)
                self.assertEqual(failed.json()["member"]["status"], "error")
                self.assertEqual(failed.json()["member"]["error_code"], "not_authorized")
                self.assertIn("home_node_id", failed.json()["member"]["error_message"])

                pending = client.get(f"/v1/workgroups/{wid}/hitl", params={"pending_only": True})
                self.assertEqual(pending.status_code, 200, pending.text)
                self.assertEqual(len(pending.json()), 0)  # 已决议
                all_hitl = client.get(f"/v1/workgroups/{wid}/hitl", params={"pending_only": False})
                self.assertEqual(all_hitl.status_code, 200, all_hitl.text)
                self.assertEqual(len(all_hitl.json()), 1)

    def test_member_tool_catalog(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db")
            app = create_app(settings)
            with TestClient(app) as client:
                res = client.get("/v1/workgroups/meta/member-tools")
                self.assertEqual(res.status_code, 200, res.text)
                body = res.json()
                ids = [t["id"] for t in body["tools"]]
                self.assertIn("read_file", ids)
                self.assertIn("bash_run", ids)
                self.assertIn("search_replace", ids)
                self.assertIn("read_file", body["default_allow_names"])
                self.assertNotIn("bash_run", body["default_allow_names"])
                self.assertTrue(set(body["default_allow_names"]).issubset(set(ids)))


if __name__ == "__main__":
    unittest.main()


