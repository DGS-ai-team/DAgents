from __future__ import annotations

import asyncio
import json
import time
import uuid
from typing import Any, Literal

import httpx
from pydantic import BaseModel, ConfigDict, Field

from app.config.settings import get_settings
from app.context.models import OpenAIConversationContext
from app.observability.metrics import record_a2a_operation, record_a2a_terminal_state
from app.schemas.agent_peer import AgentPeerError, AgentPeerTaskState, build_agent_peer_envelope

DEFAULT_HTTP_TIMEOUT_SECONDS = 15.0
A2A_TOKEN_HEADER = "x-dagents-a2a-token"


class PeerApprovalEntry(BaseModel):
    model_config = ConfigDict(extra="allow")

    target_session_id: str = Field(description="对端会话 ID（用于审批 resume 路由）。")
    approval_id: str | None = Field(default=None, description="审批批次 ID。")
    approval_type: str = Field(default="execute_tool", description="审批类型。")
    content: str = Field(default="", description="对端审批提示文本。")
    description: str = Field(default="", description="审批描述。")
    display_type: str = Field(default="normal_text", description="展示类型。")
    approval_args: dict[str, Any] = Field(default_factory=dict, description="审批参数。")


class PeerStreamSummary(BaseModel):
    model_config = ConfigDict(extra="allow")

    text: str = Field(default="", description="对 Agent 可读的拼接正文。")
    approvals: list[PeerApprovalEntry] = Field(default_factory=list, description="对端审批事件汇总。")
    errors: list[str] = Field(default_factory=list, description="对端 error 事件文本列表。")
    final_state: Literal["succeeded", "requires_input", "failed", "truncated"] = Field(default="succeeded")
    truncated: bool = Field(default=False, description="是否因超时截断。")


def session_id_from_context(context: OpenAIConversationContext | None, fallback_prefix: str) -> str:
    if context is not None and (context.session_id or "").strip():
        return context.session_id.strip()
    return f"{fallback_prefix}-{uuid.uuid4().hex[:8]}"


def new_peer_session_id(*, caller_session_id: str, target_agent_id: str) -> str:
    short = uuid.uuid4().hex[:10]
    safe_caller = (caller_session_id or "").strip() or "anon"
    safe_target = (target_agent_id or "").strip() or "unknown"
    return f"peer-{safe_caller}-{safe_target}-{short}"


def json_text(payload: dict[str, Any]) -> str:
    return json.dumps(payload, ensure_ascii=False)


def stable_groups(raw_groups: list[str] | None) -> list[str]:
    if not raw_groups:
        return []
    seen: set[str] = set()
    result: list[str] = []
    for item in raw_groups:
        cleaned = (item or "").strip()
        if not cleaned or cleaned in seen:
            continue
        seen.add(cleaned)
        result.append(cleaned)
    return result


def a2a_auth_headers() -> dict[str, str]:
    token = (get_settings().agent_peer_shared_token or "").strip()
    if not token:
        return {}
    return {A2A_TOKEN_HEADER: token}


def build_error_envelope_text(
    *,
    intent: str,
    session_id: str,
    target_agent_id: str | None,
    target_groups: list[str] | None,
    message: str,
    code: str,
    retryable: bool,
    trace_id: str | None = None,
) -> str:
    s = get_settings()
    env = build_agent_peer_envelope(
        caller_agent_id=s.agent_id,
        caller_session_id=session_id,
        caller_groups=s.discovery_groups,
        target_agent_id=target_agent_id,
        target_groups=target_groups,
        intent=intent,  # type: ignore[arg-type]
        payload_content={"ok": False, "message": message},
        payload_content_type="application/json",
        trace_id=trace_id,
        error=AgentPeerError(code=code, message=message, retryable=retryable),
    )
    return json_text(env.model_dump())


def approval_entry_from_event(*, target_session_id: str, data: dict[str, Any]) -> PeerApprovalEntry:
    raw_args = data.get("approval_args")
    safe_args = raw_args if isinstance(raw_args, dict) else {}
    return PeerApprovalEntry(
        target_session_id=target_session_id,
        approval_id=(str(data.get("approval_id")) if data.get("approval_id") is not None else None),
        approval_type=str(data.get("approval_type") or "execute_tool"),
        content=str(data.get("content") or ""),
        description=str(data.get("description") or ""),
        display_type=str(data.get("display_type") or "normal_text"),
        approval_args=dict(safe_args),
    )


