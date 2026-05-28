"""主 Agent 入口：OpenAI 隐式 ReAct runtime 工厂与回合业务编排器。"""

from __future__ import annotations

import asyncio
import json
from typing import Any, Awaitable, Callable, Literal

from app.config.settings import get_settings
from app.context.models import OpenAIConversationContext, PendingToolCall, RunTurnPhase
from app.harness.history.raw_message_journal import append_openai_message_with_journal, insert_openai_message_with_journal
from app.harness.queue.message_queue import MessageEnvelope
from app.harness.service.interface import AgentEventEnvelope
from app.harness.tools.result_policy import package_tool_result
from app.observability.metrics import record_tool_execution_result

from app.core.main_agent.display_inference import (
    infer_tool_call_display_type,
    infer_tool_result_display_type,
)
from app.core.main_agent.runtime_openai import OpenAIImplicitReActRuntime
from app.core.main_agent.summary_compression import SummaryCompressionCoordinator, messages_fingerprint
from app.core.main_agent.tool_execution import (
    ToolExecutionCoordinator,
    ToolExecutionPlan,
    build_approval_required_payload,
    build_tool_execution_plan,
    pending_calls_to_dicts,
    pending_tool_call_to_approval_item,
)
from app.core.main_agent.tool_resume import ResumeDecisionPlan, ToolResumeCoordinator, build_resume_decision_plan
from app.harness.tools.user_information import (
    ASK_USER_INFORMATION_TOOL,
    format_user_information_tool_result,
)
from app.schemas.user_information import is_user_information_resume, parse_user_information_resume
from app.core.summary_agent.agent import init_agent as init_summary_agent

import logging

