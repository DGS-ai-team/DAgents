"""Tool execution planning and batch coordination."""

from __future__ import annotations

import asyncio
import json
from dataclasses import dataclass
from typing import Any, Awaitable, Callable

from app.context.models import OpenAIConversationContext, PendingToolCall, RunTurnPhase
from app.core.main_agent.display_inference import infer_tool_call_display_type, infer_tool_result_display_type
from app.harness.queue.message_queue import MessageEnvelope
from app.harness.service.interface import AgentEventEnvelope
from app.harness.tools.tool import ToolApprovalDecision, decide_tool_approval
from app.observability.metrics import record_tool_approval_required
from app.schemas.approval import ApprovalRequiredEnvelopePayload, ApprovalToolCallsArgs, ToolCallApprovalItem


EmitEnvelope = Callable[..., Awaitable[None]]
SubmitMessage = Callable[..., Awaitable[None]]
InvokeTool = Callable[..., Awaitable[str]]
AppendToolMessage = Callable[..., None]


@dataclass(frozen=True, slots=True)
class ToolExecutionPlan:
    auto_exec_calls: list[PendingToolCall]
    need_approval_calls: list[PendingToolCall]
    approval_decisions: dict[str, ToolApprovalDecision]

    @property
    def has_pending_approval(self) -> bool:
        return bool(self.need_approval_calls)


def build_tool_execution_plan(
    *,
    ctx: OpenAIConversationContext,
    captured_tool_calls: list[dict[str, Any]],
) -> ToolExecutionPlan:
    pending_by_id = {p.call_id: p for p in ctx.pending_tool_calls}
    auto_exec_calls: list[PendingToolCall] = []
    need_approval_calls: list[PendingToolCall] = []
    approval_decisions: dict[str, ToolApprovalDecision] = {}
    for call in captured_tool_calls:
        call_id = str(call.get("id") or "")
        item = pending_by_id.get(call_id)
        if item is None:
            continue
        call_args = item.arguments if isinstance(item.arguments, dict) else {}
        decision = decide_tool_approval(
            tool_name=item.name,
            tool_args=call_args,
            context=ctx,
        )
        approval_decisions[item.call_id] = decision
        if decision.require_approval:
            need_approval_calls.append(item)
        else:
            auto_exec_calls.append(item)
    return ToolExecutionPlan(
        auto_exec_calls=auto_exec_calls,
        need_approval_calls=need_approval_calls,
        approval_decisions=approval_decisions,
    )


def build_approval_required_payload(
    tool_calls: list[dict[str, Any]],
    *,
    assistant_content: str = "",
) -> dict[str, Any]:
    display_type = infer_tool_call_display_type(str(assistant_content or ""), tool_calls)
    payload = ApprovalRequiredEnvelopePayload(
        message="检测到工具调用，等待用户确认后继续执行。",
        args=ApprovalToolCallsArgs(tool_calls=[ToolCallApprovalItem.model_validate(c) for c in tool_calls]),
        description="OpenAI tool calling 审批",
        display_type=display_type,
    )
    return payload.model_dump()


def pending_tool_call_to_approval_item(
    item: PendingToolCall,
    *,
    decision: ToolApprovalDecision | None = None,
) -> dict[str, Any]:
    payload = {
        "id": item.call_id,
        "name": item.name,
        "arguments": dict(item.arguments),
        "raw_arguments": json.dumps(item.arguments, ensure_ascii=False),
    }
    if decision is not None:
        payload.update(
            {
                "approval_reason": decision.reason,
                "risk_level": decision.risk_level,
                "approval_mode": decision.mode,
            }
        )
    return payload


class ToolExecutionCoordinator:
    def __init__(
        self,
        *,
        invoke_tool: InvokeTool,
        append_tool_message: AppendToolMessage,
        emit_envelope: EmitEnvelope,
        submit_message: SubmitMessage,
    ) -> None:
        self._invoke_tool = invoke_tool
        self._append_tool_message = append_tool_message
        self._emit_envelope = emit_envelope
        self._submit_message = submit_message

    async def wait_for_approval_batch(
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
        for item in execution_plan.need_approval_calls:
            record_tool_approval_required(tool_name=item.name)
        pending_by_id = {p.call_id: p for p in ctx.pending_tool_calls}
        pending_batch = [
            pending_by_id[str(call.get("id") or "")]
            for call in captured_tool_calls
            if str(call.get("id") or "") in pending_by_id
        ]
        ctx.pending_tool_calls = pending_batch
        await self._emit_envelope(
            env=env,
            envelope=AgentEventEnvelope(
                event_type="approval_required",
                payload=build_approval_required_payload(
                    [
                        pending_tool_call_to_approval_item(
                            p,
                            decision=execution_plan.approval_decisions.get(p.call_id),
                        )
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

    async def execute_auto_batch(
        self,
        *,
        ctx: OpenAIConversationContext,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
        auto_exec_calls: list[PendingToolCall],
    ) -> None:
        auto_exec_tasks = [
            asyncio.create_task(self._invoke_tool(ctx, item, env=env, base_meta=base_meta))
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
            self._append_tool_message(ctx=ctx, tool_call_id=item.call_id, content=result_text)
        ctx.pending_tool_calls.clear()
        await self._submit_message(
            session_id=env.session_id,
            client_id=env.client_id,
            content="",
            request_type="tool_result",
            tool_result={"results": executed_results},
            source="service",
            priority="tool_result",
        )
