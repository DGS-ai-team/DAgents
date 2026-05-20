"""Agent 间交互工具：发现（含卡片信息）、点对点发送、广播、工具审批与任务查询。"""

from __future__ import annotations

import uuid
from typing import Any

import httpx

from app.config.settings import get_settings
from app.context.models import OpenAIConversationContext
from app.harness.tools.agent_peer_common import (
    DEFAULT_HTTP_TIMEOUT_SECONDS as _DEFAULT_HTTP_TIMEOUT_SECONDS,
    PeerApprovalEntry,
    PeerStreamSummary,
    a2a_auth_headers as _a2a_auth_headers,
    build_error_envelope_text as _build_error_envelope_text,
    build_resume_value as _build_resume_value,
    collect_peer_stream_summary as _collect_peer_stream_summary,
    json_text as _json_text,
    new_peer_session_id as _new_peer_session_id,
    peer_state_to_task_state as _peer_state_to_task_state,
    session_id_from_context as _session_id_from_context,
    stable_groups as _stable_groups,
)
from app.harness.tools.agent_peer_registry import (
    attach_agent_card_summary as _attach_agent_card_summary,
    cache_agent_list as _cache_agent_list,
    clear_agent_list_cache as _clear_agent_list_cache,
    discover_agents_by_groups as _discover_agents_by_groups,
    require_registry_url as _require_registry_url,
    resolve_registry_url as _resolve_registry_url,
    resolve_target_agent as _resolve_target_agent,
)
from app.harness.tools.tool import tool
from app.schemas.agent_peer import AgentPeerError, AgentPeerTask, build_agent_peer_envelope


@tool("agent_discover")
def agent_discover(
    context: OpenAIConversationContext | None = None,
) -> str:
    """使用场景：发现与当前 Agent 同组的可协作 Agent，并附带每个 Agent 的 card 摘要；不用于发送消息本身。

    字段说明：
    - 无业务入参：分组范围固定取当前 Agent 配置 `DISCOVERY_GROUPS`。

    返回说明：
    - 成功：返回 JSON 文本，正文包含 `ok/requested_groups/agents`。
    - 失败：返回 JSON 文本，正文包含失败原因（`ok=false` 与错误信息）。

    调用范例：
    - `agent_discover({})`
    """

    s = get_settings()
    session_id = _session_id_from_context(context, "discover")
    groups = _stable_groups(s.discovery_groups)
    if not groups:
        return _build_error_envelope_text(
            intent="ask",
            session_id=session_id,
            target_agent_id=None,
            target_groups=["invalid-group"],
            message="discovery_groups 不能为空",
            code="invalid_groups",
            retryable=False,
        )
    try:
        discovered_agents = _discover_agents_by_groups(groups)
        _cache_agent_list(discovered_agents)
        agents = list(discovered_agents)
        enriched_agents: list[dict[str, Any]] = []
        for item in agents:
            enriched_agents.append(_attach_agent_card_summary(item))
        agents = enriched_agents
        env = build_agent_peer_envelope(
            caller_agent_id=s.agent_id,
            caller_session_id=session_id,
            caller_groups=s.discovery_groups,
            target_agent_id=None,
            target_groups=groups,
            intent="ask",
            payload_content={
                "ok": True,
                "requested_groups": groups,
                "agents": agents,
            },
            payload_content_type="application/json",
        )
        return _json_text(env.model_dump())
    except Exception as exc:
        return _build_error_envelope_text(
            intent="ask",
            session_id=session_id,
            target_agent_id=None,
            target_groups=groups,
            message=str(exc),
            code="discover_failed",
            retryable=True,
        )


