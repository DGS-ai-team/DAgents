"""Tests for Platform Blob API (Task 4) and Skills Store (Task 5)."""

import hashlib
import tempfile
import unittest
from pathlib import Path

from fastapi import FastAPI
from fastapi.testclient import TestClient

from manage.platform.blob import BlobStore, BlobStoreConfig
from manage.platform.blob_routes import build_blob_router


def _blob_client():
    d = tempfile.mkdtemp()
    blob = BlobStore(BlobStoreConfig(root=Path(d), max_bytes=None))
    app = FastAPI()
    app.include_router(build_blob_router(blob))
    return TestClient(app)


class BlobTest(unittest.TestCase):
    def test_upload_download_roundtrip(self):
        c = _blob_client()
        data = b"hello-skill-zip-bytes"
        r = c.post("/v1/blobs", files={"file": ("s.zip", data, "application/zip")})
        self.assertEqual(r.status_code, 200, r.text)
        bid = r.json()["blob_id"]
        self.assertEqual(r.json()["sha256"], hashlib.sha256(data).hexdigest())
        got = c.get(f"/v1/blobs/{bid}")
        self.assertEqual(got.content, data)
        self.assertEqual(c.head(f"/v1/blobs/{bid}").status_code, 200)

    def test_head_returns_metadata_headers(self):
        c = _blob_client()
        data = b"zip-data-for-head"
        r = c.post("/v1/blobs", files={"file": ("a.zip", data, "application/zip")})
        bid = r.json()["blob_id"]
        hr = c.head(f"/v1/blobs/{bid}")
        self.assertEqual(hr.status_code, 200)
        self.assertEqual(hr.headers["x-blob-sha256"], hashlib.sha256(data).hexdigest())
        self.assertEqual(hr.headers["x-blob-size"], str(len(data)))

    def test_delete_blob(self):
        c = _blob_client()
        data = b"delete-me"
        r = c.post("/v1/blobs", files={"file": ("d.zip", data, "application/zip")})
        bid = r.json()["blob_id"]
        dr = c.delete(f"/v1/blobs/{bid}")
        self.assertEqual(dr.status_code, 200)
        self.assertTrue(dr.json()["deleted"])
        # after deletion, get returns 404
        self.assertEqual(c.get(f"/v1/blobs/{bid}").status_code, 404)

    def test_get_missing_returns_404(self):
        c = _blob_client()
        r = c.get("/v1/blobs/deadbeef" * 8)
        self.assertEqual(r.status_code, 404)

    def test_max_bytes_enforced(self):
        d = tempfile.mkdtemp()
        blob = BlobStore(BlobStoreConfig(root=Path(d), max_bytes=10))
        app = FastAPI()
        app.include_router(build_blob_router(blob))
        c = TestClient(app)
        data = b"x" * 11
        r = c.post("/v1/blobs", files={"file": ("big.zip", data, "application/zip")})
        self.assertEqual(r.status_code, 413)

    def test_content_addressed_deduplication(self):
        """Uploading same data twice returns same blob_id."""
        c = _blob_client()
        data = b"deduplicate-me"
        r1 = c.post("/v1/blobs", files={"file": ("f1.zip", data, "application/zip")})
        r2 = c.post("/v1/blobs", files={"file": ("f2.zip", data, "application/zip")})
        self.assertEqual(r1.json()["blob_id"], r2.json()["blob_id"])

    def test_invalid_blob_id_returns_404(self):
        """Path-traversal guard: non-hex / short blob_id must return 404 before filesystem access."""
        c = _blob_client()
        self.assertEqual(c.get("/v1/blobs/not-a-valid-hash").status_code, 404)
        self.assertEqual(c.get("/v1/blobs/../etc/passwd").status_code, 404)
        self.assertEqual(c.get("/v1/blobs/short").status_code, 404)


from manage.storage.sqlite import SQLiteDatabase
from manage.skills.store import SkillPackageStore
from manage.skills.models import SkillPackageCreate
from manage.skills.routes import build_skills_router
from manage.platform.audit import AuditLog


def _skill_store():
    d = tempfile.mkdtemp()
    return SkillPackageStore(SQLiteDatabase(Path(d) / "m.db"))


class SkillStoreTest(unittest.TestCase):
    def _mk(self, **kw):
        base = dict(skill_id="svc-restart", version="1.0.0", name="Service Restart",
                    risk_level="medium", blob_id="deadbeef")
        base.update(kw); return SkillPackageCreate(**base)

    def test_draft_then_publish_appears_in_catalog(self):
        s = _skill_store()
        s.create(self._mk(), now=1)
        self.assertEqual(s.catalog(), [])               # draft not in catalog
        pub = s.publish("svc-restart", "1.0.0", now=2)
        self.assertEqual(pub.status, "published")
        self.assertEqual(len(s.catalog()), 1)
        self.assertEqual(s.catalog_version(), 1)

    def test_sync_manifest_since(self):
        s = _skill_store()
        s.create(self._mk(), now=1); s.publish("svc-restart", "1.0.0", now=2)
        self.assertEqual(len(s.sync_manifest(since=0)), 1)
        self.assertEqual(s.sync_manifest(since=1), [])   # nothing new past current version


def _skills_client():
    d = tempfile.mkdtemp()
    db = SQLiteDatabase(Path(d) / "m.db")
    blob = BlobStore(BlobStoreConfig(root=Path(d) / "blobs", max_bytes=None))
    app = FastAPI()
    app.include_router(build_skills_router(SkillPackageStore(db), blob, AuditLog(max_entries=50)))
    return TestClient(app)


class SkillRouterTest(unittest.TestCase):
    def test_upload_publish_catalog_download(self):
        c = _skills_client()
        zip_bytes = b"PK\x03\x04 fake skill zip"
        r = c.post("/v1/skills/packages",
                   data={"skill_id": "svc-restart", "version": "1.0.0",
                         "name": "Service Restart", "risk_level": "medium"},
                   files={"file": ("svc.zip", zip_bytes, "application/zip")})
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json()["status"], "draft")
        self.assertEqual(c.get("/v1/skills/catalog").json(), [])  # draft hidden
        self.assertEqual(
            c.post("/v1/skills/packages/svc-restart/versions/1.0.0/publish").status_code, 200)
        cat = c.get("/v1/skills/catalog").json()
        self.assertEqual(len(cat), 1)
        dl = c.get("/v1/skills/catalog/svc-restart/versions/1.0.0/download")
        self.assertEqual(dl.content, zip_bytes)
        resp = c.get("/v1/skills/sync/manifest?since=0").json()
        self.assertEqual(len(resp["items"]), 1)
        self.assertGreaterEqual(resp["catalog_version"], 1)
