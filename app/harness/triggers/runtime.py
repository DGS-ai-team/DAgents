"""触发器运行时共享入口：进程内单例 store / scheduler。"""

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
    """获取进程内共享的 JsonTriggerStore（懒创建单例）。

    逻辑：
    1. 首次调用时用 `path` 或默认 `triggers_store_path()` 构造；
    2. 后续忽略 path 参数，返回同一实例。

    与外部交互：API lifespan 与 `@tool` 触发器工具均通过此入口访问存储。
    """
    global _store
    with _lock:
        if _store is None:
            _store = JsonTriggerStore(path or triggers_store_path())
        return _store


def set_trigger_runtime(*, store: JsonTriggerStore | None, scheduler: "TriggerScheduler | None") -> None:
    """由 FastAPI lifespan 注入 store / scheduler（测试可用 `reset_trigger_runtime` 后重注）。

    副作用：更新模块级 `_store`、`_scheduler` 引用。
    """
    global _store, _scheduler
    with _lock:
        if store is not None:
            _store = store
        _scheduler = scheduler


def get_trigger_scheduler() -> "TriggerScheduler | None":
    """返回当前调度器；`TRIGGERS_ENABLED=false` 或未启动 API 时为 None。"""
    with _lock:
        return _scheduler


def reset_trigger_runtime() -> None:
    """清空单例（单测 teardown 用）；不关闭已在运行的 asyncio 任务。"""
    global _store, _scheduler
    with _lock:
        _store = None
        _scheduler = None
