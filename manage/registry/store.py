"""Registry Agent 存储（内存 + 可选 SQLite）。"""

from __future__ import annotations

import json
import threading
import time
from dataclasses import dataclass
from typing import Literal

from manage.registry.models import (
    AgentDiscoverRecord,
    AgentHeartbeatRequest,
    AgentRecord,
    AgentRegisterRequest,
    AgentStoredRecord,
)
from manage.registry.status import AgentStatus, derive_status, offline_grace_seconds
from manage.storage.sqlite import SQLiteDatabase


@dataclass(frozen=True)
class AgentListQuery:
    discovery_group: str | None = None
    team: str | None = None
    status: AgentStatus | Literal["all"] = "online"
    q: str | None = None
    page: int = 1
    page_size: int = 50


def stored_to_public(record: AgentStoredRecord, *, now_unix: int | None = None) -> AgentRecord:
    now = int(time.time()) if now_unix is None else now_unix
    status = derive_status(now_unix=now, expires_at_unix=record.expires_at_unix)
    return AgentRecord(**record.model_dump(mode="python"), status=status)


def _migrate_stored_dict(data: dict[str, object]) -> dict[str, object]:
    migrated = dict(data)
    registered = int(migrated.get("registered_at_unix") or 0)
    if "updated_at_unix" not in migrated:
        migrated["updated_at_unix"] = registered
    if "last_seen_unix" not in migrated:
        migrated["last_seen_unix"] = registered
    agent_id = str(migrated.get("agent_id") or "")
    node_id = str(migrated.get("node_id") or "").strip()
    if not node_id:
        node_id = agent_id
    migrated["node_id"] = node_id
    if not migrated.get("name"):
        migrated["name"] = agent_id or node_id
    for key in ("description", "owner", "team", "version"):
        migrated.setdefault(key, "")
    for key in ("capabilities_hint", "capabilities", "tools", "skills", "allowed_scopes"):
        if key not in migrated or migrated[key] is None:
            migrated[key] = []
    if "metadata" not in migrated or not isinstance(migrated.get("metadata"), dict):
        migrated["metadata"] = {}
    meta = migrated["metadata"]
    if isinstance(meta, dict):
        meta.setdefault("node_id", node_id)
    if "card" not in migrated or not isinstance(migrated.get("card"), dict):
        migrated["card"] = {}
    migrated.setdefault("auth_method", "shared_token")
    migrated.setdefault("risk_level", "medium")
    migrated.pop("expose_to_peers", None)
    migrated.setdefault("last_error_summary", None)
    migrated.setdefault("recent_task_summary", None)
    return migrated


def record_node_id(record: AgentStoredRecord | AgentRecord) -> str:
    """解析记录的 node 身份（缺省回退 agent_id）。"""
    node = str(getattr(record, "node_id", "") or "").strip()
    if node:
        return node
    return str(getattr(record, "agent_id", "") or "").strip()


