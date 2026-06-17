"""Tests for Platform Blob API (Task 4)."""

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
