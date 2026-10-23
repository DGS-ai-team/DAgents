"""Release 安装包元数据 store。"""

from __future__ import annotations

import threading
from functools import cmp_to_key

from manage.releases.models import DEFAULT_ARTIFACT, DEFAULT_CHANNEL, ReleasePackage, ReleasePackageCreate
from manage.releases.semver import compare_versions, upgrade_available
from manage.storage.sqlite import SQLiteDatabase


class ReleasePackageStore:
    def __init__(self, db: SQLiteDatabase | None = None) -> None:
        self._db = db if (db and db.enabled) else None
        self._lock = threading.RLock()
        self._mem: dict[tuple[str, str, str, str], ReleasePackage] = {}

    @staticmethod
    def _key(artifact: str, channel: str, platform: str, version: str) -> tuple[str, str, str, str]:
        return (artifact, channel, platform, version)

    def _save(self, pkg: ReleasePackage) -> None:
        key = self._key(pkg.artifact, pkg.channel, pkg.platform, pkg.version)
        if self._db is None:
            self._mem[key] = pkg
            return
        with self._db.connect() as conn:
            conn.execute(
                "INSERT INTO release_packages(artifact,channel,platform,version,payload_json) "
                "VALUES(?,?,?,?,?) "
                "ON CONFLICT(artifact,channel,platform,version) DO UPDATE SET payload_json=excluded.payload_json",
                (pkg.artifact, pkg.channel, pkg.platform, pkg.version, pkg.model_dump_json()),
            )
            conn.commit()

    def _save_many(self, packages: list[ReleasePackage]) -> None:
        if not packages:
            return
        if self._db is None:
            for pkg in packages:
                self._mem[self._key(pkg.artifact, pkg.channel, pkg.platform, pkg.version)] = pkg
            return
        with self._db.connect() as conn:
            conn.executemany(
                "INSERT INTO release_packages(artifact,channel,platform,version,payload_json) "
                "VALUES(?,?,?,?,?) "
                "ON CONFLICT(artifact,channel,platform,version) DO UPDATE SET payload_json=excluded.payload_json",
                [
                    (
                        pkg.artifact,
                        pkg.channel,
                        pkg.platform,
                        pkg.version,
                        pkg.model_dump_json(),
                    )
                    for pkg in packages
                ],
            )
            conn.commit()

    def _all(self) -> list[ReleasePackage]:
        if self._db is None:
            return list(self._mem.values())
        with self._db.connect() as conn:
            return [
                ReleasePackage.model_validate_json(r["payload_json"])
                for r in conn.execute("SELECT payload_json FROM release_packages")
            ]

    def get(self, artifact: str, channel: str, platform: str, version: str) -> ReleasePackage | None:
        key = self._key(artifact, channel, platform, version)
        if self._db is None:
            return self._mem.get(key)
        return next(
            (
                p
                for p in self._all()
                if p.artifact == artifact
                and p.channel == channel
                and p.platform == platform
                and p.version == version
            ),
            None,
        )

    def list_packages(
        self,
        *,
        artifact: str | None = None,
        channel: str | None = None,
        platform: str | None = None,
        status: str | None = None,
    ) -> list[ReleasePackage]:
        rows = self._all()
        if artifact:
            rows = [p for p in rows if p.artifact == artifact]
        if channel:
            rows = [p for p in rows if p.channel == channel]
        if platform:
            rows = [p for p in rows if p.platform == platform]
        if status and status != "all":
            rows = [p for p in rows if p.status == status]
        def compare_packages(left: ReleasePackage, right: ReleasePackage) -> int:
            left_group = (left.platform, left.channel, left.artifact)
            right_group = (right.platform, right.channel, right.artifact)
            if left_group != right_group:
                return -1 if left_group < right_group else 1
            by_version = compare_versions(right.version, left.version)
            if by_version:
                return by_version
            return (right.updated_at > left.updated_at) - (right.updated_at < left.updated_at)

        return sorted(rows, key=cmp_to_key(compare_packages))

    def create_draft(self, payload: ReleasePackageCreate, now: int) -> ReleasePackage:
        with self._lock:
            existing = self.get(payload.artifact, payload.channel, payload.platform, payload.version)
            if existing and existing.status != "draft":
                raise ValueError("version already published")
            pkg = ReleasePackage(
                status="draft",
                is_latest=False,
                created_at=existing.created_at if existing else now,
                updated_at=now,
                **payload.model_dump(),
            )
            self._save(pkg)
            return pkg

    def publish(
        self,
        artifact: str,
        channel: str,
        platform: str,
        version: str,
        now: int,
        *,
        set_latest: bool = False,
    ) -> ReleasePackage | None:
        with self._lock:
            pkg = self.get(artifact, channel, platform, version)
            if not pkg:
                return None
            if pkg.status != "published":
                pkg.status = "published"
                pkg.published_at = now
            elif pkg.published_at is None:
                # Fill the field for packages published before published_at existed.
                pkg.published_at = pkg.updated_at
            pkg.updated_at = now
            changed = [pkg]
            if set_latest:
                changed.extend(self._clear_latest(artifact, channel, platform, except_version=version))
                pkg.is_latest = True
            self._save_many(changed)
            return pkg

    def promote(self, artifact: str, channel: str, platform: str, version: str, now: int) -> ReleasePackage | None:
        with self._lock:
            pkg = self.get(artifact, channel, platform, version)
            if not pkg or pkg.status != "published":
                return None
            changed = self._clear_latest(artifact, channel, platform, except_version=version)
            pkg.is_latest = True
            pkg.updated_at = now
            changed.append(pkg)
            self._save_many(changed)
            return pkg

    def _clear_latest(self, artifact: str, channel: str, platform: str, *, except_version: str) -> list[ReleasePackage]:
        changed = []
        for pkg in self._all():
            if (
                pkg.artifact == artifact
                and pkg.channel == channel
                and pkg.platform == platform
                and pkg.is_latest
                and pkg.version != except_version
            ):
                pkg.is_latest = False
                changed.append(pkg)
        return changed

    def get_latest(self, artifact: str, channel: str, platform: str) -> ReleasePackage | None:
        matches = [
            p
            for p in self._all()
            if p.artifact == artifact
            and p.channel == channel
            and p.platform == platform
            and p.status == "published"
            and p.is_latest
        ]
        return matches[0] if matches else None

    def delete(self, artifact: str, channel: str, platform: str, version: str) -> ReleasePackage | None:
        with self._lock:
            pkg = self.get(artifact, channel, platform, version)
            if not pkg:
                return None
            if pkg.is_latest:
                raise ValueError("cannot delete latest release")
            key = self._key(artifact, channel, platform, version)
            if self._db is None:
                self._mem.pop(key, None)
            else:
                with self._db.connect() as conn:
                    conn.execute(
                        "DELETE FROM release_packages WHERE artifact=? AND channel=? AND platform=? AND version=?",
                        (artifact, channel, platform, version),
                    )
                    conn.commit()
            return pkg

    def check(
        self,
        *,
        current: str,
        platform: str,
        channel: str = DEFAULT_CHANNEL,
        artifact: str = DEFAULT_ARTIFACT,
    ) -> dict:
        latest_pkg = self.get_latest(artifact, channel, platform)
        latest_version = latest_pkg.version if latest_pkg else current
        available = bool(latest_pkg) and upgrade_available(current, latest_version)
        asset = None
        if latest_pkg and available:
            asset = {
                "download_url": (
                    f"/v1/releases/packages/{artifact}/{channel}/{platform}/latest/download"
                ),
                "sha256": latest_pkg.sha256,
                "size_bytes": latest_pkg.size_bytes,
                "filename": latest_pkg.filename,
                "source": "manage_hosted",
                "origin": latest_pkg.source,
            }
        return {
            "artifact": artifact,
            "platform": platform,
            "channel": channel,
            "current": current,
            "latest": latest_version,
            "upgrade_available": available,
            "platform_supported": latest_pkg is not None,
            "published_at": (latest_pkg.published_at or latest_pkg.updated_at) if latest_pkg else None,
            "release_notes": latest_pkg.release_notes if latest_pkg else "",
            "asset": asset,
        }