class AgentRegistryStore:
    def __init__(self, db: SQLiteDatabase | None = None) -> None:
        self._lock = threading.RLock()
        self._records: dict[str, AgentStoredRecord] = {}
        self._group_catalog: set[str] = set()
        self._db = db
        self._load_from_db()

    def register(self, payload: AgentRegisterRequest) -> AgentRecord:
        now_unix = int(time.time())
        with self._lock:
            node_key = (payload.node_id or payload.agent_id).strip()
            existing = self._records.get(node_key)
            registered_at = existing.registered_at_unix if existing else now_unix
            discovery_group = existing.discovery_group if existing else []
            stored = AgentStoredRecord(
                agent_id=node_key,
                node_id=node_key,
                base_url=payload.base_url,
                discovery_group=discovery_group,
                capabilities_hint=payload.capabilities_hint,
                name=payload.name,
                description=payload.description,
                owner=payload.owner,
                team=payload.team,
                capabilities=payload.capabilities,
                tools=payload.tools,
                skills=payload.skills,
                auth_method=payload.auth_method,
                risk_level=payload.risk_level,
                allowed_scopes=payload.allowed_scopes,
                version=payload.version,
                card=dict(payload.card),
                metadata=payload.metadata,
                last_error_summary=payload.last_error_summary,
                recent_task_summary=payload.recent_task_summary,
                registered_at_unix=registered_at,
                updated_at_unix=now_unix,
                last_seen_unix=now_unix,
                expires_at_unix=now_unix + int(payload.ttl_seconds),
            )
            self._records[node_key] = stored
            self._persist_locked()
            return stored_to_public(stored, now_unix=now_unix)

    def update_groups(self, agent_id: str, discovery_group: list[str]) -> AgentRecord | None:
        with self._lock:
            existing = self._records.get(agent_id)
            if existing is None:
                return None
            now_unix = int(time.time())
            data = existing.model_dump(mode="python")
            cleaned = [g.strip() for g in discovery_group if str(g).strip()]
            data["discovery_group"] = cleaned
            data["updated_at_unix"] = now_unix
            stored = AgentStoredRecord.model_validate(data)
            self._records[agent_id] = stored
            for name in cleaned:
                self._group_catalog.add(name)
            self._persist_locked()
            self._persist_catalog_locked()
            return stored_to_public(stored, now_unix=now_unix)

    def list_discovery_groups(self) -> list[dict[str, object]]:
        """聚合目录 + 节点上已出现的 discovery_group。"""
        with self._lock:
            names = set(self._group_catalog)
            members: dict[str, list[str]] = {n: [] for n in names}
            for rec in self._records.values():
                for g in rec.discovery_group:
                    name = str(g).strip()
                    if not name:
                        continue
                    names.add(name)
                    members.setdefault(name, []).append(rec.agent_id)
            rows: list[dict[str, object]] = []
            for name in sorted(names):
                node_ids = sorted(set(members.get(name, [])))
                rows.append(
                    {
                        "name": name,
                        "node_count": len(node_ids),
                        "node_ids": node_ids,
                        "in_catalog": name in self._group_catalog,
                    }
                )
            return rows

    def create_discovery_group(self, name: str) -> dict[str, object]:
        cleaned = name.strip()
        if not cleaned:
            raise ValueError("group name required")
        with self._lock:
            self._group_catalog.add(cleaned)
            self._persist_catalog_locked()
            node_ids = sorted(
                rec.agent_id for rec in self._records.values() if cleaned in rec.discovery_group
            )
            return {
                "name": cleaned,
                "node_count": len(node_ids),
                "node_ids": node_ids,
                "in_catalog": True,
            }

    def delete_discovery_group(self, name: str, *, detach_nodes: bool = True) -> bool:
        cleaned = name.strip()
        if not cleaned:
            return False
        with self._lock:
            existed = cleaned in self._group_catalog or any(
                cleaned in rec.discovery_group for rec in self._records.values()
            )
            self._group_catalog.discard(cleaned)
            if detach_nodes:
                now_unix = int(time.time())
                for agent_id, rec in list(self._records.items()):
                    if cleaned not in rec.discovery_group:
                        continue
                    data = rec.model_dump(mode="python")
                    data["discovery_group"] = [g for g in rec.discovery_group if g != cleaned]
                    data["updated_at_unix"] = now_unix
                    self._records[agent_id] = AgentStoredRecord.model_validate(data)
                self._persist_locked()
            self._persist_catalog_locked()
            return existed

    def heartbeat(self, agent_id: str, payload: AgentHeartbeatRequest) -> AgentRecord | None:
        with self._lock:
            existing = self._records.get(agent_id)
            if existing is None:
                return None
            now_unix = int(time.time())
            data = existing.model_dump(mode="python")
            data["updated_at_unix"] = now_unix
            data["last_seen_unix"] = now_unix
            data["expires_at_unix"] = now_unix + int(payload.ttl_seconds)
            if payload.version:
                data["version"] = payload.version.strip()
            if payload.tools:
                data["tools"] = payload.tools
            if payload.skills:
                data["skills"] = payload.skills
            if payload.last_error_summary is not None:
                data["last_error_summary"] = payload.last_error_summary
            if payload.recent_task_summary is not None:
                data["recent_task_summary"] = payload.recent_task_summary
            data.pop("expose_to_peers", None)
            if not data.get("node_id"):
                data["node_id"] = agent_id
            stored = AgentStoredRecord.model_validate(data)
            self._records[agent_id] = stored
            self._persist_locked()
            return stored_to_public(stored, now_unix=now_unix)

    def get(self, agent_id: str) -> AgentRecord | None:
        with self._lock:
            self._prune_expired_locked()
            stored = self._records.get(agent_id)
            if stored is None:
                return None
            return stored_to_public(stored)

    def list(self, query: AgentListQuery) -> tuple[list[AgentRecord], int]:
        with self._lock:
            self._prune_expired_locked()
            now_unix = int(time.time())
            records = [stored_to_public(item, now_unix=now_unix) for item in self._records.values()]
        filtered = self._apply_filters(records, query)
        total = len(filtered)
        page = max(1, query.page)
        page_size = max(1, min(200, query.page_size))
        start = (page - 1) * page_size
        return filtered[start : start + page_size], total

    def discover(self, *, discovery_group: str | None, caller_groups: list[str] | None = None) -> list[AgentDiscoverRecord]:
        query = AgentListQuery(discovery_group=discovery_group, status="online", page=1, page_size=10_000)
        items, _ = self.list(query)
        out: list[AgentDiscoverRecord] = []
        for item in items:
            if caller_groups:
                if not any(group in item.discovery_group for group in caller_groups):
                    continue
            caps = list(item.capabilities)
            seen = set(caps)
            for hint in item.capabilities_hint:
                if hint not in seen:
                    caps.append(hint)
                    seen.add(hint)
            out.append(
                AgentDiscoverRecord(
                    agent_id=item.agent_id,
                    node_id=record_node_id(item) or item.agent_id,
                    discovery_group=item.discovery_group,
                    capabilities=caps,
                    capabilities_hint=item.capabilities_hint,
                    name=item.name,
                    description=item.description,
                    team=item.team,
                    risk_level=item.risk_level,
                    version=item.version,
                    card=dict(item.card),
                )
            )
        return out

    @staticmethod
    def _apply_filters(records: list[AgentRecord], query: AgentListQuery) -> list[AgentRecord]:
        filtered = records
        if query.discovery_group is not None:
            filtered = [item for item in filtered if query.discovery_group in item.discovery_group]
        if query.team:
            team = query.team.strip()
            filtered = [item for item in filtered if item.team == team]
        if query.status != "all":
            filtered = [item for item in filtered if item.status == query.status]
        if query.q:
            needle = query.q.strip().lower()
            filtered = [
                item
                for item in filtered
                if needle in item.agent_id.lower()
                or needle in item.name.lower()
                or needle in item.description.lower()
            ]
        return sorted(filtered, key=lambda item: item.agent_id)

    def delete(self, agent_id: str) -> bool:
        with self._lock:
            if agent_id not in self._records:
                return False
            del self._records[agent_id]
            self._persist_locked()
            return True

    def count(self) -> int:
        with self._lock:
            self._prune_expired_locked()
            return len(self._records)

    def _prune_expired_locked(self) -> bool:
        now_unix = int(time.time())
        grace = offline_grace_seconds()
        expired_ids = [
            agent_id
            for agent_id, item in self._records.items()
            if derive_status(now_unix=now_unix, expires_at_unix=item.expires_at_unix, grace_seconds=grace) == "expired"
        ]
        for agent_id in expired_ids:
            self._records.pop(agent_id, None)
        if expired_ids:
            self._persist_locked()
        return bool(expired_ids)

    def _load_from_db(self) -> None:
        if self._db is None or not self._db.enabled:
            return
        with self._lock, self._db.connect() as conn:
            rows = conn.execute("SELECT agent_id, payload_json FROM registry_agents").fetchall()
            try:
                catalog_rows = conn.execute("SELECT name FROM discovery_group_catalog").fetchall()
            except Exception:
                catalog_rows = []
        loaded: dict[str, AgentStoredRecord] = {}
        for row in rows:
            try:
                raw = json.loads(str(row["payload_json"]))
            except json.JSONDecodeError:
                continue
            if not isinstance(raw, dict):
                continue
            migrated = _migrate_stored_dict(raw)
            record = AgentStoredRecord.model_validate(migrated)
            loaded[record.agent_id] = record
        catalog = {str(row["name"]).strip() for row in catalog_rows if str(row["name"]).strip()}
        with self._lock:
            self._records = loaded
            self._group_catalog = catalog
            for rec in loaded.values():
                for g in rec.discovery_group:
                    if str(g).strip():
                        self._group_catalog.add(str(g).strip())

    def _persist_catalog_locked(self) -> None:
        if self._db is None or not self._db.enabled:
            return
        now_unix = int(time.time())
        with self._db.connect() as conn:
            conn.execute("DELETE FROM discovery_group_catalog")
            for name in sorted(self._group_catalog):
                conn.execute(
                    "INSERT INTO discovery_group_catalog(name, created_at_unix) VALUES (?, ?)",
                    (name, now_unix),
                )
            conn.commit()

    def _persist_locked(self) -> None:
        if self._db is None or not self._db.enabled:
            return
        with self._db.connect() as conn:
            conn.execute("DELETE FROM registry_agents")
            for item in self._records.values():
                conn.execute(
                    "INSERT INTO registry_agents(agent_id, payload_json) VALUES (?, ?)",
                    (item.agent_id, json.dumps(item.model_dump(mode="json"), ensure_ascii=False)),
                )
            conn.commit()

    @classmethod
    def import_rc_json(cls, db: SQLiteDatabase, raw_json: dict[str, object]) -> int:
        store = cls(db=db)
        agents = raw_json.get("agents", []) if isinstance(raw_json, dict) else []
        count = 0
        for item in agents:
            if not isinstance(item, dict):
                continue
            migrated = _migrate_stored_dict(item)
            record = AgentStoredRecord.model_validate(migrated)
            store._records[record.agent_id] = record
            count += 1
        with store._lock:
            store._persist_locked()
        return count
