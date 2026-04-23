"""主 Agent 入口：OpenAI 隐式 ReAct runtime 工厂与回合业务编排器。"""

from __future__ import annotations

import asyncio
import json
from typing import Any, Awaitable, Callable, Literal

from app.config.settings import get_settings
from app.context.models import OpenAIConversationContext, PendingToolCall, RunTurnPhase
from app.harness.queue.message_queue import MessageEnvelope
from app.harness.service.interface import AgentEventEnvelope
from app.schemas.approval import (
    ApprovalRequiredEnvelopePayload,
    ApprovalToolCallsArgs,
    ResumeToolApprove,
    ResumeToolReject,
    ResumeToolSelection,
    ToolCallApprovalItem,
    parse_resume_tool_decision,
)

from app.core.main_agent.runtime_openai import OpenAIImplicitReActRuntime
from app.core.summary_agent.agent import init_agent as init_summary_agent


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
        log: Callable[[str, str, str], None],
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
        self._log = log
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
    ) -> None:
        """在每轮消息处理入口执行 summary 压缩编排。

        逻辑：
        1. 先按阈值判断本轮压缩级别（`silent` / `blocking` / `none`）；
        2. 命中 `silent`：仅当无在途静默任务时启动并记录后台压缩；
        3. 命中 `blocking`：若有在途压缩先阻塞等待；否则（或等待后仍未产出结果）执行阻塞压缩；
        4. 最后统一尝试应用已完成压缩结果（若有）。
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
                self._log("summary", session_id, f"silent compression failed: {exc}")
            running_task = None
        if running_task is not None and not running_task.done():
            has_running_task = True
        else:
            has_running_task = False
        if should_compress and trigger_level == "silent":
            if not has_running_task:
                self._session_summary_tasks[session_id] = asyncio.create_task(
                    self._run_compression_flow(session_id=session_id, ctx=ctx)
                )
        elif should_compress and trigger_level == "blocking":
            if has_running_task:
                try:
                    await running_task
                except asyncio.CancelledError:
                    pass
                except Exception as exc:  # noqa: BLE001
                    self._log("summary", session_id, f"blocking wait silent task failed: {exc}")
            # 阻塞压缩失败后的错误处理策略待补充（当前先留空）。
            blocking_ok = await self._run_compression_flow(session_id=session_id, ctx=ctx)
            if not blocking_ok:
                # TODO: 阻塞压缩失败时返回特定错误消息（当前先留空）。
                pass
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
        3. 替换失败（区间越界/内容空）时直接丢弃待应用结果，不改写消息。
        """
        task = self._session_summary_tasks.get(session_id)
        if task is not None and task.done():
            self._session_summary_tasks.pop(session_id, None)
            try:
                await task
            except asyncio.CancelledError:
                pass
            except Exception as exc:  # noqa: BLE001
                self._log("summary", session_id, f"silent compression failed: {exc}")
        pending = self._session_pending_compression_results.get(session_id)
        if not isinstance(pending, dict):
            return
        self._session_pending_compression_results.pop(session_id, None)
        start = int(pending.get("start", -1))
        end = int(pending.get("end", -1))
        content = str(pending.get("content", "") or "").strip()
        if start < 0 or end < start or end >= len(ctx.messages) or not content:
            return
        replacement = {"role": "user", "content": content}
        ctx.messages = [*ctx.messages[:start], replacement, *ctx.messages[end + 1 :]]
        self._log("summary", session_id, "applied compressed message block")

    async def _run_compression_flow(self, *, session_id: str, ctx: OpenAIConversationContext) -> bool:
        """执行一次完整压缩流程（解析区间 -> 生成摘要 -> 暂存替换结果）。"""
        summary_runtime = self._get_summary_runtime()
        prepared = summary_runtime.build_compression_plan(ctx.messages)
        if not bool(prepared.get("ok")):
            return False
        start = int(prepared.get("start", -1))
        end = int(prepared.get("end", -1))
        block = str(prepared.get("block") or "").strip()
        if start < 0 or end < start or end >= len(ctx.messages) or not block:
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
            self._log("summary", session_id, f"compression failed: {exc}")
            return False
        if not summary_text or not str(summary_text).strip():
            return False
        self._session_pending_compression_results[session_id] = {
            "start": start,
            "end": end,
            "content": str(summary_text).strip(),
        }
        return True

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
        await self.maybe_handle_summary_compression(session_id=env.session_id, ctx=ctx)
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

    async def _handle_resume(
        self,
        *,
        ctx: OpenAIConversationContext,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
    ) -> None:
        """处理审批后的 resume：执行批准工具、处理拒绝并回灌 tool_result。"""
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
                envelope=AgentEventEnvelope(event_type="done", payload={}, meta={}),
                base_meta=base_meta,
            )
            return

        decision = parse_resume_tool_decision(env.resume_value)
        if isinstance(decision, ResumeToolReject):
            await self._emit_envelope(
                env=env,
                envelope=AgentEventEnvelope(
                    event_type="error",
                    payload={"message": "工具执行已被拒绝。"},
                    meta={},
                ),
                base_meta=base_meta,
            )
            await self._emit_envelope(
                env=env,
                envelope=AgentEventEnvelope(event_type="done", payload={}, meta={}),
                base_meta=base_meta,
            )
            return

        pending_by_id = {p.call_id: p for p in ctx.pending_tool_calls}
        approved_ids: set[str]
        rejected_ids: set[str]
        if isinstance(decision, ResumeToolApprove):
            approved_ids = set(pending_by_id.keys())
            rejected_ids = set()
        elif isinstance(decision, ResumeToolSelection):
            approved_ids = {str(c).strip() for c in decision.approved}
            rejected_ids = {str(c).strip() for c in decision.rejected}
        else:
            approved_ids = set()
            rejected_ids = set()

        executed_results: list[dict[str, Any]] = []
        remaining_pending: list[type(ctx.pending_tool_calls[0])] = []
        for item in list(ctx.pending_tool_calls):
            call_id = item.call_id
            if call_id in approved_ids:
                result_text = await self._invoke_tool(ctx, item)
                self._append_tool_message(
                    ctx=ctx,
                    tool_call_id=item.call_id,
                    content=result_text,
                )
                executed_results.append(
                    {
                        "tool_name": item.name,
                        "tool_call_id": item.call_id,
                    }
                )
            elif call_id in rejected_ids:
                ctx.messages.append(
                    {
                        "role": "tool",
                        "tool_call_id": call_id,
                        "content": f"工具 {item.name!r} 已被用户拒绝执行。",
                    }
                )
            else:
                remaining_pending.append(item)

        if remaining_pending:
            ctx.pending_tool_calls = remaining_pending
            return

        await self._submit_message(
            session_id=env.session_id,
            content="",
            request_type="tool_result",
            tool_result={
                "results": executed_results,
            },
            source="service",
            priority="tool_result",
            stream_id=env.stream_id,
        )
        ctx.pending_tool_calls.clear()

    async def _handle_async_tool_result(
        self,
        *,
        ctx: OpenAIConversationContext,
        runtime: Any,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
    ) -> None:
        """处理异步工具完成回灌，并继续一轮 tool_message 推理。"""
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
            ctx.messages.append(assistant_message)
            ctx.messages.append(tool_message)
        elif tail_kind == "tail_assistant_with_tool_calls":
            insert_at = len(ctx.messages) - 1
            ctx.messages.insert(insert_at, assistant_message)
            ctx.messages.insert(insert_at + 1, tool_message)
        elif tail_kind == "tail_assistant_without_tool_calls":
            ctx.messages.append(user_message)
            ctx.messages.append(assistant_message)
            ctx.messages.append(tool_message)
        else:
            ctx.messages.append(assistant_message)
            ctx.messages.append(tool_message)
        await self._emit_envelope(
            env=env,
            envelope=AgentEventEnvelope(
                event_type="tool_call",
                payload={
                    "assistant_content": "",
                    "tool_calls": [
                        {
                            "id": tool_call_id,
                            "name": "tool_callback",
                            "arguments": {"job_id": str(payload.get("job_id", "") or "unknown-job")},
                            "raw_arguments": assistant_message["tool_calls"][0]["function"]["arguments"],
                        }
                    ],
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
                    "content": "",
                    "partial": False,
                    "async_status": status,
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
        """处理同步工具结果回灌，并继续一轮 tool_message 推理。"""
        payload = dict(env.tool_result or {})
        raw_results = payload.get("results")
        if isinstance(raw_results, list):
            for item in raw_results:
                await self._emit_envelope(
                    env=env,
                    envelope=AgentEventEnvelope(
                        event_type="tool_result",
                        payload={
                            "tool_name": str(item.get("tool_name", "") or ""),
                            "tool_call_id": str(item.get("tool_call_id", "") or "").strip(),
                            "content": "",
                            "partial": False,
                        },
                        meta={},
                    ),
                    base_meta=base_meta,
                )
        else:
            await self._emit_envelope(
                env=env,
                envelope=AgentEventEnvelope(
                    event_type="tool_result",
                    payload={
                        "tool_name": str(payload.get("tool_name", "") or ""),
                        "tool_call_id": str(payload.get("tool_call_id", "") or "").strip(),
                        "content": "",
                        "partial": False,
                    },
                    meta={},
                ),
                base_meta=base_meta,
            )
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
        """处理默认用户消息路径。"""
        closed_any = False
        for call_id, tool_name in self._stalled_tool_call_specs(ctx):
            closed_any = True
            ctx.messages.append(
                {
                    "role": "tool",
                    "tool_call_id": call_id,
                    "content": self._TOOL_USER_INTERRUPTED_MESSAGE,
                }
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
                    },
                    meta={},
                ),
                base_meta=base_meta,
            )
        if closed_any and ctx.pending_tool_calls:
            ctx.pending_tool_calls.clear()

        await self._run_turn_and_maybe_execute_tools(
            ctx=ctx,
            runtime=runtime,
            env=env,
            base_meta=base_meta,
            request_type="human_message",
            content=str(env.content or ""),
        )

    @staticmethod
    def _build_approval_required_payload(tool_calls: list[dict[str, Any]]) -> dict[str, Any]:
        """把 `tool_call` 列表转换为统一 `approval_required` 载荷。"""
        payload = ApprovalRequiredEnvelopePayload(
            message="检测到工具调用，等待用户确认后继续执行。",
            args=ApprovalToolCallsArgs(tool_calls=[ToolCallApprovalItem.model_validate(c) for c in tool_calls]),
            description="OpenAI tool calling 审批",
        )
        return payload.model_dump()

    def _split_calls_by_approval(
        self,
        *,
        ctx: OpenAIConversationContext,
        captured_tool_calls: list[dict[str, Any]],
    ) -> tuple[list[PendingToolCall], list[PendingToolCall]]:
        """按审批要求拆分本轮工具调用列表。"""
        pending_by_id = {p.call_id: p for p in ctx.pending_tool_calls}
        auto_exec_calls: list[PendingToolCall] = []
        need_approval_calls: list[PendingToolCall] = []
        for call in captured_tool_calls:
            call_id = str(call.get("id") or "")
            item = pending_by_id.get(call_id)
            if item is None:
                continue
            call_args = item.arguments if isinstance(item.arguments, dict) else {}
            requires_approval = bool(call_args.get("requires_approval", True))
            if requires_approval:
                need_approval_calls.append(item)
            else:
                auto_exec_calls.append(item)
        return auto_exec_calls, need_approval_calls

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
        """统一的一轮 run_turn + 工具执行编排。"""
        captured_tool_calls: list[dict[str, Any]] = []
        runtime_done_envelope: AgentEventEnvelope | None = None
        async for envelope in runtime.run_turn(
            ctx,
            request_type=request_type,
            content=content,
        ):
            if envelope.event_type == "tool_call":
                captured_tool_calls = list(envelope.payload.get("tool_calls", []) or [])
            elif envelope.event_type == "done":
                runtime_done_envelope = envelope
                continue
            await self._emit_envelope(env=env, envelope=envelope, base_meta=base_meta)

        final_done_envelope = runtime_done_envelope or AgentEventEnvelope(
            event_type="done",
            payload={},
            meta={},
        )

        if not captured_tool_calls:
            await self._emit_envelope(env=env, envelope=final_done_envelope, base_meta=base_meta)
            return

        auto_exec_calls, need_approval_calls = self._split_calls_by_approval(
            ctx=ctx,
            captured_tool_calls=captured_tool_calls,
        )
        auto_exec_tasks = [asyncio.create_task(self._invoke_tool(ctx, item)) for item in auto_exec_calls]

        if need_approval_calls:
            ctx.pending_tool_calls = list(need_approval_calls)
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
                            for p in need_approval_calls
                        ]
                    ),
                    meta={},
                ),
                base_meta=base_meta,
            )
            ctx.run_turn_phase = RunTurnPhase.AWAITING_TOOL_EXECUTION
            await self._emit_envelope(env=env, envelope=final_done_envelope, base_meta=base_meta)
            if auto_exec_tasks:
                auto_exec_results = await asyncio.gather(*auto_exec_tasks)
                executed_results = [
                    {
                        "tool_name": item.name,
                        "tool_call_id": item.call_id,
                    }
                    for item, result_text in zip(auto_exec_calls, auto_exec_results)
                ]
                for item, result_text in zip(auto_exec_calls, auto_exec_results):
                    self._append_tool_message(
                        ctx=ctx,
                        tool_call_id=item.call_id,
                        content=result_text,
                    )
            return

        auto_exec_results = await asyncio.gather(*auto_exec_tasks)
        executed_results = [
            {
                "tool_name": item.name,
                "tool_call_id": item.call_id,
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
            content="",
            request_type="tool_result",
            tool_result={
                "results": executed_results,
            },
            source="service",
            priority="tool_result",
            stream_id=env.stream_id,
        )

    @staticmethod
    def _append_tool_message(
        *,
        ctx: OpenAIConversationContext,
        tool_call_id: str,
        content: str,
    ) -> None:
        """将工具执行结果回填到会话消息列表。"""
        ctx.messages.append(
            {
                "role": "tool",
                "tool_call_id": str(tool_call_id or "").strip(),
                "content": str(content or ""),
            }
        )

    async def _invoke_tool(self, ctx: OpenAIConversationContext, tool_call: PendingToolCall) -> str:
        """执行单个工具调用并返回文本结果。"""
        spec = self._tool_map.get(tool_call.name)
        if spec is None:
            return f"ERROR: 未注册的工具：{tool_call.name!r}"
        try:
            result = spec.invoke(tool_call.arguments, ctx)
            if asyncio.iscoroutine(result):
                result = await result
            return str(result)
        except Exception as exc:  # noqa: BLE001
            return f"ERROR: 工具 {tool_call.name!r} 执行失败: {exc}"

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
        tool_text = f"工具{tool_name}执行已完成，job_id：{job_id}，执行结果如下：{result_body}"
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

    @staticmethod
    def _stalled_tool_call_specs(ctx: OpenAIConversationContext) -> list[tuple[str, str]]:
        """当末尾仍是 `assistant(tool_calls)` 时，返回需要补写占位 tool 的 `(call_id, tool_name)` 列表。"""
        if not ctx.messages:
            return []
        last = ctx.messages[-1]
        if not isinstance(last, dict) or last.get("role") != "assistant":
            return []
        raw_calls = last.get("tool_calls") or []
        if not raw_calls:
            return []
        pending_names = {p.call_id: p.name for p in ctx.pending_tool_calls}
        out: list[tuple[str, str]] = []
        for idx, c in enumerate(raw_calls):
            if not isinstance(c, dict):
                continue
            call_id = str(c.get("id") or f"tool-call-{idx}")
            fn = c.get("function", {}) if isinstance(c.get("function"), dict) else {}
            name = str(fn.get("name") or "") or pending_names.get(call_id, "")
            out.append((call_id, name))
        return out


def init_agent() -> OpenAIImplicitReActRuntime:
    """创建 OpenAI 隐式 ReAct 运行时实例。"""
    return OpenAIImplicitReActRuntime()
