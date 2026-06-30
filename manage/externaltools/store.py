"""External tool package store (in-memory + optional SQLite)."""

from __future__ import annotations

import threading

from manage.externaltools.models import ExternalToolPackage, ExternalToolPackageCreate
from manage.storage.sqlite import SQLiteDatabase


class ExternalToolPackageStore:
    def __init__(self, db: SQLiteDatabase | None = None) -> None:
        self._db = db if (db and db.enabled) else None
        self._lock = threading.RLock()
        self._mem: dict[tuple[str, str], ExternalToolPackage] = {}
        self._mem_ver = 0

    def _save(self, pkg: ExternalToolPackage) -> None:
        if self._db is None:
            self._mem[(pkg.tool_id, pkg.version)] = pkg
            return
        with self._db.connect() as conn:
            conn.execute(
                "INSERT INTO externaltool_packages(tool_id,version,payload_json) VALUES(?,?,?) "
                "ON CONFLICT(tool_id,version) DO UPDATE SET payload_json=excluded.payload_json",
                (pkg.tool_id, pkg.version, pkg.model_dump_json()),
            )
            conn.commit()

    def _all(self) -> list[ExternalToolPackage]:
        if self._db is None:
            return list(self._mem.values())
        with self._db.connect() as conn:
            return [
                ExternalToolPackage.model_validate_json(r["payload_json"])
                for r in conn.execute("SELECT payload_json FROM externaltool_packages")
            ]

    def catalog_version(self) -> int:
        if self._db is None:
            return self._mem_ver
        with self._db.connect() as conn:
            row = conn.execute(
                "SELECT value FROM schema_meta WHERE key='externaltools_catalog_version'"
            ).fetchone()
            return int(row["value"]) if row else 0

    def _bump_version(self) -> int:
        if self._db is None:
            self._mem_ver += 1
            return self._mem_ver
        with self._db.connect() as conn:
            cur = self.catalog_version() + 1
            conn.execute(
                "INSERT INTO schema_meta(key,value) VALUES('externaltools_catalog_version',?) "
                "ON CONFLICT(key) DO UPDATE SET value=?",
                (str(cur), str(cur)),
            )
            conn.commit()
            return cur

    def create(self, payload: ExternalToolPackageCreate, now: int) -> ExternalToolPackage:
        with self._lock:
            pkg = ExternalToolPackage(
                status="draft",
                created_at=now,
                updated_at=now,
                **payload.model_dump(),
            )
            self._save(pkg)
            return pkg

    def get_version(self, tool_id: str, version: str) -> ExternalToolPackage | None:
        return next(
            (p for p in self._all() if p.tool_id == tool_id and p.version == version),
            None,
        )

    def publish(self, tool_id: str, version: str, now: int) -> ExternalToolPackage | None:
        with self._lock:
            pkg = self.get_version(tool_id, version)
            if not pkg:
                return None
            if pkg.status == "published":
                return pkg
            pkg.status = "published"
            pkg.updated_at = now
            pkg.catalog_seq = self._bump_version()
            self._save(pkg)
            return pkg

    def get(self, tool_id: str) -> list[ExternalToolPackage]:
        return [p for p in self._all() if p.tool_id == tool_id]

    def catalog(self) -> list[ExternalToolPackage]:
        return sorted(
            [p for p in self._all() if p.status == "published"],
            key=lambda p: p.catalog_seq,
        )

    def sync_manifest(self, since: int) -> list[dict]:
        return [
            {
                "tool_id": p.tool_id,
                "version": p.version,
                "platform": p.platform,
                "blob_id": p.blob_id,
                "download_url": (
                    f"/v1/externaltools/catalog/{p.tool_id}/versions/{p.version}/download"
                ),
            }
            for p in self.catalog()
            if p.catalog_seq > since
        ]
