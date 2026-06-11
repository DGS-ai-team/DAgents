"""Register Center 的存储实现。"""

from __future__ import annotations

import json
import os
import threading
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Literal

from rc_models import AgentRecord, AgentStoredRecord, AgentUpsertRequest
from rc_status import AgentStatus, derive_status, offline_grace_seconds


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
    return AgentRecord(
        **record.model_dump(mode="python"),
        status=status,
    )


def _migrate_stored_dict(data: dict[str, object]) -> dict[str, object]:
    migrated = dict(data)
    registered = int(migrated.get("registered_at_unix") or 0)
    if "updated_at_unix" not in migrated:
        migrated["updated_at_unix"] = registered
    if "last_seen_unix" not in migrated:
        migrated["last_seen_unix"] = registered
    agent_id = str(migrated.get("agent_id") or "")
    if not migrated.get("name"):
        migrated["name"] = agent_id
    for key in ("description", "owner", "team", "version"):
        migrated.setdefault(key, "")
    for key in ("capabilities_hint", "capabilities", "tools", "skills", "allowed_scopes"):
        if key not in migrated or migrated[key] is None:
            migrated[key] = []
    if "metadata" not in migrated or not isinstance(migrated.get("metadata"), dict):
        migrated["metadata"] = {}
    migrated.setdefault("auth_method", "shared_token")
    migrated.setdefault("risk_level", "medium")
    migrated.setdefault("last_error_summary", None)
    migrated.setdefault("recent_task_summary", None)
    return migrated


class AgentRegistryStore:
    """Agent 登记信息的内存仓库。"""

    def __init__(self, persist_path: str | os.PathLike[str] | None = None) -> None:
        self._lock = threading.RLock()
        self._records: dict[str, AgentStoredRecord] = {}
        self._persist_path = Path(persist_path) if persist_path else None
        self._load_from_disk()
        with self._lock:
            if self._prune_expired_locked():
                self._persist_locked()

    def upsert(self, payload: AgentUpsertRequest) -> AgentRecord:
        now_unix = int(time.time())
        with self._lock:
            existing = self._records.get(payload.agent_id)
            registered_at = existing.registered_at_unix if existing else now_unix
            stored = AgentStoredRecord(
                agent_id=payload.agent_id,
                base_url=payload.base_url,
                discovery_group=payload.discovery_group,
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
                metadata=payload.metadata,
                last_error_summary=payload.last_error_summary,
                recent_task_summary=payload.recent_task_summary,
                registered_at_unix=registered_at,
                updated_at_unix=now_unix,
                last_seen_unix=now_unix,
                expires_at_unix=now_unix + int(payload.ttl_seconds),
            )
            self._records[payload.agent_id] = stored
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
        end = start + page_size
        return filtered[start:end], total

    def list_deliverable(self, *, discovery_group: str | None = None) -> list[AgentRecord]:
        query = AgentListQuery(discovery_group=discovery_group, status="online", page=1, page_size=10_000)
        items, _ = self.list(query)
        return items

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

    def _prune_expired_locked(self) -> bool:
        now_unix = int(time.time())
        grace = offline_grace_seconds()
        expired_ids = [
            agent_id
            for agent_id, item in self._records.items()
            if derive_status(now_unix=now_unix, expires_at_unix=item.expires_at_unix, grace_seconds=grace)
            == "expired"
        ]
        for agent_id in expired_ids:
            self._records.pop(agent_id, None)
        if expired_ids:
            self._persist_locked()
        return bool(expired_ids)

    def _load_from_disk(self) -> None:
        if self._persist_path is None or not self._persist_path.exists():
            return
        raw = json.loads(self._persist_path.read_text(encoding="utf-8"))
        records = raw.get("agents", []) if isinstance(raw, dict) else []
        loaded: dict[str, AgentStoredRecord] = {}
        for item in records:
            if not isinstance(item, dict):
                continue
            migrated = _migrate_stored_dict(item)
            record = AgentStoredRecord.model_validate(migrated)
            loaded[record.agent_id] = record
        with self._lock:
            self._records = loaded

    def _persist_locked(self) -> None:
        if self._persist_path is None:
            return
        self._persist_path.parent.mkdir(parents=True, exist_ok=True)
        payload = {
            "schema_version": 2,
            "agents": [
                item.model_dump(mode="json")
                for item in sorted(self._records.values(), key=lambda value: value.agent_id)
            ],
        }
        tmp_path = self._persist_path.with_name(f".{self._persist_path.name}.{os.getpid()}.tmp")
        tmp_path.write_text(json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True), encoding="utf-8")
        tmp_path.replace(self._persist_path)

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
