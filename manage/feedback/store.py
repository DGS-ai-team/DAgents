from __future__ import annotations
import threading
from .models import FeedbackCreate, FeedbackPatch, FeedbackRecord
from manage.storage.sqlite import SQLiteDatabase


class FeedbackConflict(Exception):
    pass


class FeedbackStore:
    def __init__(self, db: SQLiteDatabase | None = None):
        self._db = db if db and db.enabled else None
        self._lock = threading.RLock()
        self._mem: dict[tuple[str, str], FeedbackRecord] = {}

    def _get_unlocked(self, node_id: str, client_id: str):
        key = (node_id, client_id)
        if self._db is None:
            return self._mem.get(key)
        with self._db.connect() as c:
            row = c.execute(
                "SELECT payload_json FROM feedback WHERE node_id=? AND client_feedback_id=?", key
            ).fetchone()
            return FeedbackRecord.model_validate_json(row[0]) if row else None

    def get(self, node_id, client_id):
        with self._lock:
            return self._get_unlocked(node_id, client_id)

    def _save(self, rec):
        key = (rec.node_id, rec.client_feedback_id)
        if self._db is None:
            self._mem[key] = rec
            return
        with self._db.connect() as c:
            c.execute(
                "INSERT INTO feedback(node_id,client_feedback_id,payload_json) VALUES(?,?,?) ON CONFLICT(node_id,client_feedback_id) DO UPDATE SET payload_json=excluded.payload_json",
                (*key, rec.model_dump_json()),
            )
            c.commit()

    def create(self, payload: FeedbackCreate, now: str):
        with self._lock:
            old = self._get_unlocked(payload.node_id, payload.client_feedback_id)
            if old:
                data = payload.model_dump()
                data.update(
                    created_at=old.created_at,
                    status=old.status,
                    reply=old.reply,
                    revision=old.revision,
                    updated_at=old.updated_at,
                )
                comparable = FeedbackRecord(**data)
                if comparable.model_dump() != old.model_dump():
                    raise FeedbackConflict("client_feedback_id already exists with different content")
                return old, False
            data = payload.model_dump()
            data.update(created_at=payload.created_at or now, updated_at=now)
            rec = FeedbackRecord(**data)
            if self._db is not None:
                with self._db.connect() as c:
                    cur = c.execute(
                        "INSERT OR IGNORE INTO feedback(node_id,client_feedback_id,payload_json) VALUES(?,?,?)",
                        (rec.node_id, rec.client_feedback_id, rec.model_dump_json()),
                    )
                    c.commit()
                    if cur.rowcount == 0:
                        old = self._get_unlocked(rec.node_id, rec.client_feedback_id)
                        if old is not None:
                            data = payload.model_dump()
                            data.update(
                                created_at=old.created_at,
                                status=old.status,
                                reply=old.reply,
                                revision=old.revision,
                                updated_at=old.updated_at,
                            )
                            if FeedbackRecord(**data).model_dump() != old.model_dump():
                                raise FeedbackConflict("client_feedback_id already exists with different content")
                            return old, False
            else:
                self._save(rec)
            return rec, True

    def patch(self, node_id, client_id, payload: FeedbackPatch, now: str):
        with self._lock:
            if self._db is not None:
                with self._db.connect() as c:
                    row = c.execute(
                        "SELECT payload_json FROM feedback WHERE node_id=? AND client_feedback_id=?",
                        (node_id, client_id),
                    ).fetchone()
                    if not row:
                        return None
                    old = FeedbackRecord.model_validate_json(row[0])
                    if old.revision != payload.revision:
                        raise FeedbackConflict("revision conflict")
                    data = old.model_dump()
                    if payload.status is not None:
                        data["status"] = payload.status
                    if payload.reply is not None:
                        data["reply"] = payload.reply
                    data["revision"] += 1
                    data["updated_at"] = now
                    rec = FeedbackRecord(**data)
                    cur = c.execute(
                        "UPDATE feedback SET payload_json=? WHERE node_id=? AND client_feedback_id=? AND json_extract(payload_json,'$.revision')=?",
                        (rec.model_dump_json(), node_id, client_id, payload.revision),
                    )
                    if cur.rowcount != 1:
                        raise FeedbackConflict("revision conflict")
                    c.commit()
                    return rec
            old = self._get_unlocked(node_id, client_id)
            if old is None:
                return None
            if old.revision != payload.revision:
                raise FeedbackConflict("revision conflict")
            data = old.model_dump()
            if payload.status is not None:
                data["status"] = payload.status
            if payload.reply is not None:
                data["reply"] = payload.reply
            data["revision"] += 1
            data["updated_at"] = now
            rec = FeedbackRecord(**data)
            self._save(rec)
            return rec

    def list(self, *, node_id=None, status=None, category=None, page=1, page_size=50):
        with self._lock:
            if self._db is None:
                items = list(self._mem.values())
            else:
                with self._db.connect() as c:
                    items = [
                        FeedbackRecord.model_validate_json(r[0]) for r in c.execute("SELECT payload_json FROM feedback")
                    ]
            items = [
                x
                for x in items
                if (node_id is None or x.node_id == node_id)
                and (status is None or x.status == status)
                and (category is None or x.category == category)
            ]
            items.sort(key=lambda x: (x.updated_at, x.client_feedback_id), reverse=True)
            total = len(items)
            start = (page - 1) * page_size
            return items[start : start + page_size], total
