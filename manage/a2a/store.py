"""A2A Task 存储（内存 + 可选 SQLite；inbox long poll + per-agent 索引）。"""

from __future__ import annotations

import json
import threading
import time
import uuid
from typing import Callable

from manage.a2a.models import (
    InboxTaskItem,
    TaskCreateRequest,
    TaskRecord,
    TaskReplyRequest,
    TaskStatus,
    TaskStoredRecord,
)
from manage.storage.sqlite import SQLiteDatabase

DEFAULT_INBOX_CONTENT_MAX_CHARS = 4096
DEFAULT_EXPIRE_SWEEP_SECONDS = 30


def _new_task_id() -> str:
    return f"a2a-task-{uuid.uuid4().hex[:16]}"


def inbox_content_preview(content: str, *, max_chars: int) -> tuple[str, bool]:
    if max_chars <= 0 or len(content) <= max_chars:
        return content, False
    return content[:max_chars] + "…", True


def stored_to_inbox_item(record: TaskStoredRecord, *, max_content_chars: int = DEFAULT_INBOX_CONTENT_MAX_CHARS) -> InboxTaskItem:
    preview, truncated = inbox_content_preview(record.content, max_chars=max_content_chars)
    return InboxTaskItem(
        task_id=record.task_id,
        from_agent_id=record.from_agent_id,
        kind=record.kind,
        content=preview,
        content_truncated=truncated,
        blob_ids=list(record.blob_ids),
        caller_session_id=record.caller_session_id,
        trace_id=record.trace_id,
        created_at_unix=record.created_at_unix,
        expires_at_unix=record.expires_at_unix,
    )


