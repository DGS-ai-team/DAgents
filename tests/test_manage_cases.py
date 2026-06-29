"""Tests for Manage case examples library."""

import json
import tempfile
import unittest
from pathlib import Path

from fastapi import FastAPI
from fastapi.testclient import TestClient

from manage.cases.jsonl import export_jsonl_bytes, parse_jsonl_bytes
from manage.cases.models import CaseCreate, CaseMessage, CaseResources
from manage.cases.routes import build_cases_router
from manage.cases.store import CaseExampleStore
from manage.platform.audit import AuditLog
from manage.storage.sqlite import SQLiteDatabase


def _cases_client():
    db = SQLiteDatabase(Path(tempfile.mkdtemp()) / "m.db")
    store = CaseExampleStore(db=db)
    audit = AuditLog(max_entries=50)
    app = FastAPI()
    app.include_router(build_cases_router(store, audit))
    return TestClient(app), store


class JsonlTest(unittest.TestCase):
    def test_parse_and_export_roundtrip(self):
        raw = (
            '{"recorded_at":"2026-06-29T10:00:00+08:00","message":{"role":"user","content":"hi"}}\n'
            '{"recorded_at":"2026-06-29T10:00:01+08:00","message":{"role":"assistant","content":"hello"}}\n'
        ).encode()
        messages = parse_jsonl_bytes(raw)
        self.assertEqual(len(messages), 2)
        self.assertEqual(messages[0].role, "user")
        exported = export_jsonl_bytes(messages)
        again = parse_jsonl_bytes(exported)
        self.assertEqual(len(again), 2)
        self.assertEqual(again[1].content, "hello")


class CaseStoreTest(unittest.TestCase):
    def test_message_crud(self):
        store = CaseExampleStore(SQLiteDatabase(Path(tempfile.mkdtemp()) / "m.db"))
        payload = CaseCreate(
            case_id="demo-a",
            name="Demo A",
            description="desc",
            resources=CaseResources(skill_ids=["skill-a"], plugin_ids=["plug-a"]),
        )
        store.create(payload, messages=[], now=1)
        msg = CaseMessage(id="m1", role="user", content="ping")
        updated = store.insert_message("demo-a", msg, index=None, now=2)
        assert updated is not None
        self.assertEqual(len(updated.messages), 1)
        updated = store.update_message(
            "demo-a",
            "m1",
            CaseMessage(id="m1", role="user", content="pong"),
            now=3,
        )
        assert updated is not None
        self.assertEqual(updated.messages[0].content, "pong")
        updated = store.delete_message("demo-a", "m1", now=4)
        assert updated is not None
        self.assertEqual(updated.messages, [])


class CaseRoutesTest(unittest.TestCase):
    def test_create_with_jsonl_and_edit_messages(self):
        client, _store = _cases_client()
        jsonl = (
            b'{"recorded_at":"t1","message":{"role":"user","content":"start"}}\n'
            b'{"recorded_at":"t2","message":{"role":"assistant","content":"ok"}}\n'
        )
        r = client.post(
            "/v1/cases",
            data={
                "case_id": "demo-route",
                "name": "Route Demo",
                "description": "from test",
                "skill_ids": "skill-x",
                "plugin_ids": "plug-y",
            },
            files={"file": ("sess.jsonl", jsonl, "application/x-ndjson")},
        )
        self.assertEqual(r.status_code, 200, r.text)
        body = r.json()
        self.assertEqual(body["case_id"], "demo-route")
        self.assertEqual(len(body["messages"]), 2)
        self.assertEqual(body["resources"]["skill_ids"], ["skill-x"])

        listed = client.get("/v1/cases")
        self.assertEqual(listed.status_code, 200)
        self.assertEqual(len(listed.json()), 1)

        msg_id = body["messages"][0]["id"]
        r2 = client.patch(
            f"/v1/cases/demo-route/messages/{msg_id}",
            json={"id": msg_id, "role": "user", "content": "edited", "recorded_at": "t1"},
        )
        self.assertEqual(r2.status_code, 200)
        self.assertEqual(r2.json()["messages"][0]["content"], "edited")

        r3 = client.post(
            "/v1/cases/demo-route/messages",
            json={
                "index": 1,
                "message": {"id": "new-msg", "role": "system", "content": "hint", "recorded_at": ""},
            },
        )
        self.assertEqual(r3.status_code, 200)
        self.assertEqual(len(r3.json()["messages"]), 3)
        self.assertEqual(r3.json()["messages"][1]["role"], "system")

        r4 = client.delete(f"/v1/cases/demo-route/messages/{msg_id}")
        self.assertEqual(r4.status_code, 200)
        self.assertEqual(len(r4.json()["messages"]), 2)

        export = client.get("/v1/cases/demo-route/export/jsonl")
        self.assertEqual(export.status_code, 200)
        lines = [ln for ln in export.content.decode().splitlines() if ln.strip()]
        self.assertEqual(len(lines), 2)
        first = json.loads(lines[0])
        self.assertEqual(first["message"]["role"], "system")

    def test_parse_jsonl_preview(self):
        client, _store = _cases_client()
        jsonl = (
            b'{"recorded_at":"t1","message":{"role":"user","content":"start"}}\n'
            b'{"recorded_at":"t2","message":{"role":"assistant","content":"ok"}}\n'
        )
        r = client.post(
            "/v1/cases/parse-jsonl",
            files={"file": ("sess.jsonl", jsonl, "application/x-ndjson")},
        )
        self.assertEqual(r.status_code, 200, r.text)
        body = r.json()
        self.assertEqual(len(body), 2)
        self.assertEqual(body[0]["role"], "user")
        self.assertEqual(body[1]["content"], "ok")

    def test_duplicate_case_id(self):
        client, _store = _cases_client()
        data = {"case_id": "dup", "name": "One", "description": "", "skill_ids": "", "plugin_ids": ""}
        self.assertEqual(client.post("/v1/cases", data=data).status_code, 200)
        self.assertEqual(client.post("/v1/cases", data=data).status_code, 409)


if __name__ == "__main__":
    unittest.main()