@tool("agent_send_message")
async def agent_send_message(
    target_agent_id: str,
    message: str,
    context: OpenAIConversationContext | None = None,
) -> str:
    """使用场景：向指定 Agent 发起点对点委托请求；返回对端流式输出与可能的工具审批摘要。

    字段说明：
    - `target_agent_id`：目标 Agent ID（必填）。
    - `message`：发送给目标 Agent 的消息正文（必填）。

    返回说明：
    - 成功（对端正常 done）：返回 JSON 文本，`task.state="succeeded"`，`payload.content` 含 `target_session_id/stream_output`。
    - 等待审批（对端 `approval_required`）：返回 JSON 文本，`task.state="requires_input"`；`payload.content.approvals[]` 列出每个待审批批次（含 `target_session_id/approval_id/display_type/approval_args.tool_calls`）。后续应调用 **`agent_peer_approve_tools`** 逐批做出决策。
    - 失败：`task.state="failed"`，`error.code` 标注失败类别。

    调用范例：
    - `agent_send_message({"target_agent_id":"agent-b","message":"请总结日报"})`
    """

    s = get_settings()
    msg = message.strip()
    target_id = target_agent_id.strip()
    final_delivery_mode = (s.agent_peer_delivery_mode or "direct").strip().lower()
    caller_session_id = _session_id_from_context(context, f"peer-{s.agent_id}")
    # 对端会话独立命名，避免把调用方会话 id 复用到对端导致跨会话串扰。
    peer_session_id = _new_peer_session_id(
        caller_session_id=caller_session_id,
        target_agent_id=target_id or "unknown",
    )
    trace_id = f"trace-{uuid.uuid4().hex}"
    if not target_id:
        return _build_error_envelope_text(
            intent="delegate",
            session_id=caller_session_id,
            target_agent_id="",
            target_groups=None,
            message="target_agent_id 不能为空",
            code="invalid_target_agent",
            retryable=False,
            trace_id=trace_id,
        )
    if not msg:
        return _build_error_envelope_text(
            intent="delegate",
            session_id=caller_session_id,
            target_agent_id=target_id,
            target_groups=None,
            message="message 不能为空",
            code="invalid_message",
            retryable=False,
            trace_id=trace_id,
        )
    if final_delivery_mode not in {"direct", "relay"}:
        return _build_error_envelope_text(
            intent="delegate",
            session_id=caller_session_id,
            target_agent_id=target_id or "",
            target_groups=None,
            message="配置 AGENT_PEER_DELIVERY_MODE 仅支持 direct 或 relay",
            code="invalid_delivery_mode_config",
            retryable=False,
            trace_id=trace_id,
        )
    try:
        req_env = build_agent_peer_envelope(
            caller_agent_id=s.agent_id,
            caller_session_id=caller_session_id,
            caller_groups=s.discovery_groups,
            target_agent_id=target_id,
            target_groups=None,
            intent="delegate",
            payload_content=msg,
            payload_content_type="text/plain",
            trace_id=trace_id,
        )
        peer_client_id = f"peer-{uuid.uuid4().hex}"
        body = {
            "session_id": peer_session_id,
            "client_id": peer_client_id,
            "request_type": "message",
            "content": _json_text(req_env.model_dump()),
            "source": "agent-peer",
            "priority": "human",
        }
        target_base_url = ""
        async with httpx.AsyncClient(timeout=_DEFAULT_HTTP_TIMEOUT_SECONDS) as client:
            if final_delivery_mode == "direct":
                target = _resolve_target_agent(target_id)
                target_base_url = str(target.get("base_url") or "").strip().rstrip("/")
                resp = await client.post(f"{target_base_url}/v1/messages", json=body, headers=_a2a_auth_headers())
            else:
                registry_url = _require_registry_url()
                relay_payload = {
                    "target_agent_id": target_id,
                    "caller_groups": _stable_groups(s.discovery_groups),
                    "session_id": peer_session_id,
                    "client_id": peer_client_id,
                    "request_type": "message",
                    "content": _json_text(req_env.model_dump()),
                    "source": "agent-peer-relay",
                    "priority": "human",
                }
                resp = await client.post(f"{registry_url}/v1/relay", json=relay_payload, headers=_a2a_auth_headers())
            resp.raise_for_status()
            submit = resp.json()
            if final_delivery_mode == "relay":
                target_base_url = str(submit.get("target_base_url") or "").strip().rstrip("/")
        accepted = bool(submit.get("accepted", False))
        if not accepted:
            raise ValueError("目标 Agent 未确认入队（accepted=false）")
        summary = await _collect_peer_stream_summary(
            base_url=target_base_url,
            client_id=peer_client_id,
            session_id=peer_session_id,
            timeout_seconds=float(max(1, int(s.agent_peer_stream_timeout_seconds))),
        )
        ack_env = build_agent_peer_envelope(
            caller_agent_id=s.agent_id,
            caller_session_id=caller_session_id,
            caller_groups=s.discovery_groups,
            target_agent_id=target_id,
            target_groups=None,
            intent="delegate",
            payload_content={
                "ok": summary.final_state in {"succeeded", "requires_input"},
                "target_agent_id": target_id,
                "target_base_url": target_base_url,
                "target_session_id": peer_session_id,
                "target_client_id": peer_client_id,
                "submit": submit,
                "stream_output": summary.text,
                "stream_output_truncated": summary.truncated,
                "approvals": [item.model_dump() for item in summary.approvals],
                "errors": list(summary.errors),
                "final_state": summary.final_state,
            },
            payload_content_type="application/json",
            trace_id=trace_id,
            task=AgentPeerTask(
                task_id=f"peer-{trace_id}",
                state=_peer_state_to_task_state(summary.final_state),
                artifact_refs=[],
            ),
            error=(
                AgentPeerError(
                    code="peer_failed",
                    message="; ".join(summary.errors)[:500] or "对端流式处理失败",
                    retryable=True,
                )
                if summary.final_state == "failed"
                else None
            ),
        )
        return _json_text(ack_env.model_dump())
    except Exception as exc:
        # 与目标 Agent 通信失败时刷新目录缓存，便于下次改用最新地址重试。
        try:
            _refresh_agent_list_for_visible_groups(_stable_groups(s.discovery_groups))
        except Exception:
            pass
        return _build_error_envelope_text(
            intent="delegate",
            session_id=caller_session_id,
            target_agent_id=target_id,
            target_groups=None,
            message=str(exc),
            code="send_message_failed",
            retryable=True,
            trace_id=trace_id,
        )


