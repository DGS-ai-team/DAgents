"""主 Agent 入口：OpenAI 隐式 ReAct runtime 工厂与回合业务编排器。"""

from __future__ import annotations

import asyncio
import json
from dataclasses import dataclass
from typing import Any, Awaitable, Callable, Literal

from app.config.settings import get_settings
from app.context.models import OpenAIConversationContext, PendingToolCall, RunTurnPhase
from app.harness.history.raw_message_journal import append_openai_message_with_journal, insert_openai_message_with_journal
from app.harness.queue.message_queue import MessageEnvelope
from app.harness.service.interface import AgentEventEnvelope
from app.harness.tools.result_policy import package_tool_result
from app.harness.tools.tool import should_require_tool_approval
from app.observability.metrics import (
    record_summary_compression_result,
    record_tool_approval_required,
    record_tool_execution_result,
)
from app.schemas.approval import (
    ApprovalRequiredEnvelopePayload,
    ApprovalToolCallsArgs,
    ResumeToolApprove,
    ResumeToolReject,
    ResumeToolSelection,
    ToolCallApprovalItem,
    parse_resume_tool_decision,
)

from app.core.main_agent.display_inference import (
    infer_tool_call_display_type,
    infer_tool_result_display_type,
)
from app.core.main_agent.runtime_openai import OpenAIImplicitReActRuntime
from app.core.summary_agent.agent import init_agent as init_summary_agent

import logging

_logger = logging.getLogger(__name__)


@dataclass(frozen=True, slots=True)
class ToolExecutionPlan:
    """单轮模型 `tool_calls` 的执行计划。

    逻辑：
    1. `auto_exec_calls` 保存可直接执行的工具；
    2. `need_approval_calls` 保存必须等待 resume 的工具；
    3. 通过 `has_pending_approval` 让主编排分支显式表达“执行 vs 等待审批”。

    关键边界：
    - 本类只承载拆分结果，不执行工具、不修改上下文；
    - 后续若加入 rejected/blocked/skipped 等状态，应优先扩展本类，避免分支继续散落。
    """

    auto_exec_calls: list[PendingToolCall]
    need_approval_calls: list[PendingToolCall]

    @property
    def has_pending_approval(self) -> bool:
        """判断本轮是否存在待人工审批工具。"""
        return bool(self.need_approval_calls)


