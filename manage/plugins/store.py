"""Hook plugin package store (in-memory + optional SQLite)."""

from __future__ import annotations

import threading

from manage.plugins.models import PluginPackage, PluginPackageCreate
from manage.storage.sqlite import SQLiteDatabase


class PluginPackageStore:
    def __init__(self, db: SQLiteDatabase | None = None) -> None:
        self._db = db if (db and db.enabled) else None
        self._lock = threading.RLock()
        self._mem: dict[tuple[str, str], PluginPackage] = {}
        self._mem_ver = 0

    def _save(self, pkg: PluginPackage) -> None:
        if self._db is None:
            self._mem[(pkg.plugin_id, pkg.version)] = pkg
            return
        with self._db.connect() as conn:
            conn.execute(
                "INSERT INTO plugin_packages(plugin_id,version,payload_json) VALUES(?,?,?) "
                "ON CONFLICT(plugin_id,version) DO UPDATE SET payload_json=excluded.payload_json",
                (pkg.plugin_id, pkg.version, pkg.model_dump_json()),
            )
            conn.commit()

    def _all(self) -> list[PluginPackage]:
        if self._db is None:
            return list(self._mem.values())
        with self._db.connect() as conn:
            return [
                PluginPackage.model_validate_json(r["payload_json"])
                for r in conn.execute("SELECT payload_json FROM plugin_packages")
            ]

    def catalog_version(self) -> int:
        if self._db is None:
            return self._mem_ver
        with self._db.connect() as conn:
            row = conn.execute(
                "SELECT value FROM schema_meta WHERE key='plugins_catalog_version'"
            ).fetchone()
            return int(row["value"]) if row else 0

    def _bump_version(self) -> int:
        if self._db is None:
            self._mem_ver += 1
            return self._mem_ver
        with self._db.connect() as conn:
            cur = self.catalog_version() + 1
            conn.execute(
                "INSERT INTO schema_meta(key,value) VALUES('plugins_catalog_version',?) "
                "ON CONFLICT(key) DO UPDATE SET value=?",
                (str(cur), str(cur)),
            )
            conn.commit()
            return cur

    def create(self, payload: PluginPackageCreate, now: int) -> PluginPackage:
        with self._lock:
            pkg = PluginPackage(
                status="draft",
                created_at=now,
                updated_at=now,
                **payload.model_dump(),
            )
            self._save(pkg)
            return pkg

    def get_version(self, plugin_id: str, version: str) -> PluginPackage | None:
        return next(
            (p for p in self._all() if p.plugin_id == plugin_id and p.version == version),
            None,
        )

    def publish(self, plugin_id: str, version: str, now: int) -> PluginPackage | None:
        with self._lock:
            pkg = self.get_version(plugin_id, version)
            if not pkg:
                return None
            if pkg.status == "published":
                return pkg
            pkg.status = "published"
            pkg.updated_at = now
            pkg.catalog_seq = self._bump_version()
            self._save(pkg)
            return pkg

    def get(self, plugin_id: str) -> list[PluginPackage]:
        return [p for p in self._all() if p.plugin_id == plugin_id]

    def catalog(self) -> list[PluginPackage]:
        return sorted(
            [p for p in self._all() if p.status == "published"],
            key=lambda p: p.catalog_seq,
        )

    def published_ids(self) -> set[str]:
        return {p.plugin_id for p in self.catalog()}

    def sync_manifest(self, since: int) -> list[dict]:
        return [
            {
                "plugin_id": p.plugin_id,
                "version": p.version,
                "platform": p.platform,
                "blob_id": p.blob_id,
                "download_url": (
                    f"/v1/plugins/catalog/{p.plugin_id}/versions/{p.version}/download"
                ),
            }
            for p in self.catalog()
            if p.catalog_seq > since
        ]
