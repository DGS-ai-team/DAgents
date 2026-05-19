"""Summary compression coordination for main-agent turns."""

from __future__ import annotations

import asyncio
import json
import logging
from typing import Any, Awaitable, Callable

from app.context.models import OpenAIConversationContext
from app.core.summary_agent.agent import init_agent as init_summary_agent
from app.harness.queue.message_queue import MessageEnvelope
from app.harness.service.interface import AgentEventEnvelope
from app.observability.metrics import record_summary_compression_apply, record_summary_compression_result

_logger = logging.getLogger(__name__)


EmitEnvelope = Callable[..., Awaitable[None]]


class SummaryCompressionCoordinator:
    def __init__(
        self,
        *,
        emit_envelope: EmitEnvelope,
        silent_trigger_tokens: int,
        blocking_trigger_tokens: int,
        summary_runtime_factory: Callable[[], Any] = init_summary_agent,
    ) -> None:
        self._emit_envelope = emit_envelope
        self._summary_runtime_factory = summary_runtime_factory
        self._summary_runtime: Any | None = None
        self._silent_trigger_tokens = max(0, int(silent_trigger_tokens))
        self._blocking_trigger_tokens = max(0, int(blocking_trigger_tokens))
        self.session_tasks: dict[str, asyncio.Task[None]] = {}
        self.pending_results: dict[str, dict[str, Any]] = {}

    async def maybe_handle(
        self,
        *,
        session_id: str,
        ctx: OpenAIConversationContext,
        env: MessageEnvelope | None = None,
        base_meta: dict[str, Any] | None = None,
    ) -> None:
        summary_runtime = self.get_runtime()
        decision = summary_runtime.should_compress(
            ctx.messages,
            silent_trigger_tokens=self._silent_trigger_tokens,
            blocking_trigger_tokens=self._blocking_trigger_tokens,
            messages_total_tokens=int(ctx.messages_total_tokens),
        )
        should_compress = bool(decision.get("should_compress"))
        trigger_level = str(decision.get("trigger_level") or "none")
        running_task = await self._reap_finished_task(session_id=session_id)
        has_running_task = running_task is not None and not running_task.done()
        if should_compress and trigger_level == "silent":
            if not has_running_task:
                self._start_task(session_id=session_id, ctx=ctx, trigger_level=trigger_level)
        elif should_compress and trigger_level == "blocking":
            if has_running_task and running_task is not None:
                await self._wait_task(
                    session_id=session_id,
                    task=running_task,
                    error_label="blocking wait silent task failed",
                )
            blocking_ok = await self.run_compression_flow(
                session_id=session_id,
                ctx=ctx,
                trigger_level=trigger_level,
            )
            if not blocking_ok:
                await self._emit_blocking_failure(
                    session_id=session_id,
                    ctx=ctx,
                    env=env,
                    base_meta=base_meta,
                    trigger_level=trigger_level,
                )
        await self.try_apply_ready_result(session_id=session_id, ctx=ctx)

    async def cancel_all_tasks(self) -> None:
        for task in list(self.session_tasks.values()):
            if task is not None and not task.done():
                task.cancel()
        for task in list(self.session_tasks.values()):
            if task is None:
                continue
            try:
                await task
            except asyncio.CancelledError:
                pass
        self.session_tasks.clear()
        self.pending_results.clear()

    async def cancel_session_task(self, *, session_id: str) -> None:
        task = self.session_tasks.get(session_id)
        if task is not None and not task.done():
            task.cancel()
        if task is not None:
            try:
                await task
            except asyncio.CancelledError:
                pass
        self.session_tasks.pop(session_id, None)
        self.pending_results.pop(session_id, None)

    def get_runtime(self) -> Any:
        if self._summary_runtime is None:
            self._summary_runtime = self._summary_runtime_factory()
        return self._summary_runtime

    async def try_apply_ready_result(self, *, session_id: str, ctx: OpenAIConversationContext) -> None:
        await self._reap_finished_task(session_id=session_id)
        pending = self.pending_results.get(session_id)
        if not isinstance(pending, dict):
            return
        self.pending_results.pop(session_id, None)
        start = int(pending.get("start", -1))
        end = int(pending.get("end", -1))
        content = str(pending.get("content", "") or "").strip()
        trigger_level = str(pending.get("trigger_level") or "unknown")
        compressed_message_count = max(0, int(pending.get("compressed_message_count", 0) or 0))
        if start < 0 or end < start or end >= len(ctx.messages) or not content:
            record_summary_compression_apply(trigger_level=trigger_level, status="invalid")
            _logger.warning(
                "%s: discard invalid compression result",
                session_id,
                extra={"session_id": session_id, "compression_start": start, "compression_end": end},
            )
            return
        current_slice = ctx.messages[start : end + 1]
        source_slice_fingerprint = str(pending.get("source_slice_fingerprint") or "")
        if source_slice_fingerprint:
            is_stale = source_slice_fingerprint != messages_fingerprint(current_slice)
        else:
            source_len = int(pending.get("source_len", -1))
            source_fingerprint = str(pending.get("source_fingerprint") or "")
            is_stale = source_len != len(ctx.messages) or source_fingerprint != messages_fingerprint(ctx.messages)
        if is_stale:
            record_summary_compression_apply(trigger_level=trigger_level, status="stale")
            _logger.warning(
                "%s: discard stale compression result",
                session_id,
                extra={
                    "session_id": session_id,
                    "compression_start": start,
                    "compression_end": end,
                    "current_message_len": len(ctx.messages),
                },
            )
            return
        replacement = {"role": "user", "content": content}
        ctx.messages = [*ctx.messages[:start], replacement, *ctx.messages[end + 1 :]]
        record_summary_compression_apply(
            trigger_level=trigger_level,
            status="applied",
            compressed_message_count=compressed_message_count or len(current_slice),
        )
        _logger.info(
            "%s: applied compressed message block",
            session_id,
            extra={
                "session_id": session_id,
                "compression_start": start,
                "compression_end": end,
                "compressed_message_count": len(current_slice),
            },
        )

    async def run_compression_flow(
        self,
        *,
        session_id: str,
        ctx: OpenAIConversationContext,
        trigger_level: str = "unknown",
        messages_snapshot: list[dict[str, Any]] | None = None,
    ) -> bool:
        summary_runtime = self.get_runtime()
        source_messages = messages_snapshot if messages_snapshot is not None else snapshot_messages(ctx.messages)
        prepared = summary_runtime.build_compression_plan(source_messages)
        if not bool(prepared.get("ok")):
            _logger.info(
                "%s: compression skipped before model call",
                session_id,
                extra={
                    "session_id": session_id,
                    "compression_reason": str(prepared.get("reason") or ""),
                    "source_message_count": int(prepared.get("source_message_count", len(source_messages))),
                },
            )
            record_summary_compression_result(trigger_level=trigger_level, ok=False)
            return False
        start = int(prepared.get("start", -1))
        end = int(prepared.get("end", -1))
        block = str(prepared.get("block") or "").strip()
        if start < 0 or end < start or end >= len(source_messages) or not block:
            record_summary_compression_result(trigger_level=trigger_level, ok=False)
            return False
        try:
            follow_content = summary_runtime.build_follow_content(source_messages, end=end)
            summary_text = await summary_runtime.run_turn(
                ctx,
                request_type="human_message",
                content=block,
                follow_content=follow_content,
            )
        except asyncio.CancelledError:
            flush = getattr(summary_runtime, "flush_cancelled_turn", None)
            if callable(flush):
                flush(ctx)
            raise
        except Exception as exc:  # noqa: BLE001
            _logger.error("%s: compression failed: %s", session_id, exc)
            record_summary_compression_result(trigger_level=trigger_level, ok=False)
            return False
        if not summary_text or not str(summary_text).strip():
            record_summary_compression_result(trigger_level=trigger_level, ok=False)
            return False
        self.pending_results[session_id] = {
            "start": start,
            "end": end,
            "content": str(summary_text).strip(),
            "source_slice_fingerprint": messages_fingerprint(source_messages[start : end + 1]),
            "compressed_message_count": int(prepared.get("compressed_message_count", 0)),
            "trigger_level": trigger_level,
        }
        record_summary_compression_result(trigger_level=trigger_level, ok=True)
        return True

    async def _reap_finished_task(self, *, session_id: str) -> asyncio.Task[None] | None:
        task = self.session_tasks.get(session_id)
        if task is None or not task.done():
            return task
        self.session_tasks.pop(session_id, None)
        await self._wait_task(session_id=session_id, task=task, error_label="silent compression failed")
        return None

    async def _wait_task(self, *, session_id: str, task: asyncio.Task[None], error_label: str) -> None:
        try:
            await task
        except asyncio.CancelledError:
            pass
        except Exception as exc:  # noqa: BLE001
            _logger.error("%s: %s: %s", session_id, error_label, exc)

    def _start_task(self, *, session_id: str, ctx: OpenAIConversationContext, trigger_level: str) -> None:
        messages_snapshot = snapshot_messages(ctx.messages)
        self.session_tasks[session_id] = asyncio.create_task(
            self.run_compression_flow(
                session_id=session_id,
                ctx=ctx,
                trigger_level=trigger_level,
                messages_snapshot=messages_snapshot,
            )
        )

    async def _emit_blocking_failure(
        self,
        *,
        session_id: str,
        ctx: OpenAIConversationContext,
        env: MessageEnvelope | None,
        base_meta: dict[str, Any] | None,
        trigger_level: str,
    ) -> None:
        _logger.warning(
            "%s: blocking compression failed; continue with original context",
            session_id,
            extra={
                "session_id": session_id,
                "compression_trigger": trigger_level,
                "messages_total_tokens": int(ctx.messages_total_tokens),
            },
        )
        if env is None:
            return
        await self._emit_envelope(
            env=env,
            envelope=AgentEventEnvelope(
                event_type="error",
                payload={
                    "message": "上下文阻塞压缩失败，已继续使用原始上下文。",
                    "recoverable": True,
                    "stage": "summary_compression",
                },
                meta={},
            ),
            base_meta=dict(base_meta or {}),
        )


def snapshot_messages(messages: list[dict[str, Any]]) -> list[dict[str, Any]]:
    try:
        cloned = json.loads(json.dumps(messages, ensure_ascii=False, default=str))
    except Exception:  # noqa: BLE001
        cloned = [dict(item) for item in messages if isinstance(item, dict)]
    return [dict(item) for item in cloned if isinstance(item, dict)]


def messages_fingerprint(messages: list[dict[str, Any]]) -> str:
    try:
        return json.dumps(messages, ensure_ascii=False, sort_keys=True, default=str)
    except Exception:  # noqa: BLE001
        return repr(messages)
