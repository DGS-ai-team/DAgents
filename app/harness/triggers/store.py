"""触发器 JSON 存储：内存索引 + 原子写盘。"""

from __future__ import annotations

import json
import time
from pathlib import Path
from threading import RLock
from typing import Any

from app.harness.triggers.models import TriggerDefinition, TriggerFireRecord, TriggerUpdateIn, infer_schedule_kind


class JsonTriggerStore:
    """触发器与触发历史的本地 JSON 存储。

    职责：
    - 维护 trigger_id → TriggerDefinition 与全局 fire 历史列表；
    - 提供 CRUD、到期查询（`due_triggers`）、fire 后状态更新。

    与外部交互：
    - 默认路径 `<运行根>/.runtime/triggers/triggers.json`（见 `triggers_store_path`）；
    - 写盘采用 tmp 替换，降低半截文件风险。

    线程安全：所有公开方法在 `RLock` 内执行。
    """

    def __init__(self, path: Path, *, history_limit: int = 200) -> None:
        """加载或初始化存储文件。

        Args:
            path: JSON 文件路径；父目录不存在时首次保存会创建。
            history_limit: 内存中保留的 history 条数上限（超出截断最旧）。
        """
        self._path = path
        self._history_limit = max(1, history_limit)
        self._lock = RLock()
        self._triggers: dict[str, TriggerDefinition] = {}
        self._history: list[TriggerFireRecord] = []
        self._load()

    def list_triggers(self) -> list[TriggerDefinition]:
        """返回全部触发器，按 created_at、trigger_id 排序。"""
        with self._lock:
            return sorted(self._triggers.values(), key=lambda item: (item.created_at, item.trigger_id))

    def get_trigger(self, trigger_id: str) -> TriggerDefinition | None:
        """按 ID 查询；不存在返回 None。"""
        with self._lock:
            return self._triggers.get(trigger_id)

    def create_trigger(self, trigger: TriggerDefinition) -> TriggerDefinition:
        """新增触发器并落盘。

        关键分支：trigger_id 已存在时抛 ValueError。
        """
        with self._lock:
            if trigger.trigger_id in self._triggers:
                raise ValueError(f"trigger already exists: {trigger.trigger_id}")
            self._triggers[trigger.trigger_id] = trigger
            self._save_locked()
            return trigger

    def update_trigger(self, trigger_id: str, patch: TriggerUpdateIn) -> TriggerDefinition:
        """合并 PATCH 字段、重算 next_fire_at 并落盘。

        关键分支：不存在时抛 KeyError；校验失败由 Pydantic 抛 ValueError。
        """
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
        """删除触发器；存在则落盘。返回是否曾存在。"""
        with self._lock:
            existed = self._triggers.pop(trigger_id, None) is not None
            if existed:
                self._save_locked()
            return existed

    def due_triggers(self, now: float | None = None) -> list[TriggerDefinition]:
        """返回当前时刻应被调度器 fire 的触发器列表。

        判定：enabled 且 next_fire_at 非空且 next_fire_at <= now。
        """
        current = time.time() if now is None else now
        with self._lock:
            return [
                trigger
                for trigger in self._triggers.values()
                if trigger.enabled and trigger.next_fire_at is not None and trigger.next_fire_at <= current
            ]

    def mark_fired(self, trigger_id: str, fired_at: float | None = None) -> TriggerDefinition:
        """fire 成功后更新计数、last_fired_at 与 next_fire_at。

        逻辑：
        1. fire_count += 1，刷新 last_fired_at / updated_at；
        2. once 类型 fire 后自动 disabled；
        3. 调用 with_next_fire 推算下次 interval 触发时间。
        """
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
                    "enabled": False if infer_schedule_kind(trigger.condition) == "once" else trigger.enabled,
                }
            ).with_next_fire(current)
            self._triggers[trigger_id] = updated
            self._save_locked()
            return updated

    def add_history(self, record: TriggerFireRecord) -> TriggerFireRecord:
        """追加一条触发历史；超出 history_limit 时保留最新段。"""
        with self._lock:
            self._history.append(record)
            if len(self._history) > self._history_limit:
                self._history = self._history[-self._history_limit :]
            self._save_locked()
            return record

    def list_history(self, trigger_id: str | None = None) -> list[TriggerFireRecord]:
        """按 fired_at 倒序返回历史；可选按 trigger_id 过滤。"""
        with self._lock:
            records = self._history
            if trigger_id is not None:
                records = [record for record in records if record.trigger_id == trigger_id]
            return sorted(records, key=lambda item: item.fired_at, reverse=True)

    def _load(self) -> None:
        """启动时从磁盘加载；文件不存在则保持空表。"""
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
        """在已持锁前提下原子写盘（调用方须已进入 self._lock）。"""
        payload: dict[str, Any] = {
            "triggers": [item.model_dump() for item in self.list_triggers()],
            "history": [item.model_dump() for item in self._history],
        }
        self._path.parent.mkdir(parents=True, exist_ok=True)
        tmp = self._path.with_suffix(f"{self._path.suffix}.tmp")
        tmp.write_text(json.dumps(payload, ensure_ascii=False, indent=2, sort_keys=True), encoding="utf-8")
        tmp.replace(self._path)
