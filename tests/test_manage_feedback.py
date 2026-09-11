import os
import tempfile
import unittest
from unittest.mock import patch
from pathlib import Path
from fastapi.testclient import TestClient
from manage.config import ManageSettings
from manage.manage_app import create_app
from manage.feedback.models import FeedbackCreate, FeedbackPatch
from manage.feedback.store import FeedbackConflict, FeedbackStore
from manage.storage.sqlite import SQLiteDatabase
import threading


class FeedbackBackendTests(unittest.TestCase):
    def setUp(self):
        self.t = tempfile.TemporaryDirectory()
        self.db = Path(self.t.name) / "m.db"
        self.env = patch.dict(
            os.environ,
            {
                "MANAGE_TOKENS": '[{"id":"n1","role":"node","agent_id":"node-a","token":"ta"},{"id":"n2","role":"node","agent_id":"node-b","token":"tb"},{"id":"ad","role":"admin","token":"admin"}]',
                "MANAGE_SHARED_TOKEN": "",
                "MANAGE_ADMIN_PASSWORD": "",
            },
        )
        self.env.start()
        self.c = TestClient(create_app(ManageSettings.for_test(db_path=self.db)))

    def tearDown(self):
        self.c.close()
        self.t.cleanup()
        self.env.stop()

    def h(self, t, n):
        return {"x-dagents-a2a-token": t, "x-dagents-agent-id": n}

    def test_idempotency_identity_and_revision(self):
        p = {"client_feedback_id": "f1", "node_id": "node-a", "category": "bug", "title": "T", "body": "B"}
        a = self.c.post("/v1/feedback", json=p, headers=self.h("ta", "node-a"))
        self.assertEqual(a.status_code, 200)
        self.assertEqual(self.c.post("/v1/feedback", json=p, headers=self.h("ta", "node-a")).status_code, 200)
        self.assertEqual(
            self.c.post("/v1/feedback", json={**p, "body": "X"}, headers=self.h("ta", "node-a")).status_code, 409
        )
        self.assertEqual(self.c.get("/v1/feedback", headers=self.h("tb", "node-b")).json()["total"], 0)
        u = self.c.patch(
            "/v1/feedback/f1?node_id=node-a",
            json={"status": "resolved", "reply": "ok", "revision": 1},
            headers=self.h("admin", "x"),
        )
        self.assertEqual(u.status_code, 200)
        self.assertEqual(
            self.c.patch(
                "/v1/feedback/f1?node_id=node-a", json={"status": "closed", "revision": 1}, headers=self.h("admin", "x")
            ).status_code,
            409,
        )

    def test_anonymous_and_member_cannot_write(self):
        p = {"client_feedback_id": "fa", "node_id": "node-a", "category": "bug", "title": "T", "body": "B"}
        self.assertEqual(self.c.post("/v1/feedback", json=p).status_code, 401)
        with patch.dict(os.environ, {"MANAGE_TOKENS": '[{"role":"member","token":"m","discovery_groups":["g"]}]'}):
            self.assertEqual(self.c.post("/v1/feedback", json=p, headers={"x-dagents-a2a-token": "m"}).status_code, 403)

    def test_two_stores_compare_immutable_content_and_cas(self):
        db = SQLiteDatabase(self.db)
        a = FeedbackStore(db)
        b = FeedbackStore(db)
        p = FeedbackCreate(client_feedback_id="race", node_id="node-a", category="bug", title="T", body="B")
        barrier = threading.Barrier(2)
        results = []

        def run(st, payload):
            barrier.wait()
            try:
                results.append(st.create(payload, "now"))
            except Exception as e:
                results.append(e)

        t1 = threading.Thread(target=run, args=(a, p))
        t2 = threading.Thread(target=run, args=(b, p.model_copy(update={"body": "different"})))
        t1.start()
        t2.start()
        t1.join()
        t2.join()
        self.assertTrue(any(isinstance(x, FeedbackConflict) for x in results))
        self.assertEqual(a.patch("node-a", "race", FeedbackPatch(status="resolved", revision=1), "later").revision, 2)
        with self.assertRaises(FeedbackConflict):
            b.patch("node-a", "race", FeedbackPatch(status="closed", revision=1), "later")

    def test_rate_limit_rejects_before_persisting_and_idempotent_retry_is_free(self):
        h = self.h("ta", "node-a")
        for i in range(20):
            p = {"client_feedback_id": f"rate-{i}", "node_id": "node-a", "category": "bug", "title": "T", "body": "B"}
            self.assertEqual(self.c.post("/v1/feedback", json=p, headers=h).status_code, 200)
        p = {"client_feedback_id": "rate-20", "node_id": "node-a", "category": "bug", "title": "T", "body": "B"}
        self.assertEqual(self.c.post("/v1/feedback", json=p, headers=h).status_code, 429)
        self.assertEqual(self.c.get("/v1/feedback", headers=h).json()["total"], 20)
