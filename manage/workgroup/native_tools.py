"""Manage-native 编排工具定义与执行。"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Callable

from manage.workgroup.errors import WorkgroupError
from manage.workgroup.assignment_service import AssignmentService
from manage.workgroup.d3_models import HITLRequest
from manage.workgroup.history import build_assign_tool_result_content
from manage.workgroup.member_tools import CALL_PURPOSE_KEY
from manage.workgroup.models import AssignCreateRequest
from manage.workgroup.protocol_names import protocol_name_for_actor
from manage.workgroup.store import WorkGroupStore

_ASSIGN_TOOL_SCHEMA = (
    Path(__file__).resolve().parent
    / "schemas"
    / "assign_workgroup_task.openai.json"
)
_DEFAULT_HITL_TIMEOUT_S = 10 * 60.0


def load_assign_workgroup_task_tool() -> dict[str, Any]:
    raw = json.loads(_ASSIGN_TOOL_SCHEMA.read_text(encoding="utf-8"))
    # OpenAI tools 项不含 result_schema
    return {"type": raw["type"], "function": raw["function"]}


def leader_native_tools() -> list[dict[str, Any]]:
    return [
        load_assign_workgroup_task_tool(),
        {
            "type": "function",
            "function": {
                "name": "list_workgroup_members",
                "description": (
                    "List members with short description, status, home node host_ips "
                    "(semicolon-separated LAN IPs, no port); use this to answer member "
                    "availability and environment questions; do not assign a fake probe task."
                ),
                "parameters": {
                    "type": "object",
                    "additionalProperties": False,
                    "properties": {
                        CALL_PURPOSE_KEY: {
                            "type": "string",
                            "description": "Required. Briefly explain the purpose of this tool call; shown in the workgroup progress UI.",
                        },
                    },
                    "required": [CALL_PURPOSE_KEY],
                },
            },
        },
        {
            "type": "function",
            "function": {
                "name": "ask_workgroup_user",
                "description": (
                    "Ask the human user a clarifying question and wait for their answer "
                    "before continuing. Use when the task is ambiguous or needs a decision."
                ),
                "parameters": {
                    "type": "object",
                    "additionalProperties": False,
                    "properties": {
                        "prompt": {
                            "type": "string",
                            "description": "Question shown to the user.",
                        },
                        CALL_PURPOSE_KEY: {
                            "type": "string",
                            "description": "Required. Briefly explain the purpose of this tool call; shown in the workgroup progress UI.",
                        },
                    },
                    "required": [CALL_PURPOSE_KEY, "prompt"],
                },
            },
        },
    ]


AssignCompleter = Callable[..., str]
"""(workgroup_id, assign_id, member_id, instruction, tool_call_id='') -> summary text."""


def format_hitl_resolution(resolved: HITLRequest) -> str:
    """Encode a resolved HITL exactly as the native tool would return it."""
    resolution = dict(resolved.resolution or {})
    if resolution.get("canceled"):
        return json.dumps(
            {
                "hitl_id": resolved.hitl_id,
                "status": "canceled",
                "answer": "",
            },
            ensure_ascii=False,
        )
    answer = str(resolution.get("answer") or "").strip()
    if not answer and resolution:
        answer = json.dumps(resolution, ensure_ascii=False)
    return json.dumps(
        {
            "hitl_id": resolved.hitl_id,
            "status": "answered",
            "answer": answer,
        },
        ensure_ascii=False,
    )


class NativeToolDispatcher:
    def __init__(
        self,
        store: WorkGroupStore,
        *,
        leader_run_id: str,
        assign_completer: AssignCompleter | None = None,
        assignment_service: AssignmentService | None = None,
        registry_store: Any | None = None,
        on_hitl_created: Callable[[HITLRequest], None] | None = None,
        on_hitl_resolved: Callable[[HITLRequest], None] | None = None,
    ) -> None:
        self.store = store
        self.leader_run_id = leader_run_id
        self.assign_completer = assign_completer
        self.assignments = assignment_service or AssignmentService(store)
        self.registry_store = registry_store
        self.on_hitl_created = on_hitl_created
        self.on_hitl_resolved = on_hitl_resolved

    def _host_ips_for_node(self, home_node_id: str) -> str:
        node_id = (home_node_id or "").strip()
        if not node_id or self.registry_store is None:
            return ""
        getter = getattr(self.registry_store, "get", None)
        if not callable(getter):
            return ""
        rec = getter(node_id)
        if rec is None:
            return ""
        return str(getattr(rec, "host_ips", "") or "").strip()

    def dispatch(self, *, workgroup_id: str, tool_name: str, tool_call_id: str, arguments_json: str) -> str:
        name = (tool_name or "").strip()
        if name == "list_workgroup_members":
            members = self.store.list_members(workgroup_id)
            payload = []
            for m in members:
                payload.append(
                    {
                        "member_id": m.member_id,
                        "display_name": m.display_name,
                        "description": m.description.strip(),
                        "status": m.status,
                        "home_node_id": m.home_node_id,
                        "host_ips": self._host_ips_for_node(m.home_node_id),
                        "workspace_root_kind": "member_workspace",
                        "read_file_path_rule": "relative_to_member_workspace_only",
                    }
                )
            return json.dumps({"members": payload}, ensure_ascii=False)
        if name == "ask_workgroup_user":
            return self._ask_user(workgroup_id, arguments_json, tool_call_id=tool_call_id)
        if name == "assign_workgroup_task":
            return self._assign(workgroup_id, tool_call_id, arguments_json)
        raise WorkgroupError("invalid_tool", f"unknown manage-native tool: {name}")

    def _ask_user(self, workgroup_id: str, arguments_json: str, *, tool_call_id: str = "") -> str:
        try:
            args = json.loads(arguments_json or "{}")
        except json.JSONDecodeError as exc:
            raise WorkgroupError("invalid_json", f"tool arguments: {exc}") from exc
        prompt = str(args.get("prompt") or args.get("question") or "").strip()
        if not prompt:
            raise WorkgroupError("invalid_request", "prompt required")
        hitl = self.store.create_hitl(
            workgroup_id,
            prompt=prompt,
            kind="user_question",
            run_id=self.leader_run_id,
            tool_call_id=tool_call_id,
            reserve_waiter=True,
        )
        if self.on_hitl_created is not None:
            self.on_hitl_created(hitl)
        resolved = self.store.wait_hitl_resolved(hitl.hitl_id, timeout_s=_DEFAULT_HITL_TIMEOUT_S)
        if self.on_hitl_resolved is not None:
            self.on_hitl_resolved(resolved)
        return format_hitl_resolution(resolved)

    def _assign(self, workgroup_id: str, tool_call_id: str, arguments_json: str) -> str:
        try:
            args = json.loads(arguments_json or "{}")
        except json.JSONDecodeError as exc:
            raise WorkgroupError("invalid_json", f"tool arguments: {exc}") from exc
        member_id = str(args.get("member_id") or "").strip()
        instruction = str(args.get("instruction") or "").strip()
        if not member_id or not instruction:
            raise WorkgroupError("invalid_request", "member_id and instruction required")
        if self.assign_completer is None:
            raise WorkgroupError(
                "conflict",
                "assign completer not configured",
                http_status=500,
            )

        member = self.store.get_member(member_id)
        display = (member.display_name if member else "") or member_id
        assign = self.assignments.create(
            workgroup_id,
            AssignCreateRequest(
                member_id=member_id,
                leader_run_id=self.leader_run_id,
                leader_tool_call_id=tool_call_id,
                source="leader_tool",
                instruction=instruction,
            ),
            actor_id="leader",
            started_text=f"@{display}\n{instruction.strip()}",
        )
        terminal = False
        try:
            summary = self.assign_completer(
                workgroup_id,
                assign.assign_id,
                member_id,
                instruction,
                tool_call_id,
            )
            current = self.store.get_assign(assign.assign_id)
            if current is not None and current.status in {"succeeded", "failed", "canceled", "indeterminate"}:
                terminal = True
                return build_assign_tool_result_content(
                    assign_id=assign.assign_id,
                    status=current.status,
                    summary=current.result_summary or ("cancelled by user" if current.status == "canceled" else "assign finished"),
                    error_code=current.error_code,
                )
            assign = self.assignments.finish(
                workgroup_id,
                assign.assign_id,
                status="succeeded",
                summary=summary,
                actor_id="leader",
                text="已完成",
            )
            # Member LLM loop 已写 Timeline final；scripted completer 未写时在此补一条
            already = any(
                e.type == "actor_final_text"
                and e.actor_id == member_id
                and e.assign_id == assign.assign_id
                for e in self.store.list_timeline(workgroup_id)
            )
            if not already:
                self.store.append_timeline(
                    workgroup_id,
                    type="actor_final_text",
                    actor_id=member_id,
                    text=summary,
                    protocol_name=protocol_name_for_actor(member_id),
                    assign_id=assign.assign_id,
                )
            terminal = True
            return build_assign_tool_result_content(
                assign_id=assign.assign_id,
                status="succeeded",
                summary=summary,
            )
        except WorkgroupError as exc:
            current = self.store.get_assign(assign.assign_id)
            if current is not None and current.status in {"succeeded", "failed", "canceled", "indeterminate"}:
                terminal = True
                return build_assign_tool_result_content(
                    assign_id=assign.assign_id,
                    status=current.status,
                    summary=current.result_summary or exc.message,
                    error_code=current.error_code or (exc.code if current.status != "succeeded" else None),
                )
            assign = self.assignments.finish(
                workgroup_id,
                assign.assign_id,
                status="failed",
                summary=exc.message,
                error_code=exc.code,
                actor_id="leader",
                text=f"失败：{exc.message}",
            )
            terminal = True
            return build_assign_tool_result_content(
                assign_id=assign.assign_id,
                status="failed",
                summary=exc.message,
                error_code=exc.code,
            )
        except Exception as exc:  # noqa: BLE001 — 必须释放 active assign
            msg = str(exc) or exc.__class__.__name__
            current = self.store.get_assign(assign.assign_id)
            if current is not None and current.status in {"succeeded", "failed", "canceled", "indeterminate"}:
                terminal = True
                return build_assign_tool_result_content(
                    assign_id=assign.assign_id,
                    status=current.status,
                    summary=current.result_summary or msg,
                    error_code=current.error_code or ("conflict" if current.status != "succeeded" else None),
                )
            assign = self.assignments.finish(
                workgroup_id,
                assign.assign_id,
                status="failed",
                summary=msg,
                error_code="conflict",
                actor_id="leader",
                text=f"失败：{msg}",
            )
            terminal = True
            return build_assign_tool_result_content(
                assign_id=assign.assign_id,
                status="failed",
                summary=msg,
                error_code="conflict",
            )
        finally:
            if not terminal:
                try:
                    self.assignments.finish(
                        workgroup_id,
                        assign.assign_id,
                        status="failed",
                        summary="assign interrupted before completion",
                        error_code="canceled",
                        actor_id="leader",
                        text="已中断",
                    )
                except Exception:  # noqa: BLE001
                    pass
