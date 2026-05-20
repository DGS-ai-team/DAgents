"""触发器 JSON 存储。"""

from __future__ import annotations

import json
import time
from pathlib import Path
from threading import RLock
from typing import Any

from app.harness.triggers.models import TriggerDefinition, TriggerFireRecord, TriggerUpdateIn


class JsonTriggerStore:
    def __init__(self, path: Path, *, history_limit: int = 200) -> None:
        self._path = path
        self._history_limit = max(1, history_limit)
        self._lock = RLock()
        self._triggers: dict[str, TriggerDefinition] = {}
        self._history: list[TriggerFireRecord] = []
        self._load()

    def list_triggers(self) -> list[TriggerDefinition]:
        with self._lock:
            return sorted(self._triggers.values(), key=lambda item: (item.created_at, item.trigger_id))

    def get_trigger(self, trigger_id: str) -> TriggerDefinition | None:
        with self._lock:
            return self._triggers.get(trigger_id)

    def create_trigger(self, trigger: TriggerDefinition) -> TriggerDefinition:
        with self._lock:
            if trigger.trigger_id in self._triggers:
                raise ValueError(f"trigger already exists: {trigger.trigger_id}")
            self._triggers[trigger.trigger_id] = trigger
            self._save_locked()
            return trigger

    def update_trigger(self, trigger_id: str, patch: TriggerUpdateIn) -> TriggerDefinition:
        with self._lock:
            current = self._triggers.get(trigger_id)
            if current is None:
                raise KeyError(trigger_id)
            changes = patch.model_dump(exclude_unset=True)
            changes["updated_at"] = time.time()
            data = current.model_dump()
            data.update(changes)
            updated = TriggerDefinition.model_validate(data).with_next_fire(changes["updated_at"])
            self._triggers[trigger_id] = updated
            self._save_locked()
            return updated

    def delete_trigger(self, trigger_id: str) -> bool:
        with self._lock:
            existed = self._triggers.pop(trigger_id, None) is not None
            if existed:
                self._save_locked()
            return existed

    def due_triggers(self, now: float | None = None) -> list[TriggerDefinition]:
        current = time.time() if now is None else now
        with self._lock:
            return [
                trigger
                for trigger in self._triggers.values()
                if trigger.enabled and trigger.next_fire_at is not None and trigger.next_fire_at <= current
            ]

    def mark_fired(self, trigger_id: str, fired_at: float | None = None) -> TriggerDefinition:
        current = time.time() if fired_at is None else fired_at
        with self._lock:
            trigger = self._triggers.get(trigger_id)
            if trigger is None:
                raise KeyError(trigger_id)
            updated = trigger.model_copy(
                update={
                    "fire_count": trigger.fire_count + 1,
                    "last_fired_at": current,
                    "updated_at": current,
                    "enabled": False if trigger.source_type == "once" else trigger.enabled,
                }
            ).with_next_fire(current)
            self._triggers[trigger_id] = updated
            self._save_locked()
            return updated

    def add_history(self, record: TriggerFireRecord) -> TriggerFireRecord:
        with self._lock:
            self._history.append(record)
            if len(self._history) > self._history_limit:
                self._history = self._history[-self._history_limit :]
            self._save_locked()
            return record

    def list_history(self, trigger_id: str | None = None) -> list[TriggerFireRecord]:
        with self._lock:
            records = self._history
            if trigger_id is not None:
                records = [record for record in records if record.trigger_id == trigger_id]
            return sorted(records, key=lambda item: item.fired_at, reverse=True)

    def _load(self) -> None:
        with self._lock:
            if not self._path.is_file():
                return
            data = json.loads(self._path.read_text(encoding="utf-8"))
            triggers = data.get("triggers", []) if isinstance(data, dict) else []
            history = data.get("history", []) if isinstance(data, dict) else []
            self._triggers = {
                item.trigger_id: item
                for item in (TriggerDefinition.model_validate(raw) for raw in triggers if isinstance(raw, dict))
            }
            self._history = [TriggerFireRecord.model_validate(raw) for raw in history if isinstance(raw, dict)]

    def _save_locked(self) -> None:
        payload: dict[str, Any] = {
            "triggers": [item.model_dump() for item in self.list_triggers()],
            "history": [item.model_dump() for item in self._history],
        }
        self._path.parent.mkdir(parents=True, exist_ok=True)
        tmp = self._path.with_suffix(f"{self._path.suffix}.tmp")
        tmp.write_text(json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True), encoding="utf-8")
        tmp.replace(self._path)
