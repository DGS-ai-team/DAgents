"""触发器运行时共享入口。"""

from __future__ import annotations

from pathlib import Path
from threading import RLock
from typing import TYPE_CHECKING

from app.config.runtime_layout import triggers_store_path
from app.harness.triggers.store import JsonTriggerStore

if TYPE_CHECKING:
    from app.harness.triggers.scheduler import TriggerScheduler

_lock = RLock()
_store: JsonTriggerStore | None = None
_scheduler: "TriggerScheduler | None" = None


def get_trigger_store(path: Path | None = None) -> JsonTriggerStore:
    global _store
    with _lock:
        if _store is None:
            _store = JsonTriggerStore(path or triggers_store_path())
        return _store


def set_trigger_runtime(*, store: JsonTriggerStore | None, scheduler: "TriggerScheduler | None") -> None:
    global _store, _scheduler
    with _lock:
        if store is not None:
            _store = store
        _scheduler = scheduler


def get_trigger_scheduler() -> "TriggerScheduler | None":
    with _lock:
        return _scheduler


def reset_trigger_runtime() -> None:
    global _store, _scheduler
    with _lock:
        _store = None
        _scheduler = None
