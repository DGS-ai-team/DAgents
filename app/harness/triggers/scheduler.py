"""触发器调度器：将到期触发器投递到 AgentService。"""

from __future__ import annotations

import asyncio
import json
import logging
import time
from typing import Any

from app.harness.service.agent_service import AgentService
from app.harness.triggers.models import TriggerDefinition, TriggerFireRecord
from app.harness.triggers.store import JsonTriggerStore

_logger = logging.getLogger(__name__)


class TriggerScheduler:
    def __init__(
        self,
        *,
        store: JsonTriggerStore,
        service: AgentService,
        poll_seconds: int = 5,
    ) -> None:
        self._store = store
        self._service = service
        self._poll_seconds = max(1, int(poll_seconds))
        self._stop_event = asyncio.Event()
        self._task: asyncio.Task[None] | None = None

    def start(self) -> None:
        if self._task is not None and not self._task.done():
            return
        self._stop_event.clear()
        self._task = asyncio.create_task(self._run_loop())

    async def stop(self) -> None:
        self._stop_event.set()
        if self._task is None:
            return
        self._task.cancel()
        try:
            await self._task
        except asyncio.CancelledError:
            pass

    async def fire_trigger(
        self,
        trigger_id: str,
        *,
        reason: str = "manual",
        payload: dict[str, Any] | None = None,
        force: bool = False,
    ) -> TriggerFireRecord:
        trigger = self._store.get_trigger(trigger_id)
        if trigger is None:
            raise KeyError(trigger_id)
        return await self._fire(trigger, reason=reason, payload=payload or {}, force=force)

    async def _run_loop(self) -> None:
        while not self._stop_event.is_set():
            now = time.time()
            for trigger in self._store.due_triggers(now):
                await self._fire(trigger, reason="schedule", payload={}, force=False)
            try:
                await asyncio.wait_for(self._stop_event.wait(), timeout=self._poll_seconds)
            except TimeoutError:
                continue

    async def _fire(
        self,
        trigger: TriggerDefinition,
        *,
        reason: str,
        payload: dict[str, Any],
        force: bool,
    ) -> TriggerFireRecord:
        if not trigger.enabled and not force:
            return self._record(
                trigger=trigger,
                status="skipped",
                reason=reason,
                payload=payload,
                message="trigger is disabled",
            )
        if reason != "manual" and not _allows_autonomous_fire(trigger):
            self._store.mark_fired(trigger.trigger_id)
            return self._record(
                trigger=trigger,
                status="skipped",
                reason=reason,
                payload=payload,
                message="risk level requires approval_policy.auto_fire_allowed=true",
            )
        try:
            session_id = trigger.target_session_id or await self._service.create_session(None)
            client_id = trigger.client_id or f"trigger-{trigger.trigger_id}"
            content = _render_task_template(trigger.task_template, trigger=trigger, payload=payload, reason=reason)
            await self._service.submit_message(
                session_id=session_id,
                client_id=client_id,
                content=content,
                source=f"trigger:{trigger.trigger_id}",
                priority="other",
            )
            self._store.mark_fired(trigger.trigger_id)
            return self._record(
                trigger=trigger,
                status="queued",
                reason=reason,
                payload=payload,
                session_id=session_id,
                client_id=client_id,
                content=content,
                message="queued",
            )
        except Exception as exc:  # noqa: BLE001
            _logger.warning("trigger fire failed: trigger_id=%s error=%s", trigger.trigger_id, exc)
            return self._record(
                trigger=trigger,
                status="error",
                reason=reason,
                payload=payload,
                message=str(exc),
            )

    def _record(
        self,
        *,
        trigger: TriggerDefinition,
        status: str,
        reason: str,
        payload: dict[str, Any],
        message: str,
        session_id: str | None = None,
        client_id: str | None = None,
        content: str = "",
    ) -> TriggerFireRecord:
        record = TriggerFireRecord(
            trigger_id=trigger.trigger_id,
            status=status,  # type: ignore[arg-type]
            reason=reason,
            session_id=session_id,
            client_id=client_id,
            content=content,
            message=message,
            payload=payload,
        )
        return self._store.add_history(record)


def _allows_autonomous_fire(trigger: TriggerDefinition) -> bool:
    if trigger.risk_level in {"low", "medium"}:
        return True
    return bool(trigger.approval_policy.get("auto_fire_allowed"))


def _render_task_template(
    template: str,
    *,
    trigger: TriggerDefinition,
    payload: dict[str, Any],
    reason: str,
) -> str:
    values = {
        "trigger_id": trigger.trigger_id,
        "trigger_name": trigger.name,
        "source_type": trigger.source_type,
        "reason": reason,
        "payload_json": json.dumps(payload, ensure_ascii=False, sort_keys=True),
    }
    return template.format_map(_SafeFormatMap(values))


class _SafeFormatMap(dict[str, str]):
    def __missing__(self, key: str) -> str:
        return "{" + key + "}"