class MainAgentTurnOrchestrator:
    """消息回合业务编排器（service 无状态基础设施之上的业务层）。"""

    _TOOL_USER_INTERRUPTED_MESSAGE = "用户需要补充信息，打断了工具执行。"
    _RUNTIME_TOOL_MESSAGE_CONTENT = "tool_message"

    def __init__(
        self,
        *,
        submit_message: Callable[..., Awaitable[None]],
        emit_envelope: Callable[..., Awaitable[None]],
        tool_map: dict[str, Any],
    ) -> None:
        """注入编排依赖。

        逻辑：
        1. 保存 `submit_message`（用于回灌 tool_result 入队）；
        2. 保存 `emit_envelope`（用于统一输出事件）；
        3. 保存 `tool_map`（用于工具执行）；
        4. 初始化 summary 压缩阈值与任务映射（按 session 管理静默压缩 task）。
        """
        settings = get_settings()
        self._submit_message = submit_message
        self._emit_envelope = emit_envelope
        self._tool_map = tool_map
        self._summary_runtime: Any | None = None
        self._summary_silent_trigger_tokens = max(0, int(settings.summary_compression_silent_trigger_tokens))
        self._summary_blocking_trigger_tokens = max(0, int(settings.summary_compression_blocking_trigger_tokens))
        self._session_summary_tasks: dict[str, asyncio.Task[None]] = {}
        self._session_pending_compression_results: dict[str, dict[str, Any]] = {}

    async def maybe_handle_summary_compression(
        self,
        *,
        session_id: str,
        ctx: OpenAIConversationContext,
        env: MessageEnvelope | None = None,
        base_meta: dict[str, Any] | None = None,
    ) -> None:
        """在每轮消息处理入口执行 summary 压缩编排。

        逻辑：
        1. 先按阈值判断本轮压缩级别（`silent` / `blocking` / `none`）；
        2. 命中 `silent`：仅当无在途静默任务时启动并记录后台压缩；
        3. 命中 `blocking`：若有在途压缩先阻塞等待；否则（或等待后仍未产出结果）执行阻塞压缩；
        4. 阻塞压缩失败时发出可恢复错误事件（若调用方提供当前消息信封）；
        5. 最后统一尝试应用已完成压缩结果（若有），应用前校验消息指纹。

        关键边界：
        - 静默压缩只记录日志，不主动打断当前 turn；
        - 阻塞压缩失败不会丢弃原始上下文，后续推理继续使用未压缩消息。
        """
        summary_runtime = self._get_summary_runtime()
        decision = summary_runtime.should_compress(
            ctx.messages,
            silent_trigger_tokens=self._summary_silent_trigger_tokens,
            blocking_trigger_tokens=self._summary_blocking_trigger_tokens,
            messages_total_tokens=int(ctx.messages_total_tokens),
        )
        should_compress = bool(decision.get("should_compress"))
        trigger_level = str(decision.get("trigger_level") or "none")
        running_task = self._session_summary_tasks.get(session_id)
        if running_task is not None and running_task.done():
            self._session_summary_tasks.pop(session_id, None)
            try:
                await running_task
            except asyncio.CancelledError:
                pass
            except Exception as exc:  # noqa: BLE001
                _logger.error("%s: silent compression failed: %s", session_id, exc)
            running_task = None
        if running_task is not None and not running_task.done():
            has_running_task = True
        else:
            has_running_task = False
        if should_compress and trigger_level == "silent":
            if not has_running_task:
                self._session_summary_tasks[session_id] = asyncio.create_task(
                    self._run_compression_flow(session_id=session_id, ctx=ctx, trigger_level=trigger_level)
                )
        elif should_compress and trigger_level == "blocking":
            if has_running_task:
                try:
                    await running_task
                except asyncio.CancelledError:
                    pass
                except Exception as exc:  # noqa: BLE001
                    _logger.error("%s: blocking wait silent task failed: %s", session_id, exc)
            blocking_ok = await self._run_compression_flow(
                session_id=session_id,
                ctx=ctx,
                trigger_level=trigger_level,
            )
            if not blocking_ok:
                _logger.warning(
                    "%s: blocking compression failed; continue with original context",
                    session_id,
                    extra={
                        "session_id": session_id,
                        "compression_trigger": trigger_level,
                        "messages_total_tokens": int(ctx.messages_total_tokens),
                    },
                )
                if env is not None:
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
        else:
            pass
        await self._try_apply_ready_compression_result(session_id=session_id, ctx=ctx)

    async def cancel_all_summary_tasks(self) -> None:
        """取消并回收所有 session 的静默压缩任务。"""
        for task in list(self._session_summary_tasks.values()):
            if task is not None and not task.done():
                task.cancel()
        for task in list(self._session_summary_tasks.values()):
            if task is None:
                continue
            try:
                await task
            except asyncio.CancelledError:
                pass
        self._session_summary_tasks.clear()
        self._session_pending_compression_results.clear()

    async def cancel_session_summary_task(self, *, session_id: str) -> None:
        """取消并回收指定 session 的静默压缩任务。"""
        task = self._session_summary_tasks.get(session_id)
        if task is not None and not task.done():
            task.cancel()
        if task is not None:
            try:
                await task
            except asyncio.CancelledError:
                pass
        self._session_summary_tasks.pop(session_id, None)
        self._session_pending_compression_results.pop(session_id, None)

    def _get_summary_runtime(self) -> Any:
        """懒加载并复用 summary 压缩 runtime。"""
        if self._summary_runtime is None:
            self._summary_runtime = init_summary_agent()
        return self._summary_runtime

    async def _try_apply_ready_compression_result(
        self,
        *,
        session_id: str,
        ctx: OpenAIConversationContext,
    ) -> None:
        """在每轮入口尝试应用已完成的压缩结果。

        逻辑：
        1. 若该 session 存在已结束静默压缩 task，先回收 task 并记录失败日志；
        2. 若存在待应用压缩结果，按 `start/end` 替换消息切片；
        3. 替换前校验 source_len/source_fingerprint，确认压缩基于当前消息版本；
        4. 替换失败（区间越界/内容空/版本不一致）时直接丢弃待应用结果，不改写消息。
        """
        task = self._session_summary_tasks.get(session_id)
        if task is not None and task.done():
            self._session_summary_tasks.pop(session_id, None)
            try:
                await task
            except asyncio.CancelledError:
                pass
            except Exception as exc:  # noqa: BLE001
                _logger.error("%s: silent compression failed: %s", session_id, exc)
        pending = self._session_pending_compression_results.get(session_id)
        if not isinstance(pending, dict):
            return
        self._session_pending_compression_results.pop(session_id, None)
        start = int(pending.get("start", -1))
        end = int(pending.get("end", -1))
        content = str(pending.get("content", "") or "").strip()
        if start < 0 or end < start or end >= len(ctx.messages) or not content:
            _logger.warning(
                "%s: discard invalid compression result",
                session_id,
                extra={"session_id": session_id, "compression_start": start, "compression_end": end},
            )
            return
        source_len = int(pending.get("source_len", -1))
        source_fingerprint = str(pending.get("source_fingerprint") or "")
        if source_len != len(ctx.messages) or source_fingerprint != self._messages_fingerprint(ctx.messages):
            # 静默压缩可能晚于新消息完成；版本不一致时宁可丢弃，也不能覆盖新上下文。
            _logger.warning(
                "%s: discard stale compression result",
                session_id,
                extra={
                    "session_id": session_id,
                    "compression_source_len": source_len,
                    "current_message_len": len(ctx.messages),
                },
            )
            return
        replacement = {"role": "user", "content": content}
        ctx.messages = [*ctx.messages[:start], replacement, *ctx.messages[end + 1 :]]
        _logger.info(
            "%s: applied compressed message block",
            session_id,
            extra={
                "session_id": session_id,
                "compression_start": start,
                "compression_end": end,
                "compression_source_len": source_len,
            },
        )

    async def _run_compression_flow(
        self,
        *,
        session_id: str,
        ctx: OpenAIConversationContext,
        trigger_level: str = "unknown",
    ) -> bool:
        """执行一次完整压缩流程（解析区间 -> 生成摘要 -> 暂存替换结果）。

        逻辑：
        1. 构造压缩区间和后续上下文；
        2. 调用 summary runtime 生成摘要；
        3. 暂存摘要与源消息指纹，并记录压缩成功/失败指标。
        """
        summary_runtime = self._get_summary_runtime()
        prepared = summary_runtime.build_compression_plan(ctx.messages)
        if not bool(prepared.get("ok")):
            _logger.info(
                "%s: compression skipped before model call",
                session_id,
                extra={
                    "session_id": session_id,
                    "compression_reason": str(prepared.get("reason") or ""),
                    "source_message_count": int(prepared.get("source_message_count", len(ctx.messages))),
                },
            )
            record_summary_compression_result(trigger_level=trigger_level, ok=False)
            return False
        start = int(prepared.get("start", -1))
        end = int(prepared.get("end", -1))
        block = str(prepared.get("block") or "").strip()
        if start < 0 or end < start or end >= len(ctx.messages) or not block:
            record_summary_compression_result(trigger_level=trigger_level, ok=False)
            return False
        try:
            follow_content = summary_runtime.build_follow_content(ctx.messages, end=end)
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
        self._session_pending_compression_results[session_id] = {
            "start": start,
            "end": end,
            "content": str(summary_text).strip(),
            "source_len": len(ctx.messages),
            "source_fingerprint": self._messages_fingerprint(ctx.messages),
            "compressed_message_count": int(prepared.get("compressed_message_count", 0)),
        }
        record_summary_compression_result(trigger_level=trigger_level, ok=True)
        return True

    @staticmethod
    def _messages_fingerprint(messages: list[dict[str, Any]]) -> str:
        """计算压缩源消息的稳定指纹。

        逻辑：
        1. 仅基于当前 `messages` 内容序列化；
        2. `sort_keys=True` 保证 dict 字段顺序不影响结果；
        3. 序列化失败时退化为 `repr`，仍保证同进程内可比。

        关键边界：
        - 该指纹只用于防止静默压缩覆盖新上下文，不作为安全哈希或持久化 ID。
        """
        try:
            return json.dumps(messages, ensure_ascii=False, sort_keys=True, default=str)
        except Exception:  # noqa: BLE001
            return repr(messages)

    async def handle_message(
        self,
        *,
        ctx: OpenAIConversationContext,
        runtime: Any,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
    ) -> None:
        """处理单条消息业务分支：resume/async_tool_result/tool_result/human_message。"""
        # 统一在业务分支前执行压缩决策与应用。
        _logger.info("[begin_handle_message] %s: request_type=%s", env.session_id, env.request_type)
        await self.maybe_handle_summary_compression(
            session_id=env.session_id,
            ctx=ctx,
            env=env,
            base_meta=base_meta,
        )
        if env.request_type == "resume":
            await self._handle_resume(ctx=ctx, env=env, base_meta=base_meta)
            return
        elif env.request_type == "async_tool_result":
            await self._handle_async_tool_result(ctx=ctx, runtime=runtime, env=env, base_meta=base_meta)
            return
        elif env.request_type == "tool_result":
            await self._handle_tool_result(ctx=ctx, runtime=runtime, env=env, base_meta=base_meta)
            return
        else:
            await self._handle_human_message(ctx=ctx, runtime=runtime, env=env, base_meta=base_meta)
        _logger.info("[end_handle_message] %s: request_type=%s", env.session_id, env.request_type)

    async def _handle_resume(
        self,
        *,
        ctx: OpenAIConversationContext,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
    ) -> None:
        """处理审批后的 resume：执行批准工具、处理拒绝并回灌 tool_result。"""
        _logger.info("[begin_handle_resume] %s", env.session_id)
        # 如果没有可恢复的工具调用，即pending_tool_calls为空，则直接返回错误
        if not ctx.pending_tool_calls:
            await self._emit_envelope(
                env=env,
                envelope=AgentEventEnvelope(
                    event_type="error",
                    payload={"message": "没有可恢复的 pending tool 调用。"},
                    meta={},
                ),
                base_meta=base_meta,
            )
            await self._emit_envelope(
                env=env,
                envelope=AgentEventEnvelope(
                    event_type="done", payload={"finish_reason": "resume_no_pending_tools"}, meta={}
                ),
                base_meta=base_meta,
            )
            return

        decision = parse_resume_tool_decision(env.resume_value)
        pending_snapshot = list(ctx.pending_tool_calls)
        pending_by_id = {p.call_id: p for p in pending_snapshot}
        pending_ids = set(pending_by_id.keys())
        approved_ids: set[str]
        rejected_ids: set[str]
        if isinstance(decision, ResumeToolApprove):
            approved_ids = set(pending_ids)
            rejected_ids = set()
        elif isinstance(decision, ResumeToolReject):
            approved_ids = set()
            rejected_ids = set(pending_ids)
        elif isinstance(decision, ResumeToolSelection):
            approved_ids = {str(c).strip() for c in decision.approved if str(c).strip()}
            rejected_ids = {str(c).strip() for c in decision.rejected if str(c).strip()}
            decided_ids = approved_ids | rejected_ids
            invalid_ids = decided_ids - pending_ids
            duplicate_ids = approved_ids & rejected_ids
            if invalid_ids or duplicate_ids or decided_ids != pending_ids:
                await self._emit_envelope(
                    env=env,
                    envelope=AgentEventEnvelope(
                        event_type="error",
                        payload={"message": "selection resume 必须一次性覆盖全部 pending tool 调用，且不能包含未知或重复 call_id。"},
                        meta={},
                    ),
                    base_meta=base_meta,
                )
                await self._emit_envelope(
                    env=env,
                    envelope=AgentEventEnvelope(
                        event_type="done", payload={"finish_reason": "resume_selection_invalid"}, meta={}
                    ),
                    base_meta=base_meta,
                )
                return
        else:
            approved_ids = set()
            rejected_ids = set(pending_ids)

        executed_results: list[dict[str, Any]] = []
        for item in pending_snapshot:
            call_id = item.call_id
            if call_id in approved_ids:
                result_text = await self._invoke_tool(
                    ctx,
                    item,
                    env=env,
                    base_meta=base_meta,
                )
                self._append_tool_message(
                    ctx=ctx,
                    tool_call_id=item.call_id,
                    content=result_text,
                )
                executed_results.append(
                    {
                        "tool_name": item.name,
                        "tool_call_id": item.call_id,
                        "content": result_text,
                        "display_type": infer_tool_result_display_type(item.name, result_text),
                    }
                )
            elif call_id in rejected_ids:
                result_text = f"工具 {item.name!r} 已被用户拒绝执行。"
                self._append_tool_message(
                    ctx=ctx,
                    tool_call_id=item.call_id,
                    content=result_text,
                )
                await self._emit_envelope(
                    env=env,
                    envelope=AgentEventEnvelope(
                        event_type="tool_result",
                        payload={
                            "tool_name": item.name,
                            "tool_call_id": item.call_id,
                            "content": result_text,
                            "rejected": True,
                            "partial": False,
                            "display_type": infer_tool_result_display_type(item.name, result_text),
                        },
                        meta={},
                    ),
                    base_meta=base_meta,
                )
                executed_results.append(
                    {
                        "tool_name": item.name,
                        "tool_call_id": item.call_id,
                        "content": result_text,
                        "rejected": True,
                        "display_type": infer_tool_result_display_type(item.name, result_text),
                    }
                )

        ctx.pending_tool_calls.clear()
        ctx.run_turn_phase = RunTurnPhase.IDLE
        await self._submit_message(
            session_id=env.session_id,
            client_id=env.client_id,
            content="",
            request_type="tool_result",
            tool_result={
                "results": executed_results,
            },
            source="service",
            priority="tool_result",
        )

    async def _handle_async_tool_result(
        self,
        *,
        ctx: OpenAIConversationContext,
        runtime: Any,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
    ) -> None:
        """处理异步工具完成回灌，并继续一轮 tool_message 推理。"""
        _logger.info("[begin_handle_async_tool_result] %s", env.session_id)
        payload = dict(env.async_tool_result or {})
        (
            user_message,
            assistant_message,
            tool_message,
            tool_name,
            tool_call_id,
            status,
        ) = self._build_tool_result_messages(payload)
        tail_kind = self._classify_tool_result_tail(ctx)
        if tail_kind == "tail_tool":
            append_openai_message_with_journal(ctx, assistant_message)
            append_openai_message_with_journal(ctx, tool_message)
        elif tail_kind == "tail_assistant_with_tool_calls":
            insert_at = len(ctx.messages) - 1
            insert_openai_message_with_journal(ctx, insert_at, assistant_message)
            insert_openai_message_with_journal(ctx, insert_at + 1, tool_message)
        elif tail_kind == "tail_assistant_without_tool_calls":
            append_openai_message_with_journal(ctx, user_message)
            append_openai_message_with_journal(ctx, assistant_message)
            append_openai_message_with_journal(ctx, tool_message)
        else:
            append_openai_message_with_journal(ctx, assistant_message)
            append_openai_message_with_journal(ctx, tool_message)
        async_tool_calls_payload = [
            {
                "id": tool_call_id,
                "name": "tool_callback",
                "arguments": {"job_id": str(payload.get("job_id", "") or "unknown-job")},
                "raw_arguments": assistant_message["tool_calls"][0]["function"]["arguments"],
            }
        ]
        await self._emit_envelope(
            env=env,
            envelope=AgentEventEnvelope(
                event_type="tool_call",
                payload={
                    "assistant_content": "",
                    "tool_calls": async_tool_calls_payload,
                    "display_type": infer_tool_call_display_type("", async_tool_calls_payload),
                },
                meta={},
            ),
            base_meta=base_meta,
        )
        await self._emit_envelope(
            env=env,
            envelope=AgentEventEnvelope(
                event_type="tool_result",
                payload={
                    "tool_name": tool_name,
                    "tool_call_id": tool_call_id,
                    "content": str(tool_message.get("content", "") or ""),
                    "partial": False,
                    "async_status": status,
                    "display_type": infer_tool_result_display_type(
                        tool_name,
                        str(tool_message.get("content", "") or ""),
                    ),
                },
                meta={},
            ),
            base_meta=base_meta,
        )
        # 仅在“尾部是 tool”或“尾部是 assistant(无 tool_calls)”时继续触发下一轮推理。
        if tail_kind in {"tail_tool", "tail_assistant_without_tool_calls"}:
            await self._run_turn_and_maybe_execute_tools(
                ctx=ctx,
                runtime=runtime,
                env=env,
                base_meta=base_meta,
                request_type="tool_message",
                content=self._RUNTIME_TOOL_MESSAGE_CONTENT,
            )

    async def _handle_tool_result(
        self,
        *,
        ctx: OpenAIConversationContext,
        runtime: Any,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
    ) -> None:
        """处理同步工具结果回灌，并继续一轮 tool_message 推理。

        逻辑：
        1. 打日志；
        2. **`tool_result` 的 SSE `tool_result` 信封**已在各次 **`_invoke_tool`** 内发出，此处不再重复映射；
        3. 调用 **`_run_turn_and_maybe_execute_tools`** 进入 **`tool_message`** 下一轮。

        关键边界：
        - 与 **`_submit_message(..., request_type="tool_result")`** 配对：入队前 **`messages`** 已含对应 **`role=tool`**。
        """
        _logger.info("[begin_handle_tool_result] %s", env.session_id)
        await self._run_turn_and_maybe_execute_tools(
            ctx=ctx,
            runtime=runtime,
            env=env,
            base_meta=base_meta,
            request_type="tool_message",
            content=self._RUNTIME_TOOL_MESSAGE_CONTENT,
        )

    async def _handle_human_message(
        self,
        *,
        ctx: OpenAIConversationContext,
        runtime: Any,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
    ) -> None:
        """处理默认用户消息路径。

        逻辑：
        1. 若 **`ctx.pending_tool_calls` 非空**：视为用户在「待工具/待审批」阶段插入新 human，对当前 **`PendingToolCall`** 逐条补写打断 **`role=tool`** 并 **`emit` `tool_result`**，然后 **`clear()`** **`pending`** 且 **`run_turn_phase=IDLE`**；
        2. **`_run_turn_and_maybe_execute_tools`** 以 **`human_message`** 进入下一轮模型。

        关键边界：
        - **`pending` 为空**时不写占位 tool，直接走 2；
        - 先 **`list(pending)` 快照再遍历**，避免遍历中改列表。

        副作用说明：
        - 可能修改 **`ctx.messages`**、**`ctx.pending_tool_calls`**、**`ctx.run_turn_phase`**；并可能 **`emit_envelope`**。
        """
        _logger.info("[begin_handle_human_message] %s", env.session_id)
        pending_snapshot = list(ctx.pending_tool_calls)
        for item in pending_snapshot:
            call_id = item.call_id
            tool_name = item.name
            append_openai_message_with_journal(
                ctx,
                {
                    "role": "tool",
                    "tool_call_id": call_id,
                    "content": self._TOOL_USER_INTERRUPTED_MESSAGE,
                },
            )
            await self._emit_envelope(
                env=env,
                envelope=AgentEventEnvelope(
                    event_type="tool_result",
                    payload={
                        "tool_name": tool_name,
                        "tool_call_id": call_id,
                        "content": self._TOOL_USER_INTERRUPTED_MESSAGE,
                        "interrupted_by_user_message": True,
                        "partial": False,
                        "display_type": "normal_text",
                    },
                    meta={},
                ),
                base_meta=base_meta,
            )
        # 打断补位与 pending 一一对应：清空并退出「待工具」阶段，避免与 messages 语义脱节。
        if pending_snapshot:
            ctx.pending_tool_calls.clear()
            ctx.run_turn_phase = RunTurnPhase.IDLE

        await self._run_turn_and_maybe_execute_tools(
            ctx=ctx,
            runtime=runtime,
            env=env,
            base_meta=base_meta,
            request_type="human_message",
            content=str(env.content or ""),
        )

    @staticmethod
    def _build_approval_required_payload(
        tool_calls: list[dict[str, Any]],
        *,
        assistant_content: str = "",
    ) -> dict[str, Any]:
        """把 `tool_call` 列表转换为统一 `approval_required` 载荷。

        逻辑：
        1. 用 `infer_tool_call_display_type` 汇总展示类型（与前置 `tool_call` 事件对齐）；
        2. 组装 `ApprovalRequiredEnvelopePayload` 并 `model_dump` 为 dict。

        关键边界：
        - `assistant_content` 缺省时传空串，推断主要依赖待审批工具名与旁白。
        """
        display_type = infer_tool_call_display_type(str(assistant_content or ""), tool_calls)
        payload = ApprovalRequiredEnvelopePayload(
            message="检测到工具调用，等待用户确认后继续执行。",
            args=ApprovalToolCallsArgs(tool_calls=[ToolCallApprovalItem.model_validate(c) for c in tool_calls]),
            description="OpenAI tool calling 审批",
            display_type=display_type,
        )
        return payload.model_dump()

    def _build_tool_execution_plan(
        self,
        *,
        ctx: OpenAIConversationContext,
        captured_tool_calls: list[dict[str, Any]],
    ) -> ToolExecutionPlan:
        """按审批要求构造本轮工具执行计划。

        逻辑：
        1. 用 runtime 写入的 `ctx.pending_tool_calls` 建立 call_id 索引；
        2. 遍历本轮捕获的 tool_call 事件，逐项调用审批策略；
        3. 将工具拆分为可自动执行与需审批两组，并封装为 `ToolExecutionPlan`。

        关键边界：
        - 事件中不存在于 pending 的 call_id 会被忽略，避免执行未登记工具；
        - 审批策略异常由上层工具调用路径显式暴露前应继续向上抛出。
        """
        pending_by_id = {p.call_id: p for p in ctx.pending_tool_calls}
        auto_exec_calls: list[PendingToolCall] = []
        need_approval_calls: list[PendingToolCall] = []
        for call in captured_tool_calls:
            call_id = str(call.get("id") or "")
            item = pending_by_id.get(call_id)
            if item is None:
                continue
            call_args = item.arguments if isinstance(item.arguments, dict) else {}
            # 审批策略统一收敛到工具层入口，便于后续按工具/参数逐步细化规则。
            requires_approval = should_require_tool_approval(
                tool_name=item.name,
                tool_args=call_args,
                context=ctx,
            )
            if requires_approval:
                need_approval_calls.append(item)
            else:
                auto_exec_calls.append(item)
        return ToolExecutionPlan(
            auto_exec_calls=auto_exec_calls,
            need_approval_calls=need_approval_calls,
        )

    async def _run_turn_and_maybe_execute_tools(
        self,
        *,
        ctx: OpenAIConversationContext,
        runtime: Any,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
        request_type: str,
        content: str,
    ) -> None:
        """统一的一轮 run_turn + 工具执行编排。

        逻辑：
        1. 迭代 **`runtime.run_turn`**，将非 **`done`** 信封立即 **`_emit_envelope`**；
        2. 遇 **`done`**：记录 **`payload.finish_reason`**（若有），并暂存该 **`done`**（通常后续还有一次收口 **`done`**）；
        3. 流结束后将最后一次 **`done`** 的 **`payload`** 与已捕获的 **`finish_reason`** 做 **`setdefault`** 合并；若仍无有效字符串，则按是否捕获 **`tool_calls`** 回退为 **`tool_calls`** 或 **`stop`**，再进入 tool 分支或单次下发。

        关键边界：
        - 无 **`tool_calls`**：仅下发合并后的 **`done`**；
        - 有 **`tool_calls`**：走审批/自动执行分支，**`done`** 在分支内择点下发（载荷仍带合并后的 **`finish_reason`**）。

        副作用说明：
        - 可能 **`_emit_envelope`**、**`_invoke_tool`**、**`_submit_message`**；修改 **`ctx`**（见各分支）。
        """
        captured_tool_calls: list[dict[str, Any]] = []  # 本轮捕获的 tool_call 列表
        captured_assistant_content: str = ""  # 本轮捕获的 assistant_content
        runtime_done_envelope: AgentEventEnvelope | None = None  # 本轮最后一次 `done`（常为收口空载荷）
        # runtime 在流式 `finish_reason` 分片发出 `done` 并暂存；回合结束合并为一条再下发 SSE（网关无末包时编排层兜底 `stop`/`tool_calls`）。
        captured_finish_reason: str = ""
        async for envelope in runtime.run_turn(
            ctx,
            request_type=request_type,
            content=content,
        ):
            if envelope.event_type == "tool_call":
                captured_tool_calls = list(envelope.payload.get("tool_calls", []) or [])
                # 与 `tool_call` 事件的 `assistant_content` 同源，供审批卡片的 `display_type` 推断。
                captured_assistant_content = str(envelope.payload.get("assistant_content", "") or "")
            elif envelope.event_type == "done":
                pl = envelope.payload or {}
                fr = str(pl.get("finish_reason") or "").strip()
                if fr:
                    captured_finish_reason = fr
                runtime_done_envelope = envelope
                continue
            await self._emit_envelope(env=env, envelope=envelope, base_meta=base_meta)

        merged_done_payload: dict[str, Any] = dict(
            (runtime_done_envelope.payload if runtime_done_envelope else {}) or {}
        )
        if captured_finish_reason:
            merged_done_payload.setdefault("finish_reason", captured_finish_reason)
        if not str(merged_done_payload.get("finish_reason") or "").strip():
            merged_done_payload["finish_reason"] = "tool_calls" if captured_tool_calls else "stop"
        final_done_envelope = AgentEventEnvelope(
            event_type="done",
            payload=merged_done_payload,
            meta=dict((runtime_done_envelope.meta if runtime_done_envelope else {}) or {}),
        )

        if not captured_tool_calls:
            await self._emit_envelope(env=env, envelope=final_done_envelope, base_meta=base_meta)
            return
        # 按审批要求生成执行计划；后续所有状态迁移都从 plan 读取，避免隐式 tuple 分支。
        execution_plan = self._build_tool_execution_plan(
            ctx=ctx,
            captured_tool_calls=captured_tool_calls,
        )
        auto_exec_calls = execution_plan.auto_exec_calls
        need_approval_calls = execution_plan.need_approval_calls

        if execution_plan.has_pending_approval:
            for item in need_approval_calls:
                record_tool_approval_required(tool_name=item.name)
            pending_by_id = {p.call_id: p for p in ctx.pending_tool_calls}
            pending_batch = [pending_by_id[str(call.get("id") or "")] for call in captured_tool_calls if str(call.get("id") or "") in pending_by_id]
            ctx.pending_tool_calls = pending_batch
            await self._emit_envelope(
                env=env,
                envelope=AgentEventEnvelope(
                    event_type="approval_required",
                    payload=self._build_approval_required_payload(
                        [
                            {
                                "id": p.call_id,
                                "name": p.name,
                                "arguments": dict(p.arguments),
                                "raw_arguments": json.dumps(p.arguments, ensure_ascii=False),
                            }
                            for p in pending_batch
                        ],
                        assistant_content=captured_assistant_content,
                    ),
                    meta={},
                ),
                base_meta=base_meta,
            )
            ctx.run_turn_phase = RunTurnPhase.AWAITING_TOOL_EXECUTION
            await self._emit_envelope(env=env, envelope=final_done_envelope, base_meta=base_meta)
            return

        auto_exec_tasks = [
            asyncio.create_task(
                self._invoke_tool(ctx, item, env=env, base_meta=base_meta),
            )
            for item in auto_exec_calls
        ]
        auto_exec_results = await asyncio.gather(*auto_exec_tasks)
        executed_results = [
            {
                "tool_name": item.name,
                "tool_call_id": item.call_id,
                "content": result_text,
                "display_type": infer_tool_result_display_type(item.name, result_text),
            }
            for item, result_text in zip(auto_exec_calls, auto_exec_results)
        ]
        for item, result_text in zip(auto_exec_calls, auto_exec_results):
            self._append_tool_message(
                ctx=ctx,
                tool_call_id=item.call_id,
                content=result_text,
            )
        ctx.pending_tool_calls.clear()
        await self._submit_message(
            session_id=env.session_id,
            client_id=env.client_id,
            content="",
            request_type="tool_result",
            tool_result={
                "results": executed_results,
            },
            source="service",
            priority="tool_result",
        )

    @staticmethod
    def _append_tool_message(
        *,
        ctx: OpenAIConversationContext,
        tool_call_id: str,
        content: str,
    ) -> None:
        """将工具执行结果回填到会话消息列表，并写入原始消息 JSONL 记录。

        逻辑：
        1. 组装 **`role=tool`** 消息字典；
        2. **`append_openai_message_with_journal`** 追加并落 JSONL。

        副作用说明：
        - 修改 **`ctx.messages`**；可能写 **`history/`**（或配置目录）下 JSONL 记录文件。
        """
        append_openai_message_with_journal(
            ctx,
            {
                "role": "tool",
                "tool_call_id": str(tool_call_id or "").strip(),
                "content": str(content or ""),
            },
        )

    async def _invoke_tool(
        self,
        ctx: OpenAIConversationContext,
        tool_call: PendingToolCall,
        *,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
    ) -> str:
        """执行单个工具调用并返回文本结果，并向流层发出一条 **`tool_result`** 信封。

        逻辑：
        1. 解析 **`OpenAIToolSpec`**；未注册则得到错误文案；
        2. **`invoke`**；协程则 **`await`**；
        3. 将返回值规范为 **`result_text`**（**`dict`/`list`** → **`json.dumps`**，其余 **`str`**）；
        4. 用 **`infer_tool_result_display_type`** 组装与 **`_handle_tool_result` 历史形态一致**的 **`AgentEventEnvelope`**，**`await _emit_envelope`**；
        5. 返回 **`result_text`** 供 **`_append_tool_message`** 与 **`tool_result` 入队** 使用。

        关键边界：
        - 错误路径（未注册、异常）同样发 **`tool_result`**，便于前端展示失败原因；
        - **`env`/`base_meta`** 须与当前 turn 的 **`MessageEnvelope`** / **`_stream_base_meta`** 一致，否则 SSE 元数据会错位。

        副作用说明：
        - 调用注入的 **`emit_envelope`**（可能写订阅方）；不修改 **`ctx.messages`**（由调用方追加 tool 消息）。
        """
        async def _emit_tool_result_envelope(
            *,
            model_text: str,
            display_text: str,
            raw_ref: str,
            truncated: bool,
            sensitive_filtered: bool,
        ) -> None:
            """将单条同步工具执行结果映射为 SSE **`tool_result`** 并交给 **`emit_envelope`**。"""
            await self._emit_envelope(
                env=env,
                envelope=AgentEventEnvelope(
                    event_type="tool_result",
                    payload={
                        "tool_name": tool_call.name,
                        "tool_call_id": tool_call.call_id,
                        "content": display_text,
                        "model_content": model_text,
                        "raw_ref": raw_ref,
                        "truncated": truncated,
                        "sensitive_filtered": sensitive_filtered,
                        "partial": False,
                        "display_type": infer_tool_result_display_type(tool_call.name, display_text),
                    },
                    meta={},
                ),
                base_meta=base_meta,
            )

        spec = self._tool_map.get(tool_call.name)
        if spec is None:
            result_text = f"ERROR: 未注册的工具：{tool_call.name!r}"
            record_tool_execution_result(tool_name=tool_call.name, ok=False)
            envelope = package_tool_result(tool_name=tool_call.name, content=result_text)
            await _emit_tool_result_envelope(
                model_text=envelope.model_content,
                display_text=envelope.display_content,
                raw_ref=envelope.raw_ref,
                truncated=envelope.truncated,
                sensitive_filtered=envelope.sensitive_filtered,
            )
            return envelope.model_content
        try:
            result = spec.invoke(tool_call.arguments, ctx)
            if asyncio.iscoroutine(result):
                result = await result
            if isinstance(result, (dict, list)):
                result_text = json.dumps(result, ensure_ascii=False)
            else:
                result_text = str(result)
        except Exception as exc:  # noqa: BLE001
            result_text = f"ERROR: 工具 {tool_call.name!r} 执行失败: {exc}"
        record_tool_execution_result(tool_name=tool_call.name, ok=not result_text.startswith("ERROR:"))
        envelope = package_tool_result(tool_name=tool_call.name, content=result_text)
        await _emit_tool_result_envelope(
            model_text=envelope.model_content,
            display_text=envelope.display_content,
            raw_ref=envelope.raw_ref,
            truncated=envelope.truncated,
            sensitive_filtered=envelope.sensitive_filtered,
        )
        return envelope.model_content

    @staticmethod
    def _classify_tool_result_tail(
        ctx: OpenAIConversationContext,
    ) -> Literal["tail_tool", "tail_assistant_with_tool_calls", "tail_assistant_without_tool_calls", "other"]:
        """按最后一条消息形态分类异步 `tool_result` 的插入策略。"""
        if not ctx.messages:
            return "other"
        last = ctx.messages[-1]
        if not isinstance(last, dict):
            return "other"
        role = str(last.get("role") or "")
        if role == "tool":
            return "tail_tool"
        if role == "assistant":
            if last.get("tool_calls"):
                return "tail_assistant_with_tool_calls"
            return "tail_assistant_without_tool_calls"
        return "other"

    @staticmethod
    def _build_tool_result_messages(
        payload: dict[str, Any],
    ) -> tuple[dict[str, Any], dict[str, Any], dict[str, Any], str, str, str]:
        """构造 `async_tool_result` 写回会话历史所需的 user/assistant/tool 三段消息。"""
        tool_name = str(payload.get("tool_name", "") or "unknown_tool")
        job_id = str(payload.get("job_id", "") or "unknown-job")
        status = str(payload.get("status", "") or "succeeded")
        result_text = str(payload.get("result_text", "") or "")
        error_text = str(payload.get("error_text", "") or "")
        tool_call_id = str(payload.get("tool_call_id", "") or f"async-job-{job_id}")
        result_body = result_text if status == "succeeded" else (error_text or "工具执行失败")
        envelope = package_tool_result(tool_name=tool_name, content=result_body)
        raw_suffix = f" raw_ref={envelope.raw_ref}" if envelope.raw_ref else ""
        tool_text = (
            f"工具{tool_name}执行已完成，job_id：{job_id}，执行结果如下："
            f"{envelope.model_content}{raw_suffix}"
        )
        user_text = f"工具{tool_name}，job_id已完成，请获取执行结果并继续任务。"
        assistant_message = {
            "role": "assistant",
            "content": "",
            "tool_calls": [
                {
                    "id": tool_call_id,
                    "type": "function",
                    "function": {
                        "name": "tool_callback",
                        "arguments": json.dumps(
                            {"job_id": job_id, "tool_name": tool_name, "status": status},
                            ensure_ascii=False,
                        ),
                    },
                }
            ],
        }
        tool_message = {
            "role": "tool",
            "tool_call_id": tool_call_id,
            "content": tool_text,
        }
        user_message = {"role": "user", "content": user_text}
        return user_message, assistant_message, tool_message, tool_name, tool_call_id, status


def init_agent() -> OpenAIImplicitReActRuntime:
    """创建 OpenAI 隐式 ReAct 运行时实例。"""
    return OpenAIImplicitReActRuntime()