@tool("agent_broadcast")
async def agent_broadcast(
    message: str,
    discovery_group_ids: list[str],
    context: OpenAIConversationContext | None = None,
) -> str:
    """使用场景：按多个 discovery_group 广播消息；并发汇总每个目标的流式输出与待审批摘要。

    字段说明：
    - `message`：广播消息正文（必填）。
    - `discovery_group_ids`：目标分组列表（必填，至少一个）。

    返回说明：
    - 成功：返回 JSON 文本，正文包含广播统计、每个目标的 `final_state/stream_output/approvals`。
    - 任一目标在等待审批：`task.state="requires_input"`；调用方应对每个 `approvals[]` 调用 **`agent_peer_approve_tools`** 处理。
    - 失败：返回 JSON 文本，正文包含失败原因（如分组为空或中继服务异常）。

    调用范例：
    - `agent_broadcast({"message":"请同步最新规范","discovery_group_ids":["team-a"]})`
    - `agent_broadcast({"message":"触发巡检","discovery_group_ids":["team-a","team-b"]})`
    """

    s = get_settings()
    caller_session_id = _session_id_from_context(context, "broadcast")
    trace_id = f"trace-{uuid.uuid4().hex}"
    msg = message.strip()
    groups = _stable_groups(discovery_group_ids)
    if not msg:
        return _build_error_envelope_text(
            intent="broadcast",
            session_id=caller_session_id,
            target_agent_id=None,
            target_groups=groups or ["invalid-group"],
            message="message 不能为空",
            code="invalid_message",
            retryable=False,
            trace_id=trace_id,
        )
    if not groups:
        return _build_error_envelope_text(
            intent="broadcast",
            session_id=caller_session_id,
            target_agent_id=None,
            target_groups=["invalid-group"],
            message="discovery_group_ids 不能为空",
            code="invalid_groups",
            retryable=False,
            trace_id=trace_id,
        )
    try:
        registry_url = _require_registry_url()
        async with httpx.AsyncClient(timeout=_DEFAULT_HTTP_TIMEOUT_SECONDS) as client:
            resp = await client.post(
                f"{registry_url}/v1/broadcast",
                json={
                    "message": msg,
                    "discovery_group_ids": groups,
                    "source": "agent-peer",
                },
                headers=_a2a_auth_headers(),
            )
            resp.raise_for_status()
            result = resp.json()
        stream_timeout_seconds = float(max(1, int(s.agent_peer_broadcast_stream_timeout_seconds)))
        # 并发收集每个目标的 SSE：避免串行让快目标被慢目标饿死，整体 SLA 由 stream_timeout_seconds 兜底。
        targets: list[dict[str, Any]] = []
        for item in list(result.get("results", [])) if isinstance(result, dict) else []:
            if not isinstance(item, dict):
                continue
            agent_id = str(item.get("agent_id") or "").strip()
            base_url = str(item.get("base_url") or "").strip()
            peer_client_id = str(item.get("client_id") or "").strip()
            peer_session_id = str(item.get("session_id") or "").strip()
            if not agent_id or not base_url or not peer_client_id or not peer_session_id:
                continue
            targets.append(
                {
                    "agent_id": agent_id,
                    "base_url": base_url,
                    "client_id": peer_client_id,
                    "session_id": peer_session_id,
                }
            )

        async def _collect(t: dict[str, Any]) -> tuple[dict[str, Any], PeerStreamSummary]:
            summary = await _collect_peer_stream_summary(
                base_url=t["base_url"],
                client_id=t["client_id"],
                session_id=t["session_id"],
                timeout_seconds=stream_timeout_seconds,
            )
            return t, summary

        stream_outputs: list[dict[str, Any]] = []
        any_truncated = False
        per_target_states: list[str] = []
        if targets:
            results_per_target = await asyncio.gather(
                *(_collect(t) for t in targets),
                return_exceptions=False,
            )
            for t, summary in results_per_target:
                stream_outputs.append(
                    {
                        "agent_id": t["agent_id"],
                        "base_url": t["base_url"],
                        "client_id": t["client_id"],
                        "session_id": t["session_id"],
                        "output": summary.text,
                        "truncated": summary.truncated,
                        "final_state": summary.final_state,
                        "approvals": [item.model_dump() for item in summary.approvals],
                        "errors": list(summary.errors),
                    }
                )
                per_target_states.append(summary.final_state)
                if summary.truncated:
                    any_truncated = True
        # 聚合规则：任一目标待审批 → requires_input；全部成功 → succeeded；
        # 全部失败 → failed；混合或仍有截断 → running。
        if any(state == "requires_input" for state in per_target_states):
            aggregated: AgentPeerTaskState = "requires_input"
        elif per_target_states and all(state == "succeeded" for state in per_target_states):
            aggregated = "succeeded"
        elif per_target_states and all(state in {"failed", "truncated"} for state in per_target_states):
            aggregated = "failed" if all(state == "failed" for state in per_target_states) else "running"
        else:
            aggregated = "running"
        env = build_agent_peer_envelope(
            caller_agent_id=s.agent_id,
            caller_session_id=caller_session_id,
            caller_groups=s.discovery_groups,
            target_agent_id=None,
            target_groups=groups,
            intent="broadcast",
            payload_content={
                "ok": aggregated in {"succeeded", "requires_input", "running"},
                "broadcast_result": result,
                "stream_outputs": stream_outputs,
                "stream_timeout_seconds": stream_timeout_seconds,
                "stream_output_truncated": any_truncated,
                "aggregated_state": aggregated,
            },
            payload_content_type="application/json",
            trace_id=trace_id,
            task=AgentPeerTask(
                task_id=f"broadcast-{uuid.uuid4().hex[:10]}",
                state=aggregated,
                artifact_refs=[],
            ),
        )
        return _json_text(env.model_dump())
    except Exception as exc:
        return _build_error_envelope_text(
            intent="broadcast",
            session_id=caller_session_id,
            target_agent_id=None,
            target_groups=groups,
            message=str(exc),
            code="broadcast_failed",
            retryable=True,
            trace_id=trace_id,
        )


