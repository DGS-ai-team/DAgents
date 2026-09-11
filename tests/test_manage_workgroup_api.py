"""Current Workgroup AgentRef HTTP contract tests."""

from __future__ import annotations

import sys
import unittest
from unittest.mock import patch
from pathlib import Path
from tempfile import TemporaryDirectory

from fastapi.testclient import TestClient

_ROOT = Path(__file__).resolve().parents[1]
_AUTH_PATCH = patch.dict("os.environ", {"MANAGE_SHARED_TOKEN": "test-admin-token"})
def setUpModule(): _AUTH_PATCH.start()
def tearDownModule(): _AUTH_PATCH.stop()
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.config import ManageSettings  # noqa: E402
from manage.manage_app import create_app  # noqa: E402
from manage.registry.models import AgentRegisterRequest  # noqa: E402


class ManageWorkgroupAPITests(unittest.TestCase):
    def _app(self, tmp: str):
        return create_app(ManageSettings.for_test(db_path=Path(tmp) / "manage.db"))

    @staticmethod
    def _register(app, agent_id: str, node_id: str) -> None:
        app.state.registry_store.register(
            AgentRegisterRequest(
                agent_id=agent_id,
                node_id=node_id,
                base_url="http://127.0.0.1:18765",
            )
        )

    def test_agent_ref_member_lifecycle(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            app = self._app(tmp)
            self._register(app, "agent-b", "node-b")
            with TestClient(app, headers={"x-dagents-a2a-token": "test-admin-token"}) as client:
                headers = {"x-dagents-agent-id": "node-a"}
                created = client.post(
                    "/v1/workgroups",
                    json={"display_name": "API Demo", "created_by_node_id": "node-a"},
                    headers=headers,
                )
                self.assertEqual(created.status_code, 200, created.text)
                wid = created.json()["workgroup"]["workgroup_id"]

                acl = client.patch(
                    f"/v1/workgroups/{wid}/acl",
                    json={"collaborators": ["node-b"], "expected_revision": 1},
                )
                self.assertEqual(acl.status_code, 200, acl.text)

                member_resp = client.post(
                    f"/v1/workgroups/{wid}/members",
                    json={
                        "agent_id": "agent-b",
                        "home_node_id": "node-b",
                        "display_name": "代码员",
                        "description": "负责阅读代码",
                    },
                )
                self.assertEqual(member_resp.status_code, 200, member_resp.text)
                member = member_resp.json()["member"]
                mid = member["member_id"]
                self.assertEqual(member["agent_id"], "agent-b")
                self.assertNotIn("spec", member_resp.json())

                outbox = client.get(f"/v1/workgroups/{wid}/outbox").json()
                self.assertEqual(outbox[-1]["type"], "agent.session.open")
                self.assertEqual(outbox[-1]["payload"]["session_id"], member["session_id"])

                self.assertEqual(client.post(f"/v1/workgroups/{wid}/publish").status_code, 200)
                assign = client.post(
                    f"/v1/workgroups/{wid}/assigns",
                    json={"member_id": mid, "instruction": "读取 README"},
                )
                self.assertEqual(assign.status_code, 200, assign.text)
                self.assertEqual(assign.json()["status"], "queued")

                patched = client.patch(
                    f"/v1/workgroups/{wid}/members/{mid}",
                    json={"description": "负责阅读和总结代码"},
                )
                self.assertEqual(patched.status_code, 200, patched.text)
                self.assertEqual(patched.json()["member"]["description"], "负责阅读和总结代码")

    def test_archive_member_closes_agent_session(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp:
            app = self._app(tmp)
            self._register(app, "agent-a", "node-a")
            with TestClient(app, headers={"x-dagents-a2a-token": "test-admin-token"}) as client:
                created = client.post(
                    "/v1/workgroups",
                    json={"display_name": "Archive API", "created_by_node_id": "node-a"},
                    headers={"x-dagents-agent-id": "node-a"},
                )
                self.assertEqual(created.status_code, 200, created.text)
                wid = created.json()["workgroup"]["workgroup_id"]
                member_resp = client.post(
                    f"/v1/workgroups/{wid}/members",
                    json={
                        "agent_id": "agent-a",
                        "home_node_id": "node-a",
                        "display_name": "remove-me",
                    },
                )
                self.assertEqual(member_resp.status_code, 200, member_resp.text)
                member = member_resp.json()["member"]

                archived = client.post(f"/v1/workgroups/{wid}/members/{member['member_id']}/archive")
                self.assertEqual(archived.status_code, 200, archived.text)
                self.assertEqual(archived.json()["status"], "archived")
                outbox = client.get(f"/v1/workgroups/{wid}/outbox").json()
                self.assertEqual(outbox[-1]["type"], "agent.session.close")

    def test_configuring_workgroup_archive_is_single_call_and_idempotent(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp, TestClient(self._app(tmp), headers={"x-dagents-a2a-token": "test-admin-token"}) as client:
            created = client.post(
                "/v1/workgroups",
                json={"display_name": "Draft archive", "created_by_node_id": "node-a"},
                headers={"x-dagents-agent-id": "node-a"},
            )
            self.assertEqual(created.status_code, 200, created.text)
            wid = created.json()["workgroup"]["workgroup_id"]
            first = client.post(f"/v1/workgroups/{wid}/archive")
            self.assertEqual(first.status_code, 200, first.text)
            self.assertEqual(first.json()["status"], "archived")
            second = client.post(f"/v1/workgroups/{wid}/archive")
            self.assertEqual(second.status_code, 200, second.text)
            self.assertEqual(second.json()["status"], "archived")
            self.assertEqual(second.json()["archived_at"], first.json()["archived_at"])

    def test_removed_member_catalog_and_spec_endpoints(self) -> None:
        with TemporaryDirectory(ignore_cleanup_errors=True) as tmp, TestClient(self._app(tmp), headers={"x-dagents-a2a-token": "test-admin-token"}) as client:
            self.assertEqual(client.get("/v1/workgroups/meta/member-tools").status_code, 404)
            self.assertEqual(
                client.get(
                    "/v1/workgroups/wg_00000000000000000000000000/"
                    "members/mb_00000000000000000000000000/spec"
                ).status_code,
                404,
            )


if __name__ == "__main__":
    unittest.main()
