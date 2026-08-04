"""Manage-native 编排工具定义与执行。"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Callable

from manage.workgroup.errors import WorkgroupError
from manage.workgroup.history import build_assign_tool_result_content
from manage.workgroup.models import AssignCreateRequest
from manage.workgroup.protocol_names import protocol_name_for_actor
from manage.workgroup.store import WorkGroupStore

_FIXTURE_SCHEMA = (
    Path(__file__).resolve().parents[2]
    / "docs"
    / "design"
    / "fixtures"
    / "workgroup-d05"
    / "schemas"
    / "assign_workgroup_task.openai.json"
)


def load_assign_workgroup_task_tool() -> dict[str, Any]:
    raw = json.loads(_FIXTURE_SCHEMA.read_text(encoding="utf-8"))
    # OpenAI tools 项不含 result_schema
    return {"type": raw["type"], "function": raw["function"]}


def leader_native_tools() -> list[dict[str, Any]]:
    return [
        load_assign_workgroup_task_tool(),
        {
            "type": "function",
            "function": {
                "name": "list_workgroup_members",
                "description": "List members in the current workgroup with status.",
                "parameters": {
                    "type": "object",
                    "additionalProperties": False,
                    "properties": {},
                },
            },
        },
    ]


AssignCompleter = Callable[[str, str, str, str], str]
"""(workgroup_id, assign_id, member_id, instruction) -> summary text."""


def scripted_assign_completer(
    workgroup_id: str,
    assign_id: str,
    member_id: str,
    instruction: str,
) -> str:
    """无 Member LLM 时的同步占位：立刻成功并写 Timeline。"""
    return f"[scripted] {instruction.strip()[:500]}"


class NativeToolDispatcher:
    def __init__(
        self,
        store: WorkGroupStore,
        *,
        leader_run_id: str,
        assign_completer: AssignCompleter | None = None,
    ) -> None:
        self.store = store
        self.leader_run_id = leader_run_id
        self.assign_completer = assign_completer or scripted_assign_completer

    def dispatch(self, *, workgroup_id: str, tool_name: str, tool_call_id: str, arguments_json: str) -> str:
        name = (tool_name or "").strip()
        if name == "list_workgroup_members":
            members = self.store.list_members(workgroup_id)
            payload = [
                {
                    "member_id": m.member_id,
                    "display_name": m.display_name,
                    "status": m.status,
                    "home_node_id": m.home_node_id,
                }
                for m in members
            ]
            return json.dumps({"members": payload}, ensure_ascii=False)
        if name == "assign_workgroup_task":
            return self._assign(workgroup_id, tool_call_id, arguments_json)
        raise WorkgroupError("invalid_tool", f"unknown manage-native tool: {name}")

    def _assign(self, workgroup_id: str, tool_call_id: str, arguments_json: str) -> str:
        try:
            args = json.loads(arguments_json or "{}")
        except json.JSONDecodeError as exc:
            raise WorkgroupError("invalid_json", f"tool arguments: {exc}") from exc
        member_id = str(args.get("member_id") or "").strip()
        instruction = str(args.get("instruction") or "").strip()
        if not member_id or not instruction:
            raise WorkgroupError("invalid_request", "member_id and instruction required")

        assign = self.store.create_assign(
            workgroup_id,
            AssignCreateRequest(
                member_id=member_id,
                leader_run_id=self.leader_run_id,
                leader_tool_call_id=tool_call_id,
                instruction=instruction,
            ),
        )
        self.store.set_assign_status(assign.assign_id, "running")
        try:
            summary = self.assign_completer(workgroup_id, assign.assign_id, member_id, instruction)
            assign = self.store.set_assign_status(
                assign.assign_id, "succeeded", result_summary=summary, error_code=None
            )
            self.store.append_timeline(
                workgroup_id,
                type="actor_final_text",
                actor_id=member_id,
                text=summary,
                protocol_name=protocol_name_for_actor(member_id),
                assign_id=assign.assign_id,
            )
            return build_assign_tool_result_content(
                assign_id=assign.assign_id,
                status="succeeded",
                summary=summary,
            )
        except WorkgroupError as exc:
            assign = self.store.set_assign_status(
                assign.assign_id,
                "failed",
                result_summary=exc.message,
                error_code=exc.code,
            )
            return build_assign_tool_result_content(
                assign_id=assign.assign_id,
                status="failed",
                summary=exc.message,
                error_code=exc.code,
            )
