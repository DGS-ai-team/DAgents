"""Tests for Manage Release Hub."""

import hashlib
import json
import os
import tempfile
import unittest
from pathlib import Path

from fastapi import FastAPI
from fastapi.testclient import TestClient

from manage.platform.audit import AuditLog
from manage.releases.routes import build_releases_router
from manage.releases.seed import seed_bundled_releases
from manage.releases.semver import compare_versions, upgrade_available
from manage.releases.store import ReleasePackageStore
from manage.storage.sqlite import SQLiteDatabase

TEST_ADMIN_TOKEN = "test-manage-admin-token"
os.environ.setdefault("MANAGE_TOKENS", json.dumps([{"id": "test-admin", "token": TEST_ADMIN_TOKEN, "role": "admin"}]))


def _release_client(tmp: Path | None = None):
    root = tmp or Path(tempfile.mkdtemp())
    releases_dir = root / "releases"
    db = SQLiteDatabase(root / "m.db")
    store = ReleasePackageStore(db=db)
    audit = AuditLog(max_entries=50)
    app = FastAPI()
    app.include_router(
        build_releases_router(
            store,
            audit,
            releases_dir=releases_dir,
            release_max_bytes=1024 * 1024,
        )
    )
    return TestClient(app, headers={"x-dagents-a2a-token": TEST_ADMIN_TOKEN}), store, releases_dir


class SemverTest(unittest.TestCase):
    def test_upgrade_available(self):
        self.assertTrue(upgrade_available("0.5.1", "0.5.2"))
        self.assertFalse(upgrade_available("0.5.2", "0.5.1"))
        self.assertEqual(compare_versions("0.5.1", "0.5.1"), 0)


class ReleaseStoreTest(unittest.TestCase):
    def test_draft_publish_promote(self):
        store = ReleasePackageStore(SQLiteDatabase(Path(tempfile.mkdtemp()) / "m.db"))
        from manage.releases.models import ReleasePackageCreate

        payload = ReleasePackageCreate(
            artifact="dagents-local-assistant",
            version="0.5.2",
            platform="linux-amd64",
            channel="stable",
            filename="pkg.tar.gz",
            sha256="a" * 64,
            size_bytes=10,
            rel_path="dagents-local-assistant/stable/linux-amd64/0.5.2/pkg.tar.gz",
        )
        store.create_draft(payload, now=1)
        self.assertEqual(store.list_packages(status="draft"), [store.get(
            "dagents-local-assistant", "stable", "linux-amd64", "0.5.2"
        )])
        pub = store.publish("dagents-local-assistant", "stable", "linux-amd64", "0.5.2", now=2)
        self.assertEqual(pub.status, "published")
        self.assertFalse(pub.is_latest)
        promoted = store.promote("dagents-local-assistant", "stable", "linux-amd64", "0.5.2", now=3)
        self.assertTrue(promoted.is_latest)
        check = store.check(current="0.5.1", platform="linux-amd64")
        self.assertTrue(check["upgrade_available"])


