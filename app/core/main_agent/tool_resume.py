"""Tool resume approval/rejection coordination."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Awaitable, Callable

from app.context.models import OpenAIConversationContext, PendingToolCall
from app.core.main_agent.display_inference import infer_tool_result_display_type
from app.harness.queue.message_queue import MessageEnvelope
from app.harness.service.interface import AgentEventEnvelope
from app.schemas.approval import ResumeToolApprove, ResumeToolReject, ResumeToolSelection, parse_resume_tool_decision


EmitEnvelope = Callable[..., Awaitable[None]]
InvokeTool = Callable[..., Awaitable[str]]
AppendToolMessage = Callable[..., None]


@dataclass(frozen=True, slots=True)
class ResumeDecisionPlan:
    approved_ids: set[str]
    rejected_ids: set[str]
    error_message: str | None = None
    finish_reason: str | None = None

    @property
    def is_valid(self) -> bool:
        return self.error_message is None


def build_resume_decision_plan(resume_value: Any, pending_snapshot: list[PendingToolCall]) -> ResumeDecisionPlan:
    decision = parse_resume_tool_decision(resume_value)
    pending_ids = {p.call_id for p in pending_snapshot}
    if isinstance(decision, ResumeToolApprove):
        return ResumeDecisionPlan(approved_ids=set(pending_ids), rejected_ids=set())
    if isinstance(decision, ResumeToolReject):
        return ResumeDecisionPlan(approved_ids=set(), rejected_ids=set(pending_ids))
    if isinstance(decision, ResumeToolSelection):
        approved_ids = {str(c).strip() for c in decision.approved if str(c).strip()}
        rejected_ids = {str(c).strip() for c in decision.rejected if str(c).strip()}
        decided_ids = approved_ids | rejected_ids
        invalid_ids = decided_ids - pending_ids
        duplicate_ids = approved_ids & rejected_ids
        if invalid_ids or duplicate_ids or decided_ids != pending_ids:
            return ResumeDecisionPlan(
                approved_ids=set(),
                rejected_ids=set(),
                error_message="selection resume 必须一次性覆盖全部 pending tool 调用，且不能包含未知或重复 call_id。",
                finish_reason="resume_selection_invalid",
            )
        return ResumeDecisionPlan(approved_ids=approved_ids, rejected_ids=rejected_ids)
    return ResumeDecisionPlan(approved_ids=set(), rejected_ids=set(pending_ids))


class ToolResumeCoordinator:
    def __init__(
        self,
        *,
        invoke_tool: InvokeTool,
        append_tool_message: AppendToolMessage,
        emit_envelope: EmitEnvelope,
    ) -> None:
        self._invoke_tool = invoke_tool
        self._append_tool_message = append_tool_message
        self._emit_envelope = emit_envelope

    async def apply_decision(
        self,
        *,
        ctx: OpenAIConversationContext,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
        pending_snapshot: list[PendingToolCall],
        decision_plan: ResumeDecisionPlan,
    ) -> list[dict[str, Any]]:
        executed_results: list[dict[str, Any]] = []
        for item in pending_snapshot:
            if item.call_id in decision_plan.approved_ids:
                executed_results.append(
                    await self.execute_approved_tool(ctx=ctx, env=env, base_meta=base_meta, item=item)
                )
            elif item.call_id in decision_plan.rejected_ids:
                executed_results.append(
                    await self.record_rejected_tool(ctx=ctx, env=env, base_meta=base_meta, item=item)
                )
        return executed_results

    async def execute_approved_tool(
        self,
        *,
        ctx: OpenAIConversationContext,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
        item: PendingToolCall,
    ) -> dict[str, Any]:
        result_text = await self._invoke_tool(ctx, item, env=env, base_meta=base_meta)
        self._append_tool_message(ctx=ctx, tool_call_id=item.call_id, content=result_text)
        return {
            "tool_name": item.name,
            "tool_call_id": item.call_id,
            "content": result_text,
            "display_type": infer_tool_result_display_type(item.name, result_text),
        }

    async def record_rejected_tool(
        self,
        *,
        ctx: OpenAIConversationContext,
        env: MessageEnvelope,
        base_meta: dict[str, Any],
        item: PendingToolCall,
    ) -> dict[str, Any]:
        result_text = f"工具 {item.name!r} 已被用户拒绝执行。"
        display_type = infer_tool_result_display_type(item.name, result_text)
        self._append_tool_message(ctx=ctx, tool_call_id=item.call_id, content=result_text)
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
                    "display_type": display_type,
                },
                meta={},
            ),
            base_meta=base_meta,
        )
        return {
            "tool_name": item.name,
            "tool_call_id": item.call_id,
            "content": result_text,
            "rejected": True,
            "display_type": display_type,
        }