async def collect_peer_stream_summary(
    *,
    base_url: str,
    client_id: str,
    session_id: str,
    timeout_seconds: float,
) -> PeerStreamSummary:
    started = time.monotonic()
    final_base_url = base_url.strip().rstrip("/")
    final_client_id = client_id.strip()
    final_session_id = session_id.strip()
    summary = PeerStreamSummary()
    if not final_base_url or not final_client_id or not final_session_id:
        record_a2a_operation(
            component="agent_peer",
            operation="peer_stream",
            status="invalid_input",
            elapsed_seconds=time.monotonic() - started,
        )
        return summary
    text_lines: list[str] = []
    event_name = ""
    data_lines: list[str] = []
    received_done = False
    try:
        async with asyncio.timeout(max(1.0, timeout_seconds)):
            async with httpx.AsyncClient(timeout=None) as client:
                stream_url = f"{final_base_url}/v1/streams?client_id={final_client_id}"
                headers = {"Last-Event-ID": "-1", **a2a_auth_headers()}
                async with client.stream("GET", stream_url, headers=headers) as resp:
                    resp.raise_for_status()
                    async for line in resp.aiter_lines():
                        if line.startswith("event:"):
                            event_name = line[len("event:") :].strip()
                            continue
                        if line.startswith("data:"):
                            data_lines.append(line[len("data:") :].lstrip())
                            continue
                        if line != "":
                            continue
                        if not data_lines:
                            event_name = ""
                            continue
                        raw_data = "\n".join(data_lines)
                        try:
                            payload = json.loads(raw_data)
                        except Exception:
                            payload = {}
                        if str(payload.get("session_id", "")).strip() != final_session_id:
                            event_name = ""
                            data_lines = []
                            continue
                        stream_event_name = event_name or str(payload.get("type", "") or "")
                        data = payload.get("data", {})
                        if not isinstance(data, dict):
                            data = {}
                        if stream_event_name in {"assistant", "reasoning", "tool_result"}:
                            piece = str(data.get("content", "") or "").strip()
                            if piece:
                                text_lines.append(piece)
                        elif stream_event_name == "error":
                            err_msg = str(data.get("message", "") or "").strip()
                            if err_msg:
                                summary.errors.append(err_msg)
                                text_lines.append(f"[ERROR] {err_msg}")
                        elif stream_event_name == "approval_required":
                            summary.approvals.append(approval_entry_from_event(target_session_id=final_session_id, data=data))
                        elif stream_event_name == "done":
                            received_done = True
                            event_name = ""
                            data_lines = []
                            break
                        event_name = ""
                        data_lines = []
        summary.text = "\n".join(text_lines)
    except TimeoutError:
        summary.text = "\n".join(text_lines)
        summary.truncated = True
    except Exception as exc:
        err_text = str(exc).strip() or "读取远端流失败"
        summary.errors.append(err_text)
        text_lines.append(f"[ERROR] {err_text}")
        summary.text = "\n".join(text_lines)

    if summary.truncated:
        summary.final_state = "truncated"
    elif summary.approvals:
        summary.final_state = "requires_input"
    elif summary.errors:
        summary.final_state = "failed"
    elif received_done:
        summary.final_state = "succeeded"
    else:
        summary.final_state = "failed"
    record_a2a_operation(
        component="agent_peer",
        operation="peer_stream",
        status=summary.final_state,
        elapsed_seconds=time.monotonic() - started,
    )
    record_a2a_terminal_state(component="agent_peer", operation="peer_stream", final_state=summary.final_state)
    return summary


def peer_state_to_task_state(final_state: str) -> AgentPeerTaskState:
    if final_state == "succeeded":
        return "succeeded"
    if final_state == "requires_input":
        return "requires_input"
    if final_state == "truncated":
        return "running"
    return "failed"


def build_resume_value(
    *,
    decision: str,
    approved_call_ids: list[str] | None,
    rejected_call_ids: list[str] | None,
) -> dict[str, Any]:
    final_decision = (decision or "").strip().lower()
    if final_decision == "approve":
        return {"type": "approve"}
    if final_decision == "reject":
        return {"type": "reject"}
    if final_decision == "selection":
        approved = sorted({(c or "").strip() for c in (approved_call_ids or []) if (c or "").strip()})
        rejected = sorted({(c or "").strip() for c in (rejected_call_ids or []) if (c or "").strip()})
        overlap = set(approved) & set(rejected)
        if overlap:
            raise ValueError(f"审批决策中 approved/rejected 不能有重叠 call_id：{sorted(overlap)!r}")
        if not approved and not rejected:
            raise ValueError("selection 决策必须至少在 approved 或 rejected 中提供一个 call_id")
        return {"type": "selection", "approved": approved, "rejected": rejected}
    raise ValueError(f"不支持的审批 decision: {decision!r}（仅支持 approve/reject/selection）")