class ReleaseRoutesTest(unittest.TestCase):
    def test_upload_draft_then_publish(self):
        client, store, releases_dir = _release_client()
        data = b"fake-release-tar-gz"
        r = client.post(
            "/v1/releases/packages",
            data={
                "artifact": "dagents-local-assistant",
                "version": "0.5.2",
                "platform": "linux-amd64",
                "channel": "stable",
                "publish": "false",
            },
            files={"file": ("dagents-local-assistant-linux-amd64-0.5.2.tar.gz", data, "application/gzip")},
        )
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json()["status"], "draft")
        pub = client.post(
            "/v1/releases/packages/dagents-local-assistant/stable/linux-amd64/0.5.2/publish",
            json={"set_latest": True},
        )
        self.assertEqual(pub.status_code, 200, pub.text)
        self.assertTrue(pub.json()["is_latest"])
        check = client.get(
            "/v1/releases/check",
            params={"current": "0.5.1", "platform": "linux-amd64"},
        )
        self.assertEqual(check.status_code, 200, check.text)
        self.assertTrue(check.json()["upgrade_available"])
        dl = client.get(
            "/v1/releases/packages/dagents-local-assistant/stable/linux-amd64/latest/download"
        )
        self.assertEqual(dl.status_code, 200)
        self.assertEqual(dl.content, data)
        self.assertEqual(
            dl.headers["x-release-sha256"],
            hashlib.sha256(data).hexdigest(),
        )
        stored = releases_dir / "dagents-local-assistant/stable/linux-amd64/0.5.2"
        self.assertTrue(stored.is_dir())

    def test_upload_windows_installer(self):
        client, store, releases_dir = _release_client()
        data = b"MZ-fake-inno-installer"
        r = client.post(
            "/v1/releases/packages",
            data={
                "artifact": "dagents-local-assistant",
                "version": "0.10.5",
                "platform": "windows-386",
                "channel": "stable",
                "publish": "true",
                "set_latest": "true",
            },
            files={
                "file": (
                    "dagents-local-assistant-windows-386-installer-0.10.5.exe",
                    data,
                    "application/octet-stream",
                )
            },
        )
        self.assertEqual(r.status_code, 200, r.text)
        self.assertEqual(r.json()["platform"], "windows-386")
        self.assertEqual(r.json()["status"], "published")
        self.assertEqual(
            (releases_dir / r.json()["rel_path"]).read_bytes(),
            data,
        )
        self.assertEqual(
            store.get_latest("dagents-local-assistant", "stable", "windows-386").filename,
            "dagents-local-assistant-windows-386-installer-0.10.5.exe",
        )

    def test_windows_release_checks_never_fallback_across_architectures(self):
        client, _, _ = _release_client()

        def upload(platform: str, version: str, filename: str, payload: bytes):
            response = client.post(
                "/v1/releases/packages",
                data={
                    "artifact": "dagents-local-assistant",
                    "version": version,
                    "platform": platform,
                    "channel": "stable",
                    "publish": "true",
                    "set_latest": "true",
                },
                files={"file": (filename, payload, "application/octet-stream")},
            )
            self.assertEqual(response.status_code, 200, response.text)

        upload(
            "windows-amd64",
            "0.12.0",
            "dagents-local-assistant-windows-amd64-installer-0.12.0.exe",
            b"x64-release",
        )

        # A new x64 package must not be advertised to a 32-bit client.
        check_386 = client.get(
            "/v1/releases/check",
            params={"current": "0.11.0", "platform": "windows-386"},
        )
        self.assertEqual(check_386.status_code, 200, check_386.text)
        self.assertEqual(check_386.json()["latest"], "0.11.0")
        self.assertFalse(check_386.json()["upgrade_available"])
        self.assertFalse(check_386.json()["platform_supported"])
        self.assertIsNone(check_386.json()["asset"])
        latest_386 = client.get(
            "/v1/releases/packages/dagents-local-assistant/stable/windows-386/latest/download"
        )
        self.assertEqual(latest_386.status_code, 404)

        # If an older x86 package exists, x86 clients continue to get that
        # platform's own latest release rather than the newer x64 package.
        old_x86_filename = "dagents-local-assistant-windows-386-installer-0.10.5.exe"
        upload("windows-386", "0.10.5", old_x86_filename, b"old-x86-release")
        check_386 = client.get(
            "/v1/releases/check",
            params={"current": "0.10.0", "platform": "windows-386"},
        )
        self.assertEqual(check_386.status_code, 200, check_386.text)
        payload = check_386.json()
        self.assertEqual(payload["latest"], "0.10.5")
        self.assertTrue(payload["upgrade_available"])
        self.assertTrue(payload["platform_supported"])
        self.assertEqual(payload["asset"]["filename"], old_x86_filename)
        self.assertIn("/windows-386/latest/download", payload["asset"]["download_url"])


class ReleaseSeedTest(unittest.TestCase):
    def test_seed_bundled_releases(self):
        root = Path(tempfile.mkdtemp())
        bundled = root / "bundled"
        releases = root / "releases"
        version_dir = (
            bundled / "dagents-local-assistant/stable/linux-amd64/0.5.1"
        )
        version_dir.mkdir(parents=True)
        pkg = version_dir / "dagents-local-assistant-linux-amd64-0.5.1.tar.gz"
        pkg.write_bytes(b"seed-package")
        store = ReleasePackageStore(SQLiteDatabase(root / "m.db"))
        count = seed_bundled_releases(
            bundled_dir=bundled,
            releases_dir=releases,
            store=store,
            enabled=True,
        )
        self.assertEqual(count, 1)
        latest = store.get_latest("dagents-local-assistant", "stable", "linux-amd64")
        self.assertEqual(latest.version, "0.5.1")
        self.assertTrue((releases / latest.rel_path).is_file())
