from __future__ import annotations

import json
import sqlite3
import sys
import time
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory

from fastapi.testclient import TestClient

_ROOT = Path(__file__).resolve().parents[1]
if str(_ROOT) not in sys.path:
    sys.path.insert(0, str(_ROOT))

from manage.config import ManageSettings  # noqa: E402
from manage.manage_app import create_app  # noqa: E402
from manage.storage.sqlite import SQLiteDatabase  # noqa: E402


def _register_agent(client: TestClient, agent_id: str, *, base_url: str | None = None) -> None:
    url = base_url or f"http://{agent_id}.local"
    client.post(
        "/v1/registry/agents",
        json={"agent_id": agent_id, "base_url": url, "expose_to_peers": True},
        headers={"x-dagents-agent-id": agent_id},
    )
    client.post(
        f"/v1/registry/agents/{agent_id}/heartbeat",
        json={"ttl_seconds": 120},
        headers={"x-dagents-agent-id": agent_id},
    )


def _assign_groups(client: TestClient, agent_id: str, groups: list[str] | None = None) -> None:
    payload = {"discovery_group": groups or ["default-lab"]}
    resp = client.patch(
        f"/v1/registry/agents/{agent_id}/groups",
        json=payload,
    )
    assert resp.status_code == 200, resp.text


class ManageAdminTests(unittest.TestCase):
    def test_list_a2a_tasks_readonly(self) -> None:
        with TemporaryDirectory() as tmp:
            db_path = Path(tmp) / "manage.db"
            settings = ManageSettings.for_test(db_path=db_path, a2a_expire_sweep_seconds=0)
            now = int(time.time())
            payload = {
                "task_id": "task-admin-1",
                "from_agent_id": "caller-x",
                "to_agent_id": "callee-y",
                "kind": "invoke",
                "content": "hello admin",
                "blob_ids": [],
                "caller_session_id": "",
                "idempotency_key": "",
                "trace_id": "",
                "status": "queued",
                "created_at_unix": now,
                "updated_at_unix": now,
                "expires_at_unix": now + 3600,
                "delivered_at_unix": None,
                "result_text": "",
                "result_status": None,
                "callee_session_id": "",
                "error_detail": "",
                "pending_caller_resume": {},
            }
            SQLiteDatabase(db_path)
            with sqlite3.connect(db_path) as conn:
                conn.execute(
                    "INSERT INTO a2a_tasks(task_id, payload_json) VALUES (?, ?)",
                    ("task-admin-1", json.dumps(payload)),
                )
                conn.commit()

            app = create_app(settings)
            with TestClient(app) as client:
                listed = client.get("/v1/admin/a2a/tasks")
                self.assertEqual(listed.status_code, 200)
                body = listed.json()
                self.assertGreaterEqual(body["total"], 1)
                ids = [t["task_id"] for t in body["tasks"]]
                self.assertIn("task-admin-1", ids)

                polled = client.get("/v1/a2a/inbox", params={"agent_id": "callee-y"})
                self.assertEqual(polled.status_code, 410)

    def test_list_a2a_tasks_with_surrogate_content(self) -> None:
        with TemporaryDirectory() as tmp:
            db_path = Path(tmp) / "manage.db"
            settings = ManageSettings.for_test(db_path=db_path, a2a_expire_sweep_seconds=0)
            now = int(time.time())
            base = {
                "task_id": "task-surrogate",
                "from_agent_id": "caller-s",
                "to_agent_id": "callee-s",
                "kind": "invoke",
                "content": "bad surrogate",
                "blob_ids": [],
                "caller_session_id": "",
                "idempotency_key": "",
                "trace_id": "",
                "status": "queued",
                "created_at_unix": now,
                "updated_at_unix": now,
                "expires_at_unix": now + 3600,
                "delivered_at_unix": None,
                "result_text": "",
                "result_status": None,
                "callee_session_id": "",
                "error_detail": "",
                "pending_caller_resume": {},
            }
            raw = json.loads(json.dumps(base).replace("bad surrogate", "bad\\uDC80unicode"))
            self.assertIn("\udc80", raw["content"])
            corrupt_json = json.dumps(base).replace("bad surrogate", "bad\\uDC80unicode")

            SQLiteDatabase(db_path)
            with sqlite3.connect(db_path) as conn:
                conn.execute(
                    "INSERT INTO a2a_tasks(task_id, payload_json) VALUES (?, ?)",
                    ("task-surrogate", corrupt_json),
                )
                conn.commit()

            app = create_app(settings)
            with TestClient(app) as client:
                listed = client.get("/v1/admin/a2a/tasks")
                self.assertEqual(listed.status_code, 200, listed.text)
                body = listed.json()
                self.assertGreaterEqual(body["total"], 1)
                content = next(t["content"] for t in body["tasks"] if t["task_id"] == "task-surrogate")
                self.assertNotIn("\udc80", content)
                content.encode("utf-8")

    def test_admin_node_session_proxy_removed(self) -> None:
        with TemporaryDirectory() as tmp:
            settings = ManageSettings.for_test(db_path=Path(tmp) / "manage.db", a2a_expire_sweep_seconds=0)
            app = create_app(settings)
            with TestClient(app) as client:
                _register_agent(client, "node-z", base_url="http://node-z.test")
                for path in (
                    "/v1/admin/nodes/node-z/sessions",
                    "/v1/admin/nodes/node-z/sessions/s-1/context",
                ):
                    resp = client.get(path)
                    self.assertEqual(resp.status_code, 404, path)


if __name__ == "__main__":
    unittest.main()
