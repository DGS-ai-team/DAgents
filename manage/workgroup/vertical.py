"""D3 纵向编排：human → assign → tool.command outbox → result → Timeline。

真实 Node WS 拨号仍可延后；通过可注入的 NodeBridge 完成闭环。
"""

from __future__ import annotations

from typing import Any, Protocol

from manage.workgroup import ids
from manage.workgroup.d3_models import (
    HITLCreateRequest,
    HITLRequest,
    HITLResolveRequest,
    HumanPostRequest,
    MemberFinalRequest,
    OutboxFrame,
    ProvisionCompleteRequest,
    TimelineEvent,
    ToolResultApplyRequest,
)
from manage.workgroup.digest import sha256_digest
from manage.workgroup.errors import WorkgroupError
from manage.workgroup.models import Assign, AssignCreateRequest
from manage.workgroup.store import WorkGroupStore, _now


class NodeBridge(Protocol):
    def provision(self, payload: dict[str, Any]) -> dict[str, Any]: ...
    def execute_command(self, payload: dict[str, Any]) -> dict[str, Any]: ...
    def apply_tombstone(self, payload: dict[str, Any]) -> None: ...


class VerticalLoop:
    """Manage 侧 D3 纵向闭环编排器。"""

    def __init__(
        self,
        store: WorkGroupStore,
        bridge: NodeBridge | None = None,
        hub: Any | None = None,
    ) -> None:
        self.store = store
        self.bridge = bridge
        self.hub = hub  # WorkgroupWSHub；有连接时优先推送 outbox

    # --- Timeline / Outbox / HITL 委托 store ---

    def post_human(self, workgroup_id: str, req: HumanPostRequest) -> TimelineEvent:
        self.store.assert_acl_member(workgroup_id, req.from_node_id)
        self.store.require_active(workgroup_id)
        return self.store.append_timeline(
            workgroup_id,
            type="human_message",
            actor_id=req.from_node_id,
            text=req.text,
            client_message_id=req.client_message_id,
        )

    def enqueue_provision(self, workgroup_id: str, member_id: str) -> OutboxFrame:
        ctx = self.store.member_execution_context(member_id)
        frame = self.store.enqueue_outbox(
            workgroup_id,
            type="member.provision",
            payload={
                "provision_id": ids.new_id("pv"),
                "workgroup_id": workgroup_id,
                "member_id": member_id,
                "home_node_id": ctx["home_node_id"],
                "member_spec_digest": ctx["member_spec_digest"],
                "lease_epoch": ctx["lease_epoch"],
                "member_generation": ctx["member_generation"],
                "tool_allow_names": list(ctx["tool_allow_names"]),
            },
        )
        if self.hub is not None:
            self.hub.deliver_outbox_frame(frame, home_node_id=ctx["home_node_id"])
        if self.bridge is not None:
            result = self.bridge.provision(frame.payload)
            self.complete_provision(
                workgroup_id,
                ProvisionCompleteRequest(
                    member_id=member_id,
                    provision_id=frame.payload["provision_id"],
                    workspace_path=str(result.get("workspace_path") or ""),
                    tool_catalog_revision=str(result.get("tool_catalog_revision") or ""),
                    status="ready" if result.get("ok", True) else "error",
                ),
            )
            self.store.ack_outbox(workgroup_id, frame.delivery_seq)
        return frame

    def complete_provision(self, workgroup_id: str, req: ProvisionCompleteRequest) -> dict[str, Any]:
        member = self.store.mark_member_status(
            req.member_id,
            "ready" if req.status == "ready" else "error",
            workgroup_id=workgroup_id,
            workspace_path=req.workspace_path,
            tool_catalog_revision=req.tool_catalog_revision,
            provision_id=req.provision_id,
        )
        return {"member": member, "provision_id": req.provision_id}

    def assign_and_dispatch_read_file(
        self,
        workgroup_id: str,
        *,
        member_id: str,
        instruction: str,
        path: str = "README",
    ) -> dict[str, Any]:
        """脚本化 Member 循环：创建 Assign → 下发 read_file command →（可选）经 bridge 执行。"""
        assign = self.store.create_assign(
            workgroup_id,
            AssignCreateRequest(member_id=member_id, instruction=instruction),
        )
        member = self.store.get_member(member_id)
        if member is None or member.status != "ready":
            raise WorkgroupError("conflict", "member not ready", http_status=409)
        ctx = self.store.member_execution_context(member_id)

        cmd_id = ids.new_id("cmd")
        args = {"path": path}
        import json

        runtime = self.store.member_runtime(member_id)
        catalog_rev = runtime.get("tool_catalog_revision") or "rev_unknown"
        args_json = json.dumps(args, ensure_ascii=False, separators=(",", ":"))
        hash_payload = {
            "tool_name": "read_file",
            "arguments_json": args_json,
            "member_id": member_id,
            "assign_id": assign.assign_id,
            "tool_call_id": "call_read_1",
            "member_spec_digest": ctx["member_spec_digest"],
            "member_generation": ctx["member_generation"],
            "lease_epoch": ctx["lease_epoch"],
            "tool_catalog_revision": catalog_rev,
        }
        payload_hash = sha256_digest(hash_payload)
        command = {
            "command_id": cmd_id,
            "workgroup_id": workgroup_id,
            "member_id": member_id,
            "assign_id": assign.assign_id,
            "run_id": assign.leader_run_id,
            "turn_id": ids.new_id("tn"),
            "tool_call_id": "call_read_1",
            "tool_name": "read_file",
            "arguments_json": args_json,
            "payload_hash": payload_hash,
            "lease_id": ctx["lease_id"],
            "lease_epoch": ctx["lease_epoch"],
            "member_generation": ctx["member_generation"],
            "member_spec_digest": ctx["member_spec_digest"],
            "tool_catalog_revision": catalog_rev,
            "status": "queued",
            "side_effect_class": "fs_read",
        }
        frame = self.store.enqueue_outbox(workgroup_id, type="tool.command", payload=command)
        self.store.set_assign_status(assign.assign_id, "running")
        if self.hub is not None:
            self.hub.deliver_outbox_frame(frame, home_node_id=ctx["home_node_id"])

        tool_result: dict[str, Any] | None = None
        if self.bridge is not None:
            tool_result = self.bridge.execute_command(command)
            apply = self.apply_tool_result(
                workgroup_id,
                ToolResultApplyRequest(
                    command_id=cmd_id,
                    assign_id=assign.assign_id,
                    member_id=member_id,
                    status=tool_result.get("status", "succeeded"),
                    result_text=str(tool_result.get("result_text") or ""),
                    error_code=tool_result.get("error_code"),
                ),
            )
            self.store.ack_outbox(workgroup_id, frame.delivery_seq)
            return {
                "assign": apply["assign"],
                "command": command,
                "tool_result": tool_result,
                "outbox_seq": frame.delivery_seq,
            }
        return {"assign": assign, "command": command, "outbox_seq": frame.delivery_seq}

    def apply_tool_result(self, workgroup_id: str, req: ToolResultApplyRequest) -> dict[str, Any]:
        # 工具结果只进 RunHistory/assign，不进公开 Timeline 原文
        assign = self.store.get_assign(req.assign_id)
        if assign is None or assign.workgroup_id != workgroup_id:
            raise WorkgroupError("not_found", "assign not found", http_status=404)
        if req.status == "indeterminate":
            assign = self.store.set_assign_status(
                req.assign_id, "indeterminate", error_code=req.error_code or "indeterminate"
            )
        elif req.status == "succeeded":
            assign = self.store.set_assign_status(req.assign_id, "running")
        else:
            assign = self.store.set_assign_status(
                req.assign_id, "failed", error_code=req.error_code or "conflict"
            )
        return {"assign": assign, "leader_tool_paired": True, "raw_tool_on_timeline": False}

    def member_final(self, workgroup_id: str, req: MemberFinalRequest) -> dict[str, Any]:
        assign = self.store.set_assign_status(
            req.assign_id, "succeeded", result_summary=req.text, error_code=None
        )
        event = self.store.append_timeline(
            workgroup_id,
            type="actor_final_text",
            actor_id=req.member_id,
            text=req.text,
            assign_id=req.assign_id,
        )
        return {"assign": assign, "timeline_event": event}

    def create_info_hitl(self, workgroup_id: str, req: HITLCreateRequest) -> HITLRequest:
        return self.store.create_hitl(workgroup_id, prompt=req.prompt)

    def resolve_info_hitl(self, workgroup_id: str, hitl_id: str, req: HITLResolveRequest) -> HITLRequest:
        return self.store.resolve_hitl_cas(workgroup_id, hitl_id, resolution=req.resolution)

    def archive_with_tombstone(self, workgroup_id: str) -> dict[str, Any]:
        group = self.store.begin_archive(workgroup_id)
        if group.status == "archiving":
            group = self.store.begin_archive(workgroup_id)  # → archived
        epoch = self.store.bump_lease_epochs(workgroup_id)
        tombstone = {
            "workgroup_id": workgroup_id,
            "lease_epoch_at_archive": epoch,
        }
        frame = self.store.enqueue_outbox(workgroup_id, type="workgroup.tombstone", payload=tombstone)
        if self.bridge is not None:
            self.bridge.apply_tombstone(tombstone)
            self.store.ack_outbox(workgroup_id, frame.delivery_seq)
        return {"workgroup": group, "tombstone": tombstone, "outbox_seq": frame.delivery_seq}

    def reconcile_missing_journal(
        self,
        workgroup_id: str,
        *,
        assign_id: str,
        command_id: str,
        member_id: str,
        side_effect_started: bool,
    ) -> dict[str, Any]:
        """Node 在 accepted 后丢失 journal：禁止自动重执行，标 indeterminate。"""
        if not side_effect_started:
            raise WorkgroupError(
                "conflict",
                "journal intact recovery should re-drive accepted command on Node",
                http_status=409,
            )
        apply = self.apply_tool_result(
            workgroup_id,
            ToolResultApplyRequest(
                command_id=command_id,
                assign_id=assign_id,
                member_id=member_id,
                status="indeterminate",
                error_code="indeterminate",
            ),
        )
        return {**apply, "auto_reexec": False, "status": "indeterminate"}
