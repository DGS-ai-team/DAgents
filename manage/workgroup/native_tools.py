"""Manage-native 编排工具定义与执行。"""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Callable

from manage.workgroup.errors import WorkgroupError
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
                    "(semicolon-separated LAN IPs, no port), and tool allowlist "
                    "(workspace fs/bash tools when enabled; use this to answer capability questions; "
                    "do not assign a fake probe task)."
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


def scripted_assign_completer(
    workgroup_id: str,
    assign_id: str,
    member_id: str,
    instruction: str,
    tool_call_id: str = "",
) -> str:
    """无真实 Node 工具时的同步占位：立刻成功并写 Timeline。"""
    _ = (workgroup_id, assign_id, member_id, tool_call_id)
    return f"[scripted] {instruction.strip()[:500]}"


class NativeToolDispatcher:
    def __init__(
        self,
        store: WorkGroupStore,
        *,
        leader_run_id: str,
        assign_completer: AssignCompleter | None = None,
        registry_store: Any | None = None,
    ) -> None:
        self.store = store
        self.leader_run_id = leader_run_id
        self.assign_completer = assign_completer or scripted_assign_completer
        self.registry_store = registry_store

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
                ctx = self.store.member_execution_context(m.member_id)
                spec = self.store.get_spec(m.member_id)
                description = (spec.description if spec is not None else "") or ""
                payload.append(
                    {
                        "member_id": m.member_id,
                        "display_name": m.display_name,
                        "description": description.strip(),
                        "status": m.status,
                        "home_node_id": m.home_node_id,
                        "host_ips": self._host_ips_for_node(m.home_node_id),
                        "tool_allow_names": list(ctx.get("tool_allow_names") or []),
                        "workspace_root_kind": "member_workspace",
                        "read_file_path_rule": "relative_to_member_workspace_only",
                    }
                )
            return json.dumps({"members": payload}, ensure_ascii=False)
        if name == "ask_workgroup_user":
            return self._ask_user(workgroup_id, arguments_json)
        if name == "assign_workgroup_task":
            return self._assign(workgroup_id, tool_call_id, arguments_json)
        raise WorkgroupError("invalid_tool", f"unknown manage-native tool: {name}")

    def _ask_user(self, workgroup_id: str, arguments_json: str) -> str:
        try:
            args = json.loads(arguments_json or "{}")
        except json.JSONDecodeError as exc:
            raise WorkgroupError("invalid_json", f"tool arguments: {exc}") from exc
        prompt = str(args.get("prompt") or args.get("question") or "").strip()
        if not prompt:
            raise WorkgroupError("invalid_request", "prompt required")
        hitl = self.store.create_hitl(workgroup_id, prompt=prompt)
        resolved = self.store.wait_hitl_resolved(hitl.hitl_id, timeout_s=300.0)
        resolution = dict(resolved.resolution or {})
        if resolution.get("canceled"):
            return json.dumps(
                {
                    "hitl_id": hitl.hitl_id,
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
                "hitl_id": hitl.hitl_id,
                "status": "answered",
                "answer": answer,
            },
            ensure_ascii=False,
        )

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
        member = self.store.get_member(member_id)
        display = (member.display_name if member else "") or member_id
        instruction_text = instruction.strip()
        # Supervisor 聊天态：@成员 + 完整任务正文（前端解析为提及行 + 任务卡片）
        self.store.append_timeline(
            workgroup_id,
            type="assign_started",
            actor_id="leader",
            text=f"@{display}\n{instruction_text}",
            protocol_name="leader",
            assign_id=assign.assign_id,
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
            if current is not None and current.status in {"failed", "canceled"}:
                terminal = True
                return build_assign_tool_result_content(
                    assign_id=assign.assign_id,
                    status="failed",
                    summary=current.result_summary or "cancelled by user",
                    error_code=current.error_code or "canceled",
                )
            assign = self.store.set_assign_status(
                assign.assign_id, "succeeded", result_summary=summary, error_code=None
            )
            self.store.append_timeline(
                workgroup_id,
                type="assign_finished",
                actor_id="leader",
                text="已完成",
                protocol_name="leader",
                assign_id=assign.assign_id,
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
            assign = self.store.set_assign_status(
                assign.assign_id,
                "failed",
                result_summary=exc.message,
                error_code=exc.code,
            )
            self.store.append_timeline(
                workgroup_id,
                type="assign_finished",
                actor_id="leader",
                text=f"失败：{exc.message}",
                protocol_name="leader",
                assign_id=assign.assign_id,
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
            assign = self.store.set_assign_status(
                assign.assign_id,
                "failed",
                result_summary=msg,
                error_code="conflict",
            )
            self.store.append_timeline(
                workgroup_id,
                type="assign_finished",
                actor_id="leader",
                text=f"失败：{msg}",
                protocol_name="leader",
                assign_id=assign.assign_id,
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
                    self.store.set_assign_status(
                        assign.assign_id,
                        "failed",
                        result_summary="assign interrupted before completion",
                        error_code="canceled",
                    )
                    self.store.append_timeline(
                        workgroup_id,
                        type="assign_finished",
                        actor_id="leader",
                        text="已中断",
                        protocol_name="leader",
                        assign_id=assign.assign_id,
                    )
                except Exception:  # noqa: BLE001
                    pass
