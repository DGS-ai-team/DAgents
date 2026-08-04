"""ActorRunHistory：私有合法 assistant/tool 消息序列。"""

from __future__ import annotations

import json
from typing import Any, Literal

from pydantic import BaseModel, Field


class ToolCallFunction(BaseModel):
    name: str
    arguments: str = "{}"


class ToolCall(BaseModel):
    id: str
    type: Literal["function"] = "function"
    function: ToolCallFunction


class RunHistoryMessage(BaseModel):
    """写入 ActorRunHistory 的一条合法消息（喂 LLM / 投影）。"""

    role: Literal["system", "user", "assistant", "tool"]
    content: str | None = None
    name: str | None = None
    tool_calls: list[ToolCall] | None = None
    tool_call_id: str | None = None


class ActorRunHistory(BaseModel):
    run_id: str
    workgroup_id: str
    actor_id: str
    messages: list[RunHistoryMessage] = Field(default_factory=list)
    # Timeline seq 已消费上限（投影 watermark 可与 ActorRun 同步）
    timeline_watermark_seq: int = 0


def open_tool_call_ids(messages: list[RunHistoryMessage] | list[dict[str, Any]]) -> list[str]:
    """返回尚未配齐 tool result 的 tool_call_id（按出现顺序）。"""
    pending: list[str] = []
    seen_results: set[str] = set()
    normalized = [_as_msg(m) for m in messages]
    for m in normalized:
        if m.role == "assistant" and m.tool_calls:
            for tc in m.tool_calls:
                pending.append(tc.id)
        elif m.role == "tool" and m.tool_call_id:
            seen_results.add(m.tool_call_id)
    return [cid for cid in pending if cid not in seen_results]


def missing_tool_results(
    assistant_tool_calls: list[dict[str, Any]],
    results_so_far: list[dict[str, Any]],
) -> list[str]:
    have = {str(r.get("tool_call_id") or "") for r in results_so_far}
    return [str(c.get("id") or "") for c in assistant_tool_calls if str(c.get("id") or "") not in have]


def can_invoke_llm_after_tools(
    assistant_tool_calls: list[dict[str, Any]],
    results_so_far: list[dict[str, Any]],
) -> tuple[bool, list[str]]:
    """v1：必须全部配齐才可续写模型。返回 (可续写, 仍等待的 call ids)。"""
    wait = missing_tool_results(assistant_tool_calls, results_so_far)
    return (len(wait) == 0, wait)


def extract_assign_ids_from_tool_results(messages: list[RunHistoryMessage] | list[dict[str, Any]]) -> set[str]:
    out: set[str] = set()
    for m in (_as_msg(x) for x in messages):
        if m.role != "tool" or not m.content:
            continue
        try:
            payload = json.loads(m.content)
        except (TypeError, json.JSONDecodeError):
            continue
        if isinstance(payload, dict):
            aid = str(payload.get("assign_id") or "").strip()
            if aid:
                out.add(aid)
    return out


def build_assign_tool_result_content(
    *,
    assign_id: str,
    status: str,
    summary: str,
    error_code: str | None = None,
) -> str:
    if status == "succeeded":
        body = {
            "assign_id": assign_id,
            "status": "succeeded",
            "summary": summary,
            "error_code": None,
        }
    else:
        body = {
            "assign_id": assign_id,
            "status": status,
            "summary": summary,
            "error_code": error_code or status,
        }
    return json.dumps(body, ensure_ascii=False)


def to_provider_messages(messages: list[RunHistoryMessage] | list[dict[str, Any]]) -> list[dict[str, Any]]:
    """转为 OpenAI-compatible chat messages；确保 tool.name 永远是工具函数名。"""
    out: list[dict[str, Any]] = []
    for m in (_as_msg(x) for x in messages):
        item: dict[str, Any] = {"role": m.role}
        if m.content is not None:
            item["content"] = m.content
        if m.role == "tool":
            if m.tool_call_id:
                item["tool_call_id"] = m.tool_call_id
            # 契约：role=tool 的 name 永远是工具函数名
            if m.name:
                item["name"] = m.name
        elif m.name:
            item["name"] = m.name
        if m.tool_calls:
            item["tool_calls"] = [
                {
                    "id": tc.id,
                    "type": "function",
                    "function": {"name": tc.function.name, "arguments": tc.function.arguments},
                }
                for tc in m.tool_calls
            ]
            if "content" not in item:
                item["content"] = m.content if m.content is not None else ""
        out.append(item)
    return out


def _as_msg(raw: RunHistoryMessage | dict[str, Any]) -> RunHistoryMessage:
    if isinstance(raw, RunHistoryMessage):
        return raw
    return RunHistoryMessage.model_validate(raw)
