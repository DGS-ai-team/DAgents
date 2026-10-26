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
from manage.releases.files import safe_release_filename, validate_slug
from manage.releases.routes import build_releases_router
from manage.releases.seed import _find_package_file, seed_bundled_releases
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

    def test_release_filename_must_be_a_single_safe_component(self):
        for filename in ("../escape.exe", "..\\escape.exe", "C:\\temp\\escape.exe", "folder/name.zip", "name.exe "):
            with self.subTest(filename=filename), self.assertRaises(ValueError):
                safe_release_filename(filename)
        self.assertEqual(safe_release_filename("installer.exe"), "installer.exe")
        for field in ("artifact", "version", "platform", "channel"):
            with self.subTest(field=field), self.assertRaises(ValueError):
                validate_slug("..", field)


class ReleaseStoreTest(unittest.TestCase):
    def test_versions_are_listed_in_semantic_order(self):
        store = ReleasePackageStore()
        from manage.releases.models import ReleasePackageCreate

        for version in ("0.9.0", "0.10.0"):
            store.create_draft(
                ReleasePackageCreate(
                    artifact="dagents-local-assistant",
                    version=version,
                    platform="windows-amd64",
                    channel="stable",
                    filename=f"installer-{version}.exe",
                    sha256="a" * 64,
                    size_bytes=10,
                    rel_path=f"dagents-local-assistant/stable/windows-amd64/{version}/installer-{version}.exe",
                ),
                now=1,
            )
        self.assertEqual(
            [pkg.version for pkg in store.list_packages(platform="windows-amd64")],
            ["0.10.0", "0.9.0"],
        )

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
        self.assertEqual(pub.published_at, 2)
        self.assertFalse(pub.is_latest)
        promoted = store.promote("dagents-local-assistant", "stable", "linux-amd64", "0.5.2", now=3)
        self.assertTrue(promoted.is_latest)
        self.assertEqual(promoted.published_at, 2)
        self.assertEqual(store.check(current="0.5.1", platform="linux-amd64")["published_at"], 2)
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
        ranged = client.get(
            "/v1/releases/packages/dagents-local-assistant/stable/linux-amd64/latest/download",
            headers={"range": "bytes=0-4"},
        )
        self.assertEqual(ranged.status_code, 206, ranged.text)
        self.assertEqual(ranged.content, data[:5])
        self.assertEqual(ranged.headers["content-range"], f"bytes 0-4/{len(data)}")
        self.assertEqual(
            dl.headers["x-release-sha256"],
            hashlib.sha256(data).hexdigest(),
        )
        self.assertIn(
            "dagents-local-assistant-linux-amd64-0.5.2.tar.gz",
            dl.headers["content-disposition"],
        )
        stored = releases_dir / "dagents-local-assistant/stable/linux-amd64/0.5.2"
        self.assertTrue(stored.is_dir())

    def test_upload_rejects_unsafe_filename_and_cleans_partial_uploads(self):
        client, _, releases_dir = _release_client()
        oversized = client.post(
            "/v1/releases/packages",
            data={"version": "0.5.3", "platform": "linux-amd64"},
            files={"file": ("large.zip", b"x" * (1024 * 1024 + 1), "application/zip")},
        )
        self.assertEqual(oversized.status_code, 413, oversized.text)
        self.assertEqual(list(releases_dir.rglob(".upload-*.tmp")), [])
        self.assertEqual(list(releases_dir.rglob("*.zip")), [])

        empty = client.post(
            "/v1/releases/packages",
            data={"version": "0.5.4", "platform": "linux-amd64"},
            files={"file": ("empty.zip", b"", "application/zip")},
        )
        self.assertEqual(empty.status_code, 422, empty.text)
        self.assertEqual(list(releases_dir.rglob(".upload-*.tmp")), [])

    def test_publish_and_promote_require_matching_package_content(self):
        client, store, releases_dir = _release_client()
        response = client.post(
            "/v1/releases/packages",
            data={"version": "0.12.0", "platform": "windows-amd64"},
            files={"file": ("installer.exe", b"verified-installer", "application/octet-stream")},
        )
        self.assertEqual(response.status_code, 200, response.text)
        package = store.get("dagents-local-assistant", "stable", "windows-amd64", "0.12.0")
        path = releases_dir / package.rel_path
        path.unlink()

        publish = client.post(
            "/v1/releases/packages/dagents-local-assistant/stable/windows-amd64/0.12.0/publish",
            json={"set_latest": False},
        )
        self.assertEqual(publish.status_code, 409, publish.text)
        self.assertEqual(store.get("dagents-local-assistant", "stable", "windows-amd64", "0.12.0").status, "draft")

        path.write_bytes(b"verified-installer")
        publish = client.post(
            "/v1/releases/packages/dagents-local-assistant/stable/windows-amd64/0.12.0/publish",
            json={"set_latest": False},
        )
        self.assertEqual(publish.status_code, 200, publish.text)
        path.write_bytes(b"tampered-installer")
        promote = client.post(
            "/v1/releases/packages/dagents-local-assistant/stable/windows-amd64/0.12.0/promote"
        )
        self.assertEqual(promote.status_code, 409, promote.text)
        self.assertIsNone(store.get_latest("dagents-local-assistant", "stable", "windows-amd64"))

    def test_public_list_hides_missing_files_and_public_latest_download_works(self):
        client, _, releases_dir = _release_client()
        anonymous = TestClient(client.app)
        self.assertEqual(anonymous.get("/v1/releases/packages").status_code, 401)
        for version, filename, data in (
            ("0.12.0", "good.exe", b"good-installer"),
            ("0.12.1", "missing.exe", b"missing-installer"),
        ):
            response = client.post(
                "/v1/releases/packages",
                data={
                    "version": version,
                    "platform": "windows-amd64",
                    "publish": "true",
                    "set_latest": "true" if version == "0.12.0" else "false",
                },
                files={"file": (filename, data, "application/octet-stream")},
            )
            self.assertEqual(response.status_code, 200, response.text)
            if version == "0.12.1":
                (releases_dir / response.json()["rel_path"]).unlink()

        listing = anonymous.get("/v1/releases/public/packages")
        self.assertEqual(listing.status_code, 200, listing.text)
        self.assertEqual([row["version"] for row in listing.json()], ["0.12.0"])
        self.assertIsNotNone(listing.json()[0]["published_at"])
        latest = anonymous.get("/v1/releases/public/latest/stable/windows-amd64/download")
        self.assertEqual(latest.status_code, 200, latest.text)
        self.assertEqual(latest.content, b"good-installer")

    def test_public_downloads_include_only_published_dagents_packages(self):
        client, _, _ = _release_client()
        installer = "dagents-local-assistant-windows-amd64-installer-0.12.0.exe"
        data = b"fake-public-installer"
        for version, publish in [("0.12.0", "true"), ("0.13.0", "false")]:
            response = client.post(
                "/v1/releases/packages",
                data={
                    "version": version,
                    "platform": "windows-amd64",
                    "channel": "stable",
                    "publish": publish,
                    "set_latest": "true",
                },
                files={"file": (installer, data, "application/octet-stream")},
            )
            self.assertEqual(response.status_code, 200, response.text)

        anonymous = TestClient(client.app)
        listing = anonymous.get("/v1/releases/public/packages")
        self.assertEqual(listing.status_code, 200, listing.text)
        self.assertEqual([row["version"] for row in listing.json()], ["0.12.0"])
        self.assertNotIn("uploaded_by", listing.json()[0])
        self.assertNotIn("rel_path", listing.json()[0])

        downloaded = anonymous.get(
            "/v1/releases/public/packages/stable/windows-amd64/0.12.0/download"
        )
        self.assertEqual(downloaded.status_code, 200)
        self.assertEqual(downloaded.content, data)
        self.assertIn(installer, downloaded.headers["content-disposition"])
        self.assertEqual(
            downloaded.headers["x-release-sha256"],
            hashlib.sha256(data).hexdigest(),
        )
        self.assertEqual(
            anonymous.get(
                "/v1/releases/public/packages/stable/windows-amd64/0.13.0/download"
            ).status_code,
            404,
        )

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
    def test_seed_manifest_cannot_escape_release_directory(self):
        root = Path(tempfile.mkdtemp())
        version_dir = root / "bundle" / "0.12.0"
        version_dir.mkdir(parents=True)
        (root / "outside.exe").write_bytes(b"not-a-release")
        self.assertIsNone(
            _find_package_file(version_dir, {"filename": "../outside.exe"})
        )

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
