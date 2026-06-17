"""Skill package store (in-memory + optional SQLite)."""

from __future__ import annotations

import threading

from manage.storage.sqlite import SQLiteDatabase
from manage.skills.models import SkillPackage, SkillPackageCreate


class SkillPackageStore:
    def __init__(self, db: SQLiteDatabase | None = None) -> None:
        self._db = db if (db and db.enabled) else None
        self._lock = threading.RLock()
        self._mem: dict[tuple[str, str], SkillPackage] = {}
        self._mem_ver = 0

    def _save(self, pkg: SkillPackage) -> None:
        if self._db is None:
            self._mem[(pkg.skill_id, pkg.version)] = pkg
            return
        with self._db.connect() as conn:
            conn.execute(
                "INSERT INTO skill_packages(skill_id,version,payload_json) VALUES(?,?,?) "
                "ON CONFLICT(skill_id,version) DO UPDATE SET payload_json=excluded.payload_json",
                (pkg.skill_id, pkg.version, pkg.model_dump_json()),
            )
            conn.commit()

    def _all(self) -> list[SkillPackage]:
        if self._db is None:
            return list(self._mem.values())
        with self._db.connect() as conn:
            return [
                SkillPackage.model_validate_json(r["payload_json"])
                for r in conn.execute("SELECT payload_json FROM skill_packages")
            ]

    def catalog_version(self) -> int:
        if self._db is None:
            return self._mem_ver
        with self._db.connect() as conn:
            row = conn.execute(
                "SELECT value FROM schema_meta WHERE key='skills_catalog_version'"
            ).fetchone()
            return int(row["value"]) if row else 0

    def _bump_version(self) -> int:
        if self._db is None:
            self._mem_ver += 1
            return self._mem_ver
        with self._db.connect() as conn:
            cur = self.catalog_version() + 1
            conn.execute(
                "INSERT INTO schema_meta(key,value) VALUES('skills_catalog_version',?) "
                "ON CONFLICT(key) DO UPDATE SET value=?",
                (str(cur), str(cur)),
            )
            conn.commit()
            return cur

    def create(self, payload: SkillPackageCreate, now: int) -> SkillPackage:
        with self._lock:
            pkg = SkillPackage(status="draft", created_at=now, updated_at=now, **payload.model_dump())
            self._save(pkg)
            return pkg

    def get_version(self, skill_id: str, version: str) -> SkillPackage | None:
        return next(
            (p for p in self._all() if p.skill_id == skill_id and p.version == version),
            None,
        )

    def publish(self, skill_id: str, version: str, now: int) -> SkillPackage | None:
        with self._lock:
            pkg = self.get_version(skill_id, version)
            if not pkg:
                return None
            pkg.status = "published"
            pkg.updated_at = now
            pkg.catalog_seq = self._bump_version()
            self._save(pkg)
            return pkg

    def get(self, skill_id: str) -> list[SkillPackage]:
        return [p for p in self._all() if p.skill_id == skill_id]

    def catalog(self) -> list[SkillPackage]:
        return sorted(
            [p for p in self._all() if p.status == "published"],
            key=lambda p: p.catalog_seq,
        )

    def sync_manifest(self, since: int) -> list[dict]:
        return [
            {
                "skill_id": p.skill_id,
                "version": p.version,
                "blob_id": p.blob_id,
                "download_url": f"/v1/skills/catalog/{p.skill_id}/versions/{p.version}/download",
            }
            for p in self.catalog()
            if p.catalog_seq > since
        ]
