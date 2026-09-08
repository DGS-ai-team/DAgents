import os
import tempfile
import unittest
from datetime import datetime, timezone, timedelta
from pathlib import Path
from unittest.mock import patch

from fastapi.testclient import TestClient

from manage.auto_employees.models import AutoSummary
from manage.auto_employees.store import AutoSummaryConflict, AutoSummaryStore
from manage.config import ManageSettings
from manage.manage_app import create_app
from manage.storage.sqlite import SQLiteDatabase


class AutoEmployeeManageTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.db = Path(self.tmp.name) / "manage.db"
        self.env = patch.dict(os.environ, {"MANAGE_TOKENS": '[{"role":"node","agent_id":"node-a","token":"ta"},{"role":"node","agent_id":"node-b","token":"tb"},{"role":"member","token":"member","discovery_groups":["g"]},{"role":"admin","token":"admin"}]', "MANAGE_SHARED_TOKEN": "", "MANAGE_ADMIN_PASSWORD": ""})
        self.env.start()
        self.client = TestClient(create_app(ManageSettings.for_test(db_path=self.db)))

    def tearDown(self):
        self.client.close()
        self.env.stop()
        self.tmp.cleanup()

    @staticmethod
    def headers(token, node):
        return {"x-dagents-a2a-token": token, "x-dagents-agent-id": node}

    @staticmethod
    def payload(agent="auto-1", as_of="2026-09-08T01:00:00Z"):
        return {"agent_id": agent, "display_name": "员工", "role": "整理资料", "state": "waiting", "reason": "等待下一次唤醒", "last_result": "已完成摘要", "next_at": "2026-09-09T02:00:00Z", "usage": {"tokens": 12}, "as_of": as_of}

    def test_node_can_only_publish_own_node_and_admin_can_filter(self):
        response = self.client.put("/v1/registry/nodes/node-a/auto-summary", json=self.payload(), headers=self.headers("ta", "node-a"))
        self.assertEqual(response.status_code, 200)
        self.assertEqual(self.client.put("/v1/registry/nodes/node-b/auto-summary", json=self.payload("auto-2"), headers=self.headers("ta", "node-a")).status_code, 403)
        self.assertEqual(self.client.get("/v1/auto/overview", headers=self.headers("member", "member")).status_code, 403)
        listing = self.client.get("/v1/auto/overview?node_id=node-a&state=waiting", headers=self.headers("admin", "admin"))
        self.assertEqual(listing.status_code, 200)
        self.assertEqual(listing.json()["total"], 1)
        self.assertEqual(listing.json()["items"][0]["node_id"], "node-a")

    def test_snapshot_idempotency_conflict_and_store_reopens(self):
        store = AutoSummaryStore(SQLiteDatabase(self.db))
        newer = AutoSummary.model_validate(self.payload(as_of="2026-09-08T02:00:00Z"))
        older = AutoSummary.model_validate(self.payload(as_of="2026-09-08T01:00:00Z"))
        store.put("node-a", newer)
        with self.assertRaises(AutoSummaryConflict):
            store.put("node-a", older)
        self.assertEqual(store.put("node-a", newer).as_of, newer.as_of)
        with self.assertRaises(AutoSummaryConflict):
            store.put("node-a", newer.model_copy(update={"last_result": "different"}))
        reopened = AutoSummaryStore(SQLiteDatabase(self.db))
        self.assertEqual(reopened.get("node-a", "auto-1").as_of, newer.as_of)

    def test_http_repeated_snapshot_is_idempotent_and_conflicts_are_409(self):
        path = "/v1/registry/nodes/node-a/auto-summary"
        headers = self.headers("ta", "node-a")
        first = self.client.put(path, json=self.payload(), headers=headers)
        second = self.client.put(path, json=self.payload(), headers=headers)
        self.assertEqual(first.status_code, second.status_code, second.text)
        self.assertEqual(first.json()["received_at"], second.json()["received_at"])
        self.assertEqual(self.client.put(path, json={**self.payload(), "last_result": "different"}, headers=headers).status_code, 409)
        self.assertEqual(self.client.put(path, json=self.payload(as_of="2026-09-07T01:00:00Z"), headers=headers).status_code, 409)

    def test_timestamps_and_usage_are_bounded(self):
        path = "/v1/registry/nodes/node-a/auto-summary"
        headers = self.headers("ta", "node-a")
        self.assertEqual(self.client.put(path, json={**self.payload(), "as_of": "2026-09-09T01:00:00"}, headers=headers).status_code, 422)
        self.assertEqual(self.client.put(path, json={**self.payload(), "usage": {"tokens": -1}}, headers=headers).status_code, 422)

    def test_sqlite_write_failure_does_not_leave_memory_record(self):
        db = SQLiteDatabase(self.db)
        store = AutoSummaryStore(db)
        db._path = Path(self.tmp.name) / "write-failure"
        db._path.mkdir()
        with self.assertRaises(Exception):
            store.put("node-a", AutoSummary.model_validate(self.payload()))
        reopened = AutoSummaryStore(SQLiteDatabase(self.db))
        self.assertIsNone(reopened.get("node-a", "auto-1"))

    def test_memory_and_sqlite_freshness_use_same_server_threshold(self):
        summary = AutoSummary.model_validate(self.payload(as_of="2026-09-08T01:00:00Z"))
        memory = AutoSummaryStore()
        memory.put("node-a", summary, datetime.fromisoformat("2026-09-08T01:00:00+00:00"))
        items, _ = memory.list(now=datetime.fromisoformat("2026-09-08T01:02:01+00:00"), stale_after_seconds=120)
        self.assertTrue(items[0].stale)

    def test_http_overview_computes_server_freshness(self):
        store = self.client.app.state.auto_summary_store
        store.put("node-a", AutoSummary.model_validate(self.payload()), datetime.now(timezone.utc) - timedelta(seconds=121))
        response = self.client.get("/v1/auto/overview", headers=self.headers("admin", "admin"))
        self.assertEqual(response.status_code, 200)
        body = response.json()
        self.assertIn("server_time", body)
        self.assertTrue(body["items"][0]["stale"])
        self.assertGreaterEqual(body["items"][0]["age_seconds"], 121)

    def test_anonymous_member_and_other_node_cannot_publish(self):
        path = "/v1/registry/nodes/node-a/auto-summary"
        self.assertEqual(self.client.put(path, json=self.payload()).status_code, 401)
        self.assertEqual(self.client.put(path, json=self.payload(), headers={"x-dagents-a2a-token": "member"}).status_code, 403)
        self.assertEqual(self.client.put(path, json=self.payload(), headers=self.headers("tb", "node-b")).status_code, 403)
