"""Tests for External Tools store and API routes."""

import tempfile
import unittest
from pathlib import Path

from fastapi import FastAPI
from fastapi.testclient import TestClient

from manage.externaltools.models import ExternalToolPackageCreate
from manage.externaltools.routes import build_externaltools_router
from manage.externaltools.store import ExternalToolPackageStore
from manage.platform.audit import AuditLog
from manage.platform.blob import BlobStore, BlobStoreConfig
from manage.storage.sqlite import SQLiteDatabase


def _externaltools_store():
    d = tempfile.mkdtemp()
    return ExternalToolPackageStore(SQLiteDatabase(Path(d) / "m.db"))


class ExternalToolStoreTest(unittest.TestCase):
    def _mk(self, **kw):
        base = dict(
            tool_id="officecli",
            version="1.0.0",
            name="OfficeCLI",
            platform="linux-amd64",
            risk_level="low",
            blob_id="deadbeef",
        )
        base.update(kw)
        return ExternalToolPackageCreate(**base)

    def test_draft_then_publish_appears_in_catalog(self):
        s = _externaltools_store()
        s.create(self._mk(), now=1)
        self.assertEqual(s.catalog(), [])
        pub = s.publish("officecli", "1.0.0", now=2)
        self.assertEqual(pub.status, "published")
        self.assertEqual(len(s.catalog()), 1)
        self.assertEqual(s.catalog_version(), 1)

    def test_sync_manifest_since(self):
        s = _externaltools_store()
        s.create(self._mk(), now=1)
        s.publish("officecli", "1.0.0", now=2)
        items = s.sync_manifest(since=0)
        self.assertEqual(len(items), 1)
        self.assertEqual(items[0]["tool_id"], "officecli")
        self.assertEqual(s.sync_manifest(since=1), [])


def _externaltools_client():
    d = tempfile.mkdtemp()
    db = SQLiteDatabase(Path(d) / "m.db")
    blob = BlobStore(BlobStoreConfig(root=Path(d) / "blobs", max_bytes=None))
    app = FastAPI()
    app.include_router(
        build_externaltools_router(
            ExternalToolPackageStore(db),
            blob,
            AuditLog(max_entries=50),
        )
    )
    return TestClient(app)


class ExternalToolRouterTest(unittest.TestCase):
    def test_upload_publish_catalog_download(self):
        c = _externaltools_client()
        bin_bytes = b"\x7fELF fake binary"
        r = c.post(
            "/v1/externaltools/packages",
            data={
                "tool_id": "officecli",
                "version": "1.0.0",
                "name": "OfficeCLI",
                "platform": "linux-amd64",
                "risk_level": "low",
            },
            files={"file": ("officecli", bin_bytes, "application/octet-stream")},
        )
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json()["status"], "draft")
        self.assertEqual(c.get("/v1/externaltools/catalog").json(), [])
        self.assertEqual(
            c.post("/v1/externaltools/packages/officecli/versions/1.0.0/publish").status_code,
            200,
        )
        cat = c.get("/v1/externaltools/catalog").json()
        self.assertEqual(len(cat), 1)
        dl = c.get("/v1/externaltools/catalog/officecli/versions/1.0.0/download")
        self.assertEqual(dl.content, bin_bytes)
        resp = c.get("/v1/externaltools/sync/manifest?since=0").json()
        self.assertEqual(len(resp["items"]), 1)
        self.assertGreaterEqual(resp["catalog_version"], 1)

    def test_upload_rejects_invalid_tool_id(self):
        c = _externaltools_client()
        r = c.post(
            "/v1/externaltools/packages",
            data={"tool_id": "bad id/../x", "version": "1.0.0", "name": "X"},
            files={"file": ("x", b"data", "application/octet-stream")},
        )
        self.assertEqual(r.status_code, 422, r.text)