class A2ATaskStore:
    def __init__(
        self,
        db: SQLiteDatabase | None = None,
        *,
        inbox_content_max_chars: int = DEFAULT_INBOX_CONTENT_MAX_CHARS,
        expire_sweep_seconds: int = DEFAULT_EXPIRE_SWEEP_SECONDS,
    ) -> None:
        self._lock = threading.RLock()
        self._cond = threading.Condition(self._lock)
        self._records: dict[str, TaskStoredRecord] = {}
        self._idempotency: dict[tuple[str, str], str] = {}
        self._pending: dict[str, list[str]] = {}
        self._db = db
        self._inbox_content_max_chars = max(0, inbox_content_max_chars)
        self._expire_sweep_seconds = max(0, expire_sweep_seconds)
        self._stop_sweep = threading.Event()
        self._sweep_thread: threading.Thread | None = None
        self._load_from_db()
        if self._expire_sweep_seconds > 0:
            self._sweep_thread = threading.Thread(target=self._run_expire_sweep, name="a2a-expire-sweep", daemon=True)
            self._sweep_thread.start()

    def close(self) -> None:
        self._stop_sweep.set()

    def sweep_expired(self) -> int:
        """主动过期扫描（单测 / 运维）；返回过期条数。"""
        with self._lock:
            return self._expire_due_locked()

    def count_pending_for(self, agent_id: str) -> int:
        with self._lock:
            return len(self._pending.get(agent_id.strip(), []))

    def create(
        self,
        payload: TaskCreateRequest,
        *,
        validate_target: Callable[[str], tuple[bool, str | None]],
    ) -> tuple[TaskRecord, bool]:
        """返回 (task, created)。idempotency 命中时 created=False。"""
        key = payload.idempotency_key.strip()
        if key:
            with self._lock:
                existing_id = self._idempotency.get((payload.from_agent_id, key))
                if existing_id:
                    existing = self._records.get(existing_id)
                    if existing is not None:
                        return TaskRecord(**existing.model_dump(mode="python")), False

        ok, reason = validate_target(payload.to_agent_id)
        if not ok:
            raise ValueError(reason or "target_invalid")

        now = int(time.time())
        stored = TaskStoredRecord(
            task_id=_new_task_id(),
            from_agent_id=payload.from_agent_id.strip(),
            to_agent_id=payload.to_agent_id.strip(),
            kind=payload.kind,
            content=payload.content,
            blob_ids=list(payload.blob_ids),
            caller_session_id=payload.caller_session_id.strip(),
            idempotency_key=key,
            trace_id=payload.trace_id.strip(),
            status="queued",
            created_at_unix=now,
            updated_at_unix=now,
            expires_at_unix=now + int(payload.ttl_seconds),
        )
        with self._lock:
            self._put_record_locked(stored)
            if key:
                self._idempotency[(stored.from_agent_id, key)] = stored.task_id
            self._cond.notify_all()
        return TaskRecord(**stored.model_dump(mode="python")), True

    def get(self, task_id: str) -> TaskRecord | None:
        with self._lock:
            self._expire_one_locked(task_id)
            stored = self._records.get(task_id)
            if stored is None:
                return None
            return TaskRecord(**stored.model_dump(mode="python"))

    def list_tasks(
        self,
        *,
        to_agent_id: str | None = None,
        from_agent_id: str | None = None,
        status: TaskStatus | None = None,
        limit: int = 50,
        offset: int = 0,
    ) -> tuple[list[TaskRecord], int]:
        """只读列举 Task（不 deliver）；按 created_at 降序。"""
        to_filter = (to_agent_id or "").strip()
        from_filter = (from_agent_id or "").strip()
        limit = max(1, min(200, limit))
        offset = max(0, offset)
        with self._lock:
            self._expire_due_locked()
            rows: list[TaskStoredRecord] = []
            for stored in self._records.values():
                if to_filter and stored.to_agent_id != to_filter:
                    continue
                if from_filter and stored.from_agent_id != from_filter:
                    continue
                if status is not None and stored.status != status:
                    continue
                rows.append(stored)
            rows.sort(key=lambda item: (-item.created_at_unix, item.task_id))
            total = len(rows)
            page = rows[offset : offset + limit]
            tasks = [TaskRecord(**item.model_dump(mode="python")) for item in page]
            return tasks, total

    def poll_inbox(self, agent_id: str, *, limit: int = 10, wait_seconds: float = 0.0) -> tuple[list[InboxTaskItem], int]:
        cleaned = agent_id.strip()
        limit = max(1, min(50, limit))
        deadline = time.time() + max(0.0, wait_seconds)
        while True:
            with self._lock:
                pending = self._queued_for_agent_locked(cleaned)
                if pending:
                    selected = pending[:limit]
                    items: list[InboxTaskItem] = []
                    now = int(time.time())
                    for stored in selected:
                        updated = self._mark_delivered_locked(stored, now=now)
                        items.append(
                            stored_to_inbox_item(updated, max_content_chars=self._inbox_content_max_chars)
                        )
                    remaining = self._queued_count_locked(cleaned)
                    return items, remaining
                remaining_pending = self._queued_count_locked(cleaned)
                if wait_seconds <= 0:
                    return [], remaining_pending
            if time.time() >= deadline:
                with self._lock:
                    return [], self._queued_count_locked(cleaned)
            with self._cond:
                timeout = min(1.0, max(0.0, deadline - time.time()))
                if timeout > 0:
                    self._cond.wait(timeout=timeout)

    def ack(self, task_id: str, agent_id: str) -> TaskRecord | None:
        with self._lock:
            self._expire_one_locked(task_id)
            stored = self._records.get(task_id)
            if stored is None or stored.to_agent_id != agent_id.strip():
                return None
            if stored.status in ("completed", "failed", "expired"):
                return TaskRecord(**stored.model_dump(mode="python"))
            now = int(time.time())
            if stored.status == "queued":
                stored = self._mark_delivered_locked(stored, now=now)
            updated = self._replace_record_locked(
                stored,
                status="processing",
                updated_at_unix=now,
            )
            return TaskRecord(**updated.model_dump(mode="python"))

    def reply(self, task_id: str, agent_id: str, payload: TaskReplyRequest) -> TaskRecord | None:
        with self._lock:
            self._expire_one_locked(task_id)
            stored = self._records.get(task_id)
            if stored is None or stored.to_agent_id != agent_id.strip():
                return None
            if stored.status in ("completed", "failed", "expired"):
                return TaskRecord(**stored.model_dump(mode="python"))
            now = int(time.time())
            if payload.status == "requires_input":
                task_status: TaskStatus = "awaiting_caller"
            elif payload.status == "failed":
                task_status = "failed"
            else:
                task_status = "completed"
            updated = self._replace_record_locked(
                stored,
                status=task_status,
                updated_at_unix=now,
                result_text=payload.result_text,
                result_status=payload.status,
                callee_session_id=payload.callee_session_id.strip(),
                error_detail=payload.error_detail.strip(),
                pending_caller_resume={},
            )
            self._cond.notify_all()
            return TaskRecord(**updated.model_dump(mode="python"))

    def submit_caller_notify(self, task_id: str, caller_agent_id: str) -> TaskRecord | None:
        """Caller 已收到 requires_input 并中继至本地 TUI（awaiting_caller → caller_notified）。"""
        with self._lock:
            self._expire_one_locked(task_id)
            stored = self._records.get(task_id)
            if stored is None or stored.from_agent_id != caller_agent_id.strip():
                return None
            if stored.status == "caller_notified":
                return TaskRecord(**stored.model_dump(mode="python"))
            if stored.status != "awaiting_caller":
                return None
            now = int(time.time())
            updated = self._replace_record_locked(
                stored,
                status="caller_notified",
                updated_at_unix=now,
            )
            self._cond.notify_all()
            return TaskRecord(**updated.model_dump(mode="python"))

    def submit_caller_resume(
        self,
        task_id: str,
        caller_agent_id: str,
        resume_value: dict[str, object],
    ) -> TaskRecord | None:
        with self._lock:
            self._expire_one_locked(task_id)
            stored = self._records.get(task_id)
            if stored is None or stored.from_agent_id != caller_agent_id.strip():
                return None
            if stored.status not in ("awaiting_caller", "caller_notified"):
                if stored.status == "caller_responded":
                    return TaskRecord(**stored.model_dump(mode="python"))
                return None
            now = int(time.time())
            updated = self._replace_record_locked(
                stored,
                status="caller_responded",
                updated_at_unix=now,
                pending_caller_resume=dict(resume_value or {}),
            )
            self._cond.notify_all()
            return TaskRecord(**updated.model_dump(mode="python"))

    def poll_caller_input(
        self,
        task_id: str,
        callee_agent_id: str,
        *,
        wait_seconds: float = 0.0,
    ) -> tuple[dict[str, object] | None, TaskRecord | None]:
        cleaned = callee_agent_id.strip()
        deadline = time.time() + max(0.0, wait_seconds)
        while True:
            with self._lock:
                self._expire_one_locked(task_id)
                stored = self._records.get(task_id)
                if stored is None or stored.to_agent_id != cleaned:
                    return None, None
                if stored.status == "processing":
                    return None, TaskRecord(**stored.model_dump(mode="python"))
                if stored.status not in ("awaiting_caller", "caller_notified", "caller_responded"):
                    return None, TaskRecord(**stored.model_dump(mode="python"))
                pending = dict(stored.pending_caller_resume or {})
                if pending:
                    updated = self._replace_record_locked(
                        stored,
                        status="processing",
                        pending_caller_resume={},
                        updated_at_unix=int(time.time()),
                    )
                    return pending, TaskRecord(**updated.model_dump(mode="python"))
                if wait_seconds <= 0:
                    return None, TaskRecord(**stored.model_dump(mode="python"))
            if time.time() >= deadline:
                with self._lock:
                    stored = self._records.get(task_id)
                    if stored is None:
                        return None, None
                    return None, TaskRecord(**stored.model_dump(mode="python"))
            with self._cond:
                timeout = min(1.0, max(0.0, deadline - time.time()))
                if timeout > 0:
                    self._cond.wait(timeout=timeout)

    def _mark_delivered_locked(self, stored: TaskStoredRecord, *, now: int) -> TaskStoredRecord:
        if stored.status != "queued":
            return stored
        return self._replace_record_locked(
            stored,
            status="delivered",
            updated_at_unix=now,
            delivered_at_unix=now,
        )

    def _replace_record_locked(self, stored: TaskStoredRecord, **changes: object) -> TaskStoredRecord:
        data = stored.model_dump(mode="python")
        data.update(changes)
        updated = TaskStoredRecord.model_validate(data)
        self._put_record_locked(updated, previous=stored)
        return updated

    def _put_record_locked(self, record: TaskStoredRecord, *, previous: TaskStoredRecord | None = None) -> None:
        if previous is None:
            previous = self._records.get(record.task_id)
        self._records[record.task_id] = record
        self._sync_pending_index_locked(previous, record)
        self._upsert_task_locked(record)

    def _sync_pending_index_locked(self, before: TaskStoredRecord | None, after: TaskStoredRecord) -> None:
        task_id = after.task_id
        if before is not None and before.status == "queued":
            self._pending_remove_locked(before.to_agent_id, task_id)
        if after.status == "queued":
            self._pending_add_locked(after)

    def _pending_add_locked(self, record: TaskStoredRecord) -> None:
        agent_id = record.to_agent_id
        bucket = self._pending.setdefault(agent_id, [])
        if record.task_id not in bucket:
            bucket.append(record.task_id)

    def _pending_remove_locked(self, agent_id: str, task_id: str) -> None:
        bucket = self._pending.get(agent_id)
        if not bucket:
            return
        self._pending[agent_id] = [item for item in bucket if item != task_id]
        if not self._pending[agent_id]:
            del self._pending[agent_id]

    def _rebuild_pending_index_locked(self) -> None:
        self._pending.clear()
        queued = [item for item in self._records.values() if item.status == "queued"]
        for record in sorted(queued, key=lambda item: (item.created_at_unix, item.task_id)):
            self._pending_add_locked(record)

    def _queued_for_agent_locked(self, agent_id: str) -> list[TaskStoredRecord]:
        ids = self._pending.get(agent_id, [])
        out: list[TaskStoredRecord] = []
        for task_id in ids:
            stored = self._records.get(task_id)
            if stored is None or stored.status != "queued":
                continue
            out.append(stored)
        return out

    def _queued_count_locked(self, agent_id: str) -> int:
        return len(self._queued_for_agent_locked(agent_id))

    def _expire_one_locked(self, task_id: str) -> bool:
        stored = self._records.get(task_id)
        if stored is None:
            return False
        if stored.status in ("completed", "failed", "expired"):
            return False
        now = int(time.time())
        if stored.expires_at_unix > now:
            return False
        self._replace_record_locked(stored, status="expired", updated_at_unix=now)
        return True

    def _expire_due_locked(self) -> int:
        now = int(time.time())
        expired = 0
        for task_id, stored in list(self._records.items()):
            if stored.status in ("completed", "failed", "expired"):
                continue
            if stored.expires_at_unix > now:
                continue
            self._replace_record_locked(stored, status="expired", updated_at_unix=now)
            expired += 1
        return expired

    def _run_expire_sweep(self) -> None:
        while not self._stop_sweep.wait(self._expire_sweep_seconds):
            with self._lock:
                count = self._expire_due_locked()
            if count:
                with self._cond:
                    self._cond.notify_all()

    def _load_from_db(self) -> None:
        if self._db is None or not self._db.enabled:
            return
        with self._lock, self._db.connect() as conn:
            rows = conn.execute("SELECT task_id, payload_json FROM a2a_tasks").fetchall()
        loaded: dict[str, TaskStoredRecord] = {}
        idempotency: dict[tuple[str, str], str] = {}
        for row in rows:
            try:
                raw = json.loads(str(row["payload_json"]))
            except json.JSONDecodeError:
                continue
            if not isinstance(raw, dict):
                continue
            record = TaskStoredRecord.model_validate(raw)
            loaded[record.task_id] = record
            if record.idempotency_key:
                idempotency[(record.from_agent_id, record.idempotency_key)] = record.task_id
        with self._lock:
            self._records = loaded
            self._idempotency = idempotency
            self._rebuild_pending_index_locked()

    def _upsert_task_locked(self, record: TaskStoredRecord) -> None:
        if self._db is None or not self._db.enabled:
            return
        payload = json.dumps(record.model_dump(mode="json"), ensure_ascii=False)
        with self._db.connect() as conn:
            conn.execute(
                """
                INSERT INTO a2a_tasks(task_id, payload_json) VALUES (?, ?)
                ON CONFLICT(task_id) DO UPDATE SET payload_json = excluded.payload_json
                """,
                (record.task_id, payload),
            )
            conn.commit()
