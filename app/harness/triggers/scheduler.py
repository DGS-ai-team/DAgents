"""触发器调度器：轮询到期触发器并投递到 AgentService。"""

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
    """后台调度循环 + 统一 fire 入口（手动 API / @tool 与自动 tick 共用）。

    职责：
    1. 按 `poll_seconds` 轮询 `store.due_triggers`；
    2. 将 `task_template` 渲染为 user 消息，经 `AgentService.submit_message` 入队；
    3. 每次 fire 写入 history（queued / skipped / error）。

    非职责：不执行 Agent 推理本身。
    """

    def __init__(
        self,
        *,
        store: JsonTriggerStore,
        service: AgentService,
        poll_seconds: int = 5,
    ) -> None:
        """绑定存储与 Agent 服务，并设置轮询间隔（至少 1 秒）。"""
        self._store = store
        self._service = service
        self._poll_seconds = max(1, int(poll_seconds))
        self._stop_event = asyncio.Event()
        self._task: asyncio.Task[None] | None = None

    def start(self) -> None:
        """启动后台 `_run_loop`；已在运行则幂等忽略。"""
        if self._task is not None and not self._task.done():
            return
        self._stop_event.clear()
        self._task = asyncio.create_task(self._run_loop())

    async def stop(self) -> None:
        """停止轮询任务并等待取消完成。"""
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
        """手动或 API 触发指定触发器。

        Args:
            trigger_id: 资源 ID。
            reason: 记录用原因（manual / schedule 等）。
            payload: 传入模板 `{payload_json}` 等占位符。
            force: 为 true 时忽略 enabled=false。

        关键分支：不存在时抛 KeyError。
        """
        trigger = self._store.get_trigger(trigger_id)
        if trigger is None:
            raise KeyError(trigger_id)
        return await self._fire(trigger, reason=reason, payload=payload or {}, force=force)

    async def _run_loop(self) -> None:
        """轮询直到 stop：处理到期触发器，再 sleep poll_seconds 或提前被 stop 唤醒。"""
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
        """单次 fire 核心：门禁检查 → 会话/客户端 → 渲染模板 → submit_message → 记历史。

        逻辑：
        1. disabled 且非 force → skipped；
        2. 成功则 queued 并 mark_fired；异常记 error，不 mark_fired。

        与外部交互：`AgentService.create_session` / `submit_message`。
        """
        if not trigger.enabled and not force:
            return self._record(
                trigger=trigger,
                status="skipped",
                reason=reason,
                payload=payload,
                message="trigger is disabled",
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
        """构造 TriggerFireRecord 并写入 store history。"""
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


def _render_task_template(
    template: str,
    *,
    trigger: TriggerDefinition,
    payload: dict[str, Any],
    reason: str,
) -> str:
    """将 task_template 中的 `{trigger_id}` 等占位符替换为运行时值。

    未知占位符原样保留（`{unknown}`），避免 format 抛 KeyError。
    """
    values = {
        "trigger_id": trigger.trigger_id,
        "trigger_name": trigger.name,
        "reason": reason,
        "payload_json": json.dumps(payload, ensure_ascii=False, sort_keys=True),
    }
    return template.format_map(_SafeFormatMap(values))


class _SafeFormatMap(dict[str, str]):
    """str.format_map 用的安全 dict：缺失键返回 `{key}` 字面量。"""

    def __missing__(self, key: str) -> str:
        return "{" + key + "}"