@tool("agent_peer_approve_tools")
async def agent_peer_approve_tools(
    target_agent_id: str,
    target_session_id: str,
    decision: str,
    approved_call_ids: list[str] | None = None,
    rejected_call_ids: list[str] | None = None,
    context: OpenAIConversationContext | None = None,
) -> str:
    """使用场景：对端 `agent_send_message`/`agent_broadcast` 返回 `approval_required` 时，提交本侧对对端 pending 工具的审批决策；不用于本机自身审批。

    字段说明：
    - `target_agent_id`：审批对端 Agent ID（必填）。
    - `target_session_id`：对端等待审批的会话 ID（必填，对应 `agent_send_message` 返回的 `payload.content.target_session_id` 或 `agent_broadcast` 中每个 `stream_outputs[i].session_id`）。
    - `decision`：审批决策，取值之一：`approve`（同意全部 pending）、`reject`（拒绝全部 pending）、`selection`（逐条决策）。
    - `approved_call_ids`：`decision=selection` 时必填非空之一；要批准的 `tool_call.id` 列表。
    - `rejected_call_ids`：`decision=selection` 时必填非空之一；要拒绝的 `tool_call.id` 列表。

    返回说明：
    - 成功：JSON 文本，`task.state` 反映对端审批后的执行终态（`succeeded` 表示工具执行完毕、`requires_input` 表示对端再次进入新一轮审批），`payload.content` 含 `stream_output/approvals/final_state`。
    - 失败：JSON 文本，`task.state="failed"`，`error.code` 标注失败类别（`invalid_decision`/`resume_failed` 等）。

    调用范例：
    - `agent_peer_approve_tools({"target_agent_id":"agent-b","target_session_id":"peer-...","decision":"approve"})`
    - `agent_peer_approve_tools({"target_agent_id":"agent-b","target_session_id":"peer-...","decision":"reject"})`
    - `agent_peer_approve_tools({"target_agent_id":"agent-b","target_session_id":"peer-...","decision":"selection","approved_call_ids":["call_1"],"rejected_call_ids":["call_2"]})`
    """

    s = get_settings()
    caller_session_id = _session_id_from_context(context, f"peer-{s.agent_id}")
    trace_id = f"trace-{uuid.uuid4().hex}"
    target_id = (target_agent_id or "").strip()
    peer_session_id = (target_session_id or "").strip()
    if not target_id:
        return _build_error_envelope_text(
            intent="delegate",
            session_id=caller_session_id,
            target_agent_id="",
            target_groups=None,
            message="target_agent_id 不能为空",
            code="invalid_target_agent",
            retryable=False,
            trace_id=trace_id,
        )
    if not peer_session_id:
        return _build_error_envelope_text(
            intent="delegate",
            session_id=caller_session_id,
            target_agent_id=target_id,
            target_groups=None,
            message="target_session_id 不能为空",
            code="invalid_target_session",
            retryable=False,
            trace_id=trace_id,
        )
    try:
        resume_value = _build_resume_value(
            decision=decision,
            approved_call_ids=approved_call_ids,
            rejected_call_ids=rejected_call_ids,
        )
    except ValueError as exc:
        return _build_error_envelope_text(
            intent="delegate",
            session_id=caller_session_id,
            target_agent_id=target_id,
            target_groups=None,
            message=str(exc),
            code="invalid_decision",
            retryable=False,
            trace_id=trace_id,
        )
    final_delivery_mode = (s.agent_peer_delivery_mode or "direct").strip().lower()
    if final_delivery_mode not in {"direct", "relay"}:
        return _build_error_envelope_text(
            intent="delegate",
            session_id=caller_session_id,
            target_agent_id=target_id,
            target_groups=None,
            message="配置 AGENT_PEER_DELIVERY_MODE 仅支持 direct 或 relay",
            code="invalid_delivery_mode_config",
            retryable=False,
            trace_id=trace_id,
        )
    try:
        target_base_url = ""
        peer_client_id = f"approve-{uuid.uuid4().hex}"
        body = {
            "session_id": peer_session_id,
            "client_id": peer_client_id,
            "request_type": "resume",
            "resume_value": resume_value,
            "source": "agent-peer-approve",
            "priority": "resume",
        }
        async with httpx.AsyncClient(timeout=_DEFAULT_HTTP_TIMEOUT_SECONDS) as client:
            if final_delivery_mode == "direct":
                target = _resolve_target_agent(target_id)
                target_base_url = str(target.get("base_url") or "").strip().rstrip("/")
                if not target_base_url:
                    raise ValueError("目标 Agent 未提供 base_url")
                resp = await client.post(f"{target_base_url}/v1/messages", json=body, headers=_a2a_auth_headers())
            else:
                registry_url = _require_registry_url()
                relay_payload = {
                    "target_agent_id": target_id,
                    "caller_groups": _stable_groups(s.discovery_groups),
                    "session_id": peer_session_id,
                    "client_id": peer_client_id,
                    "request_type": "resume",
                    "resume_value": resume_value,
                    "source": "agent-peer-approve-relay",
                    "priority": "resume",
                }
                resp = await client.post(f"{registry_url}/v1/relay", json=relay_payload, headers=_a2a_auth_headers())
            resp.raise_for_status()
            submit = resp.json()
            target_base_url = target_base_url or str(submit.get("target_base_url") or "").strip().rstrip("/")
            if not target_base_url:
                raise ValueError("目标 Agent 未提供 base_url")
        accepted = bool(submit.get("accepted", False))
        if not accepted:
            raise ValueError("目标 Agent 未确认 resume 入队（accepted=false）")
        summary = await _collect_peer_stream_summary(
            base_url=target_base_url,
            client_id=peer_client_id,
            session_id=peer_session_id,
            timeout_seconds=float(max(1, int(s.agent_peer_stream_timeout_seconds))),
        )
        env = build_agent_peer_envelope(
            caller_agent_id=s.agent_id,
            caller_session_id=caller_session_id,
            caller_groups=s.discovery_groups,
            target_agent_id=target_id,
            target_groups=None,
            intent="delegate",
            payload_content={
                "ok": summary.final_state in {"succeeded", "requires_input"},
                "target_agent_id": target_id,
                "target_base_url": target_base_url,
                "target_session_id": peer_session_id,
                "target_client_id": peer_client_id,
                "decision": resume_value,
                "submit": submit,
                "stream_output": summary.text,
                "stream_output_truncated": summary.truncated,
                "approvals": [item.model_dump() for item in summary.approvals],
                "errors": list(summary.errors),
                "final_state": summary.final_state,
            },
            payload_content_type="application/json",
            trace_id=trace_id,
            task=AgentPeerTask(
                task_id=f"peer-approve-{trace_id}",
                state=_peer_state_to_task_state(summary.final_state),
                artifact_refs=[],
            ),
            error=(
                AgentPeerError(
                    code="peer_failed",
                    message="; ".join(summary.errors)[:500] or "对端 resume 后流式处理失败",
                    retryable=True,
                )
                if summary.final_state == "failed"
                else None
            ),
        )
        return _json_text(env.model_dump())
    except Exception as exc:
        return _build_error_envelope_text(
            intent="delegate",
            session_id=caller_session_id,
            target_agent_id=target_id,
            target_groups=None,
            message=str(exc),
            code="resume_failed",
            retryable=True,
            trace_id=trace_id,
        )
