"""案例库存储（内存 + SQLite）。"""

from __future__ import annotations

import threading
import uuid

from manage.cases.jsonl import parse_jsonl_bytes
from manage.cases.models import (
    CaseCreate,
    CaseExample,
    CaseMessage,
    CaseMetadataPatch,
    CaseResources,
)
from manage.storage.sqlite import SQLiteDatabase


class CaseExampleStore:
    def __init__(self, db: SQLiteDatabase | None = None) -> None:
        self._db = db if (db and db.enabled) else None
        self._lock = threading.RLock()
        self._mem: dict[str, CaseExample] = {}

    def _save(self, case: CaseExample) -> None:
        if self._db is None:
            self._mem[case.case_id] = case
            return
        with self._db.connect() as conn:
            conn.execute(
                "INSERT INTO case_examples(case_id,payload_json) VALUES(?,?) "
                "ON CONFLICT(case_id) DO UPDATE SET payload_json=excluded.payload_json",
                (case.case_id, case.model_dump_json()),
            )
            conn.commit()

    def _all(self) -> list[CaseExample]:
        if self._db is None:
            return list(self._mem.values())
        with self._db.connect() as conn:
            return [
                CaseExample.model_validate_json(r["payload_json"])
                for r in conn.execute("SELECT payload_json FROM case_examples")
            ]

    def list(self) -> list[CaseExample]:
        return sorted(self._all(), key=lambda c: (c.updated_at, c.case_id), reverse=True)

    def get(self, case_id: str) -> CaseExample | None:
        return next((c for c in self._all() if c.case_id == case_id), None)

    def create(self, payload: CaseCreate, *, messages: list[CaseMessage], now: int) -> CaseExample:
        with self._lock:
            if self.get(payload.case_id):
                raise KeyError("case_id already exists")
            case = CaseExample(
                **payload.model_dump(),
                messages=messages,
                created_at=now,
                updated_at=now,
            )
            self._save(case)
            return case

    def delete(self, case_id: str) -> CaseExample | None:
        with self._lock:
            case = self.get(case_id)
            if not case:
                return None
            if self._db is None:
                del self._mem[case_id]
            else:
                with self._db.connect() as conn:
                    conn.execute("DELETE FROM case_examples WHERE case_id=?", (case_id,))
                    conn.commit()
            return case

    def patch_metadata(self, case_id: str, patch: CaseMetadataPatch, now: int) -> CaseExample | None:
        with self._lock:
            case = self.get(case_id)
            if not case:
                return None
            data = case.model_dump()
            if patch.name is not None:
                data["name"] = patch.name
            if patch.description is not None:
                data["description"] = patch.description
            if patch.resources is not None:
                data["resources"] = patch.resources.model_dump()
            data["updated_at"] = now
            updated = CaseExample.model_validate(data)
            self._save(updated)
            return updated

    def replace_messages(self, case_id: str, messages: list[CaseMessage], now: int) -> CaseExample | None:
        with self._lock:
            case = self.get(case_id)
            if not case:
                return None
            updated = case.model_copy(update={"messages": messages, "updated_at": now})
            self._save(updated)
            return updated

    def import_jsonl(self, case_id: str, data: bytes, *, replace: bool, now: int) -> CaseExample | None:
        parsed = parse_jsonl_bytes(data)
        with self._lock:
            case = self.get(case_id)
            if not case:
                return None
            if replace:
                messages = parsed
            else:
                messages = list(case.messages) + parsed
            updated = case.model_copy(update={"messages": messages, "updated_at": now})
            self._save(updated)
            return updated

    def insert_message(
        self,
        case_id: str,
        message: CaseMessage,
        *,
        index: int | None,
        now: int,
    ) -> CaseExample | None:
        with self._lock:
            case = self.get(case_id)
            if not case:
                return None
            messages = list(case.messages)
            if not message.id.strip():
                message = message.model_copy(update={"id": str(uuid.uuid4())})
            if index is None or index >= len(messages):
                messages.append(message)
            else:
                idx = max(0, index)
                messages.insert(idx, message)
            updated = case.model_copy(update={"messages": messages, "updated_at": now})
            self._save(updated)
            return updated

    def update_message(
        self,
        case_id: str,
        message_id: str,
        patch: CaseMessage,
        now: int,
    ) -> CaseExample | None:
        with self._lock:
            case = self.get(case_id)
            if not case:
                return None
            messages: list[CaseMessage] = []
            found = False
            for msg in case.messages:
                if msg.id != message_id:
                    messages.append(msg)
                    continue
                found = True
                messages.append(
                    CaseMessage(
                        id=message_id,
                        recorded_at=patch.recorded_at,
                        role=patch.role,
                        content=patch.content,
                        raw=patch.raw,
                    )
                )
            if not found:
                return None
            updated = case.model_copy(update={"messages": messages, "updated_at": now})
            self._save(updated)
            return updated

    def delete_message(self, case_id: str, message_id: str, now: int) -> CaseExample | None:
        with self._lock:
            case = self.get(case_id)
            if not case:
                return None
            messages = [m for m in case.messages if m.id != message_id]
            if len(messages) == len(case.messages):
                return None
            updated = case.model_copy(update={"messages": messages, "updated_at": now})
            self._save(updated)
            return updated

    @staticmethod
    def empty_resources() -> CaseResources:
        return CaseResources()