_logger = logging.getLogger(__name__)


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
        self._summary_compression = SummaryCompressionCoordinator(
            emit_envelope=emit_envelope,
            silent_trigger_tokens=int(settings.summary_compression_silent_trigger_tokens),
            blocking_trigger_tokens=int(settings.summary_compression_blocking_trigger_tokens),
            summary_runtime_factory=init_summary_agent,
        )
        self._tool_resume = ToolResumeCoordinator(
            invoke_tool=self._invoke_tool,
            append_tool_message=self._append_tool_message,
            emit_envelope=emit_envelope,
        )
        self._tool_execution = ToolExecutionCoordinator(
            invoke_tool=self._invoke_tool,
            append_tool_message=self._append_tool_message,
            emit_envelope=emit_envelope,
            submit_message=submit_message,
        )
        self._session_summary_tasks = self._summary_compression.session_tasks
        self._session_pending_compression_results = self._summary_compression.pending_results

    async def maybe_handle_summary_compression(
        self,
        *,
        session_id: str,
        ctx: OpenAIConversationContext,
        env: MessageEnvelope | None = None,
        base_meta: dict[str, Any] | None = None,
    ) -> None:
        await self._summary_compression.maybe_handle(
            session_id=session_id,
            ctx=ctx,
            env=env,
            base_meta=base_meta,
        )

    async def cancel_all_summary_tasks(self) -> None:
        await self._summary_compression.cancel_all_tasks()

    async def cancel_session_summary_task(self, *, session_id: str) -> None:
        await self._summary_compression.cancel_session_task(session_id=session_id)

    def _get_summary_runtime(self) -> Any:
        return self._summary_compression.get_runtime()

    async def _try_apply_ready_compression_result(
        self,
        *,
        session_id: str,
        ctx: OpenAIConversationContext,
    ) -> None:
        await self._summary_compression.try_apply_ready_result(session_id=session_id, ctx=ctx)

    async def _run_compression_flow(
        self,
        *,
        session_id: str,
        ctx: OpenAIConversationContext,
        trigger_level: str = "unknown",
    ) -> bool:
        return await self._summary_compression.run_compression_flow(
            session_id=session_id,
            ctx=ctx,
            trigger_level=trigger_level,
        )

    @staticmethod
    def _messages_fingerprint(messages: list[dict[str, Any]]) -> str:
        return messages_fingerprint(messages)

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
        """处理审批后的 resume，或用户回答 `ask_user_information` 的 resume。"""
        _logger.info("[begin_handle_resume] %s", env.session_id)
        if is_user_information_resume(env.resume_value):
            await self._handle_user_information_resume(
                ctx=ctx,
                env=env,
                base_meta=base_meta,
            )
            return

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

        pending_snapshot = list(ctx.pending_tool_calls)
        decision_plan = self._build_resume_decision_plan(env.resume_value, pending_snapshot)
        if not decision_plan.is_valid:
            await self._emit_resume_error_done(
                env=env,
                base_meta=base_meta,
                message=str(decision_plan.error_message or "resume 决策无效。"),
                finish_reason=str(decision_plan.finish_reason or "resume_invalid"),
            )
            return

        executed_results = await self._apply_resume_decision(
            ctx=ctx,
            env=env,
            base_meta=base_meta,
            pending_snapshot=pending_snapshot,
            decision_plan=decision_plan,
        )
        ctx.pending_tool_calls.clear()
        ctx.run_turn_phase = RunTurnPhase.IDLE
        await self._submit_message(
            session_id=env.session_id,
            client_id=env.client_id,
            content="",
            request_type="tool_result",
            tool_result={"results": executed_results},
            source="service",
            priority="tool_result",
        )

    async def _handle_user_information_resume(
        self,
        *,
        ctx: OpenAIConversationContext,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
    ) -> None:
        """处理用户对 `ask_user_information` 的回答并继续工具链。

        逻辑：
        1. 校验 resume 载荷与 pending 中对应 call_id；
        2. 写入 tool message 并 emit SSE `tool_result`；
        3. 从 pending 移除已回答项，并尝试自动执行剩余免审批工具；
        4. 以 `tool_result` 入队触发下一轮 `tool_message` 推理。
        """
        parsed = parse_user_information_resume(env.resume_value)
        if parsed is None:
            await self._emit_resume_error_done(
                env=env,
                base_meta=base_meta,
                message="user_information resume 载荷无效。",
                finish_reason="resume_user_information_invalid",
            )
            return
        if not ctx.pending_tool_calls:
            await self._emit_resume_error_done(
                env=env,
                base_meta=base_meta,
                message="没有可恢复的 pending tool 调用。",
                finish_reason="resume_no_pending_tools",
            )
            return

        pending_item = next(
            (item for item in ctx.pending_tool_calls if item.call_id == parsed.tool_call_id),
            None,
        )
        if pending_item is None or pending_item.name != ASK_USER_INFORMATION_TOOL:
            await self._emit_resume_error_done(
                env=env,
                base_meta=base_meta,
                message="resume 中的 tool_call_id 与 pending ask_user_information 不匹配。",
                finish_reason="resume_user_information_mismatch",
            )
            return

        if parsed.cancelled:
            result_text = format_user_information_tool_result(
                answer="",
                selected_options=[],
                cancelled=True,
            )
        else:
            args = pending_item.arguments if isinstance(pending_item.arguments, dict) else {}
            required = bool(args.get("required", True))
            answer = str(parsed.answer or "").strip()
            selected = [str(item).strip() for item in parsed.selected_options if str(item).strip()]
            if required and not answer and not selected:
                await self._emit_resume_error_done(
                    env=env,
                    base_meta=base_meta,
                    message="必填问题未提供回答。",
                    finish_reason="resume_user_information_empty",
                )
                return
            result_text = format_user_information_tool_result(
                answer=answer,
                selected_options=selected,
                cancelled=False,
            )

        self._append_tool_message(ctx=ctx, tool_call_id=pending_item.call_id, content=result_text)
        executed_results: list[dict[str, Any]] = [
            {
                "tool_name": pending_item.name,
                "tool_call_id": pending_item.call_id,
                "content": result_text,
                "display_type": infer_tool_result_display_type(pending_item.name, result_text),
            }
        ]
        await self._emit_envelope(
            env=env,
            envelope=AgentEventEnvelope(
                event_type="tool_result",
                payload={
                    "tool_name": pending_item.name,
                    "tool_call_id": pending_item.call_id,
                    "content": result_text,
                    "partial": False,
                    "display_type": infer_tool_result_display_type(pending_item.name, result_text),
                },
                meta={},
            ),
            base_meta=base_meta,
        )

        ctx.pending_tool_calls = [
            item for item in ctx.pending_tool_calls if item.call_id != pending_item.call_id
        ]

        # 同批剩余 pending 若均为免审批工具，则在此一并执行，避免 partial tool 链悬空。
        while ctx.pending_tool_calls:
            plan = build_tool_execution_plan(
                ctx=ctx,
                captured_tool_calls=pending_calls_to_dicts(ctx.pending_tool_calls),
            )
            if plan.has_pending_user_information or plan.has_pending_approval:
                break
            if not plan.auto_exec_calls:
                break
            batch_results = await self._execute_auto_tool_batch_collect(
                ctx=ctx,
                env=env,
                base_meta=base_meta,
                auto_exec_calls=plan.auto_exec_calls,
            )
            executed_results.extend(batch_results)
            executed_ids = {item.call_id for item in plan.auto_exec_calls}
            ctx.pending_tool_calls = [
                item for item in ctx.pending_tool_calls if item.call_id not in executed_ids
            ]

        if not ctx.pending_tool_calls:
            ctx.run_turn_phase = RunTurnPhase.IDLE
        await self._submit_message(
            session_id=env.session_id,
            client_id=env.client_id,
            content="",
            request_type="tool_result",
            tool_result={"results": executed_results},
            source="service",
            priority="tool_result",
        )

    async def _execute_auto_tool_batch_collect(
        self,
        *,
        ctx: OpenAIConversationContext,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
        auto_exec_calls: list[PendingToolCall],
    ) -> list[dict[str, Any]]:
        """执行一批免审批工具并返回结果列表（不清空 pending，不单独入队）。"""
        auto_exec_tasks = [
            asyncio.create_task(self._invoke_tool(ctx, item, env=env, base_meta=base_meta))
            for item in auto_exec_calls
        ]
        auto_exec_results = await asyncio.gather(*auto_exec_tasks)
        executed_results: list[dict[str, Any]] = []
        for item, result_text in zip(auto_exec_calls, auto_exec_results):
            self._append_tool_message(ctx=ctx, tool_call_id=item.call_id, content=result_text)
            executed_results.append(
                {
                    "tool_name": item.name,
                    "tool_call_id": item.call_id,
                    "content": result_text,
                    "display_type": infer_tool_result_display_type(item.name, result_text),
                }
            )
        return executed_results

    async def _emit_resume_error_done(
        self,
        *,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
        message: str,
        finish_reason: str,
    ) -> None:
        await self._emit_envelope(
            env=env,
            envelope=AgentEventEnvelope(event_type="error", payload={"message": message}, meta={}),
            base_meta=base_meta,
        )
        await self._emit_envelope(
            env=env,
            envelope=AgentEventEnvelope(event_type="done", payload={"finish_reason": finish_reason}, meta={}),
            base_meta=base_meta,
        )

    def _build_resume_decision_plan(
        self,
        resume_value: Any,
        pending_snapshot: list[PendingToolCall],
    ) -> ResumeDecisionPlan:
        return build_resume_decision_plan(resume_value, pending_snapshot)

    async def _apply_resume_decision(
        self,
        *,
        ctx: OpenAIConversationContext,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
        pending_snapshot: list[PendingToolCall],
        decision_plan: ResumeDecisionPlan,
    ) -> list[dict[str, Any]]:
        return await self._tool_resume.apply_decision(
            ctx=ctx,
            env=env,
            base_meta=base_meta,
            pending_snapshot=pending_snapshot,
            decision_plan=decision_plan,
        )

    async def _execute_approved_resume_tool(
        self,
        *,
        ctx: OpenAIConversationContext,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
        item: PendingToolCall,
    ) -> dict[str, Any]:
        return await self._tool_resume.execute_approved_tool(ctx=ctx, env=env, base_meta=base_meta, item=item)

    async def _record_rejected_resume_tool(
        self,
        *,
        ctx: OpenAIConversationContext,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
        item: PendingToolCall,
    ) -> dict[str, Any]:
        return await self._tool_resume.record_rejected_tool(ctx=ctx, env=env, base_meta=base_meta, item=item)

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
        return build_approval_required_payload(tool_calls, assistant_content=assistant_content)

    def _build_tool_execution_plan(
        self,
        *,
        ctx: OpenAIConversationContext,
        captured_tool_calls: list[dict[str, Any]],
    ) -> ToolExecutionPlan:
        return build_tool_execution_plan(ctx=ctx, captured_tool_calls=captured_tool_calls)

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
        if execution_plan.has_pending_user_information:
            await self._wait_for_user_information(
                ctx=ctx,
                env=env,
                base_meta=base_meta,
                captured_tool_calls=captured_tool_calls,
                final_done_envelope=final_done_envelope,
                execution_plan=execution_plan,
            )
            return
        if execution_plan.has_pending_approval:
            await self._wait_for_tool_approval_batch(
                ctx=ctx,
                env=env,
                base_meta=base_meta,
                captured_tool_calls=captured_tool_calls,
                captured_assistant_content=captured_assistant_content,
                final_done_envelope=final_done_envelope,
                execution_plan=execution_plan,
            )
            return

        await self._execute_auto_tool_batch(
            ctx=ctx,
            env=env,
            base_meta=base_meta,
            auto_exec_calls=execution_plan.auto_exec_calls,
        )

    async def _wait_for_tool_approval_batch(
        self,
        *,
        ctx: OpenAIConversationContext,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
        captured_tool_calls: list[dict[str, Any]],
        captured_assistant_content: str,
        final_done_envelope: AgentEventEnvelope,
        execution_plan: ToolExecutionPlan,
    ) -> None:
        await self._tool_execution.wait_for_approval_batch(
            ctx=ctx,
            env=env,
            base_meta=base_meta,
            captured_tool_calls=captured_tool_calls,
            captured_assistant_content=captured_assistant_content,
            final_done_envelope=final_done_envelope,
            execution_plan=execution_plan,
        )

    async def _wait_for_user_information(
        self,
        *,
        ctx: OpenAIConversationContext,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
        captured_tool_calls: list[dict[str, Any]],
        final_done_envelope: AgentEventEnvelope,
        execution_plan: ToolExecutionPlan,
    ) -> None:
        await self._tool_execution.wait_for_user_information(
            ctx=ctx,
            env=env,
            base_meta=base_meta,
            captured_tool_calls=captured_tool_calls,
            final_done_envelope=final_done_envelope,
            execution_plan=execution_plan,
        )

    @staticmethod
    def _pending_tool_call_to_approval_item(item: PendingToolCall) -> dict[str, Any]:
        return pending_tool_call_to_approval_item(item)

    async def _execute_auto_tool_batch(
        self,
        *,
        ctx: OpenAIConversationContext,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
        auto_exec_calls: list[PendingToolCall],
    ) -> None:
        await self._tool_execution.execute_auto_batch(
            ctx=ctx,
            env=env,
            base_meta=base_meta,
            auto_exec_calls=auto_exec_calls,
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
        """构造 `async_tool_result` 写回会话历史所需的三段消息。

        逻辑：
        1. 从异步任务 payload 提取工具名、job、状态与结果；
        2. 将结果交给 `package_tool_result` 做模型侧内容裁剪/脱敏；
        3. 合成 `user` 提醒、`assistant.tool_calls=tool_callback` 与对应 `tool` 消息。

        关键边界：
        - 失败/取消任务优先使用 `error_text`，成功任务使用 `result_text`；
        - 合成 assistant 的 `reasoning_content` 由统一 message 写入口补齐，避免协议逻辑散落。

        与外部交互：
        - 调用 `package_tool_result` 生成模型可消费的工具结果正文。
        """
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
