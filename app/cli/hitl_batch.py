"""统一 hitl_required SSE 展开为 Client 队列项（对齐 Go client/internal/hitl/hitl_batch.go）。"""

from __future__ import annotations

from typing import Any

HITL_TYPE_USER_INFORMATION = "user_information"
HITL_TYPE_EXECUTE_TOOL = "execute_tool"


def _hitl_items_from_data(raw: Any) -> list[dict[str, Any]]:
    if not isinstance(raw, list):
        return []
    out: list[dict[str, Any]] = []
    for item in raw:
        if isinstance(item, dict):
            out.append(item)
    return out


def user_information_data_from_hitl_item(item: dict[str, Any]) -> dict[str, Any] | None:
    if not item:
        return None
    data: dict[str, Any] = {"display_type": "normal_text"}
    content = str(item.get("content") or "").strip()
    if content:
        data["content"] = content
    args = item.get("user_information_args")
    if isinstance(args, dict):
        data["user_information_args"] = args
    return data


def _hitl_routing_fields_from_batch(batch: dict[str, Any]) -> dict[str, Any]:
    out: dict[str, Any] = {}
    child_id = str(batch.get("child_session_id") or "").strip()
    if child_id:
        out["child_session_id"] = child_id
    scope = str(batch.get("hitl_scope") or "").strip()
    if scope:
        out["hitl_scope"] = scope
    purpose = str(batch.get("child_purpose") or "").strip()
    if purpose:
        out["child_purpose"] = purpose
    return out


def approval_data_from_hitl_batch(batch: dict[str, Any], execute_items: list[dict[str, Any]]) -> dict[str, Any] | None:
    if not execute_items:
        return None
    data: dict[str, Any] = {
        "approval_type": "execute_tool",
        "approval_args": {"tool_calls": list(execute_items)},
        "display_type": "normal_text",
        **_hitl_routing_fields_from_batch(batch),
    }
    hitl_id = str(batch.get("hitl_id") or "").strip()
    if hitl_id:
        data["approval_id"] = hitl_id
    message = str(batch.get("message") or "").strip()
    if message:
        data["message"] = message
    return data


def expand_hitl_required(data: dict[str, Any]) -> tuple[list[dict[str, Any]], dict[str, Any] | None]:
    """将 hitl_required 展开为 (user_information 队列项, approval 队列项)。"""
    routing = _hitl_routing_fields_from_batch(data)
    user_infos: list[dict[str, Any]] = []
    execute_items: list[dict[str, Any]] = []
    for item in _hitl_items_from_data(data.get("items")):
        hitl_type = str(item.get("hitl_type") or "").strip()
        if hitl_type == HITL_TYPE_USER_INFORMATION:
            ui = user_information_data_from_hitl_item(item)
            if ui:
                user_infos.append({**ui, **routing})
        elif hitl_type == HITL_TYPE_EXECUTE_TOOL:
            execute_items.append(item)
    approval = approval_data_from_hitl_batch(data, execute_items)
    return user_infos, approval
