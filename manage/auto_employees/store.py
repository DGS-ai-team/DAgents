from __future__ import annotations

import threading
from datetime import datetime, timezone

from manage.storage.sqlite import SQLiteDatabase

from .models import AutoSummary, AutoSummaryRecord


class AutoSummaryConflict(Exception):
    pass


class AutoSummaryStore:
    """Independent durable snapshot store; registry TTL cleanup never touches it."""

    def __init__(self, db: SQLiteDatabase | None = None) -> None:
        self._db = db if db and db.enabled else None
        self._lock = threading.RLock()
        self._mem: dict[tuple[str, str], AutoSummaryRecord] = {}

    def _read(self, node_id: str, agent_id: str) -> AutoSummaryRecord | None:
        if self._db is None:
            return self._mem.get((node_id, agent_id))
        with self._db.connect() as conn:
            row = conn.execute("SELECT payload_json, received_at FROM auto_employee_summaries WHERE node_id=? AND agent_id=?", (node_id, agent_id)).fetchone()
        if row is None:
            return None
        summary = AutoSummary.model_validate_json(row[0])
        return self._freshness(AutoSummaryRecord(**summary.model_dump(), node_id=node_id, received_at=row[1]))

    @staticmethod
    def _freshness(record: AutoSummaryRecord, *, now: datetime | None = None, stale_after_seconds: int = 120) -> AutoSummaryRecord:
        current = now or datetime.now(timezone.utc)
        age = max(0, int((current - record.received_at).total_seconds()))
        return record.model_copy(update={"age_seconds": age, "stale": age > max(0, stale_after_seconds)})

    def get(self, node_id: str, agent_id: str) -> AutoSummaryRecord | None:
        with self._lock:
            return self._read(node_id.strip(), agent_id.strip())

    def put(self, node_id: str, summary: AutoSummary, received_at: datetime | None = None) -> AutoSummaryRecord:
        node_id = node_id.strip()
        received_at = received_at or datetime.now(timezone.utc)
        with self._lock:
            old = self._read(node_id, summary.agent_id)
            if old is not None:
                if summary.as_of < old.as_of:
                    raise AutoSummaryConflict("summary is older than the stored snapshot")
                if summary.as_of == old.as_of:
                    old_summary = AutoSummary(**{key: value for key, value in old.model_dump().items() if key not in {"node_id", "received_at", "stale", "age_seconds"}})
                    if old_summary == summary:
                        return self._freshness(old)
                    raise AutoSummaryConflict("summary conflicts at the same as_of")
            record = self._freshness(AutoSummaryRecord(**summary.model_dump(), node_id=node_id, received_at=received_at))
            if self._db is None:
                self._mem[(node_id, summary.agent_id)] = record
                return record
            with self._db.connect() as conn:
                conn.execute("INSERT INTO auto_employee_summaries(node_id,agent_id,payload_json,received_at) VALUES(?,?,?,?) ON CONFLICT(node_id,agent_id) DO UPDATE SET payload_json=excluded.payload_json, received_at=excluded.received_at", (node_id, summary.agent_id, summary.model_dump_json(), received_at.isoformat()))
                conn.commit()
            return record

    def list(self, *, node_id: str | None = None, state: str | None = None, page: int = 1, page_size: int = 50, stale_after_seconds: int = 120, now: datetime | None = None) -> tuple[list[AutoSummaryRecord], int]:
        with self._lock:
            if self._db is None:
                records = [self._freshness(record, now=now, stale_after_seconds=stale_after_seconds) for record in self._mem.values()]
            else:
                clauses: list[str] = []
                args: list[str] = []
                if node_id is not None:
                    clauses.append("node_id=?")
                    args.append(node_id)
                if state is not None:
                    clauses.append("json_extract(payload_json, '$.state')=?")
                    args.append(state)
                where = (" WHERE " + " AND ".join(clauses)) if clauses else ""
                offset = (page - 1) * page_size
                with self._db.connect() as conn:
                    total = int(conn.execute("SELECT COUNT(*) FROM auto_employee_summaries" + where, args).fetchone()[0])
                    rows = conn.execute("SELECT node_id, payload_json, received_at FROM auto_employee_summaries" + where + " ORDER BY json_extract(payload_json, '$.as_of') DESC, node_id DESC, agent_id DESC LIMIT ? OFFSET ?", [*args, page_size, offset]).fetchall()
                records = [self._freshness(AutoSummaryRecord(**AutoSummary.model_validate_json(row[1]).model_dump(), node_id=row[0], received_at=row[2]), now=now, stale_after_seconds=stale_after_seconds) for row in rows]
                return records, total
            records = [x for x in records if (node_id is None or x.node_id == node_id) and (state is None or x.state == state)]
            records.sort(key=lambda x: (x.as_of, x.node_id, x.agent_id), reverse=True)
            total = len(records)
            start = (page - 1) * page_size
            return records[start : start + page_size], total
