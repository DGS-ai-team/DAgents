"""D3 纵向编排：human → assign → tool.command outbox → result → Timeline。

真实 Node 经 WS Dialer 回传；测试可注入 NodeBridge 同步闭环。
"""

from __future__ import annotations

import json
import re
import threading
import time
from typing import TYPE_CHECKING, Any, Protocol

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
from manage.workgroup.history import RunHistoryMessage
from manage.workgroup.member_tools import side_effect_for_tool
from manage.workgroup.models import ActorRunCreateRequest, AssignCreateRequest
from manage.workgroup.store import WorkGroupStore

if TYPE_CHECKING:
    from manage.workgroup.turn_kernel import TurnKernel

_DEFAULT_COMMAND_TIMEOUT_S = 60.0
_READ_FILE_TOOL = "read_file"


class NodeBridge(Protocol):
    def provision(self, payload: dict[str, Any]) -> dict[str, Any]: ...
    def execute_command(self, payload: dict[str, Any]) -> dict[str, Any]: ...
    def apply_tombstone(self, payload: dict[str, Any]) -> None: ...


def path_from_instruction(instruction: str, *, default: str = "README") -> str:
    """从 assign instruction 里启发式抽出 read_file 路径。"""
    text = (instruction or "").strip()
    if not text:
        return default
    quoted = re.search(r"[\"'`]([^\"'`]+)[\"'`]", text)
    if quoted:
        candidate = quoted.group(1).strip()
        if candidate:
            return candidate
    token = re.search(
        r"(?:^|\s)((?:[\w./\\-]+/)*README(?:\.\w+)?|[\w./\\-]+\.\w{1,8})(?:\s|$|[，。,.])",
        text,
        flags=re.IGNORECASE,
    )
    if token:
        return token.group(1)
    return default


def validate_member_workspace_path(path: str, *, tool_name: str = "read_file") -> str:
    """校验成员工作区相对路径：禁止绝对主机路径与 '..'。"""
    rel = (path or "").strip()
    if not rel:
        raise WorkgroupError("schema_mismatch", f"{tool_name} path required", http_status=400)
    normalized = rel.replace("\\", "/")
    parts = [p for p in normalized.split("/") if p not in ("", ".")]
    if any(p == ".." for p in parts):
        raise WorkgroupError(
            "not_authorized",
            f"{tool_name} path must stay inside the member workspace (no '..')",
            http_status=403,
            details={"path": rel},
        )
    if re.match(r"^[A-Za-z]:/", normalized) or normalized.startswith("//"):
        raise WorkgroupError(
            "not_authorized",
            f"member {tool_name} cannot open host absolute paths; "
            "use a path relative to the member workspace (e.g. README)",
            http_status=403,
            details={"path": rel},
        )
    if normalized.startswith("/"):
        raise WorkgroupError(
            "not_authorized",
            f"member {tool_name} cannot open absolute paths; "
            "use a path relative to the member workspace (e.g. README)",
            http_status=403,
            details={"path": rel},
        )
    return rel


def validate_member_read_path(path: str) -> str:
    """兼容旧名：校验 read_file 相对路径。"""
    return validate_member_workspace_path(path, tool_name="read_file")


_PATH_ARG_TOOLS = frozenset(
    {
        "read_file",
        "write_file",
        "grep_file",
        "search_replace",
        "show_image",
        "read_image",
    }
)
_DIR_ARG_TOOLS = frozenset({"glob_files", "grep_files"})


def sanitize_member_tool_arguments(tool_name: str, args: dict[str, Any]) -> dict[str, Any]:
    """对 path / directory / cwd 做成员工作区相对路径校验。"""
    out = dict(args)
    name = (tool_name or "").strip()
    if name in _PATH_ARG_TOOLS and "path" in out:
        out["path"] = validate_member_workspace_path(str(out.get("path") or ""), tool_name=name)
    if name in _DIR_ARG_TOOLS:
        directory = str(out.get("directory") or "").strip() or "."
        if directory not in {".", "./"}:
            validate_member_workspace_path(directory, tool_name=name)
        out["directory"] = directory
    if name == "bash_run" and "cwd" in out and out.get("cwd") is not None:
        cwd = str(out.get("cwd") or "").strip()
        if cwd and cwd not in {".", "./"}:
            out["cwd"] = validate_member_workspace_path(cwd, tool_name=name)
    return out


class VerticalLoop:
    """Manage 侧 D3 纵向闭环编排器。"""

    def __init__(
        self,
        store: WorkGroupStore,
        bridge: NodeBridge | None = None,
        hub: Any | None = None,
        *,
        command_timeout_s: float = _DEFAULT_COMMAND_TIMEOUT_S,
    ) -> None:
        self.store = store
        self.bridge = bridge
        self.hub = hub  # WorkgroupWSHub；有连接时优先推送 outbox
        self.command_timeout_s = max(0.1, float(command_timeout_s))
        self._lock = threading.Lock()
        self._command_waiters: dict[str, threading.Event] = {}
        self._command_results: dict[str, dict[str, Any]] = {}
        # workgroup_id -> command_id -> {assign_id, member_id, home_node_id}
        self._wg_pending_commands: dict[str, dict[str, dict[str, str]]] = {}

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
            # 已连接的 Home 可能尚未对本组 resume：补发 gap-fill 拉 pending
            self.hub.request_resume(ctx["home_node_id"], workgroup_id)
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

    def dispatch_tool_command_for_assign(
        self,
        workgroup_id: str,
        *,
        assign_id: str,
        member_id: str,
        tool_call_id: str,
        tool_name: str,
        arguments: dict[str, Any] | None = None,
        arguments_json: str | None = None,
    ) -> dict[str, Any]:
        """为已有 Assign 下发通用 tool.command；有 bridge 时同步执行。"""
        tool_name = (tool_name or "").strip()
        if not tool_name:
            raise WorkgroupError("schema_mismatch", "tool_name required", http_status=400)

        assign = self.store.get_assign(assign_id)
        if assign is None or assign.workgroup_id != workgroup_id:
            raise WorkgroupError("not_found", "assign not found", http_status=404)
        member = self.store.get_member(member_id)
        if member is None or member.workgroup_id != workgroup_id:
            raise WorkgroupError("not_found", "member not found", http_status=404)
        if member.status != "ready":
            raise WorkgroupError("conflict", "member not ready", http_status=409)
        ctx = self.store.member_execution_context(member_id)
        allow = {str(n) for n in (ctx.get("tool_allow_names") or [])}
        if tool_name not in allow:
            raise WorkgroupError(
                "conflict",
                f"member allowlist has no {tool_name}",
                http_status=409,
            )

        if arguments_json is None:
            args = sanitize_member_tool_arguments(tool_name, dict(arguments or {}))
            arguments_json = json.dumps(args, ensure_ascii=False, separators=(",", ":"))
        else:
            # 仍校验 path 类参数（Member LLM 传入）
            try:
                parsed = json.loads(arguments_json or "{}")
            except json.JSONDecodeError as exc:
                raise WorkgroupError("invalid_json", f"arguments_json: {exc}", http_status=400) from exc
            if not isinstance(parsed, dict):
                raise WorkgroupError("schema_mismatch", "arguments must be object", http_status=400)
            parsed = sanitize_member_tool_arguments(tool_name, parsed)
            arguments_json = json.dumps(parsed, ensure_ascii=False, separators=(",", ":"))

        cmd_id = ids.new_id("cmd")
        runtime = self.store.member_runtime(member_id)
        catalog_rev = runtime.get("tool_catalog_revision") or "rev_unknown"
        hash_payload = {
            "tool_name": tool_name,
            "arguments_json": arguments_json,
            "member_id": member_id,
            "assign_id": assign.assign_id,
            "tool_call_id": tool_call_id,
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
            "tool_call_id": tool_call_id,
            "tool_name": tool_name,
            "arguments_json": arguments_json,
            "payload_hash": payload_hash,
            "lease_id": ctx["lease_id"],
            "lease_epoch": ctx["lease_epoch"],
            "member_generation": ctx["member_generation"],
            "member_spec_digest": ctx["member_spec_digest"],
            "tool_catalog_revision": catalog_rev,
            "status": "queued",
            "side_effect_class": side_effect_for_tool(tool_name),
        }
        self._register_command_waiter(
            cmd_id,
            workgroup_id=workgroup_id,
            assign_id=assign.assign_id,
            member_id=member_id,
            home_node_id=str(ctx.get("home_node_id") or ""),
        )
        frame = self.store.enqueue_outbox(workgroup_id, type="tool.command", payload=command)
        self.store.set_assign_status(assign.assign_id, "running")
        if self.hub is not None:
            self.hub.deliver_outbox_frame(frame, home_node_id=ctx["home_node_id"])

        tool_result: dict[str, Any] | None = None
        if self.bridge is not None:
            tool_result = self.bridge.execute_command(command)
            self.apply_tool_result(
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
                "assign": self.store.get_assign(assign.assign_id) or assign,
                "command": command,
                "tool_result": tool_result,
                "outbox_seq": frame.delivery_seq,
            }
        return {
            "assign": self.store.get_assign(assign.assign_id) or assign,
            "command": command,
            "outbox_seq": frame.delivery_seq,
        }

    def dispatch_read_file_for_assign(
        self,
        workgroup_id: str,
        *,
        assign_id: str,
        member_id: str,
        tool_call_id: str,
        path: str = "README",
    ) -> dict[str, Any]:
        """测试/兼容：为已有 Assign 下发 read_file tool.command（无产品 HTTP）。"""
        return self.dispatch_tool_command_for_assign(
            workgroup_id,
            assign_id=assign_id,
            member_id=member_id,
            tool_call_id=tool_call_id,
            tool_name=_READ_FILE_TOOL,
            arguments={"path": path},
        )

    def assign_and_dispatch_read_file(
        self,
        workgroup_id: str,
        *,
        member_id: str,
        instruction: str,
        path: str = "README",
    ) -> dict[str, Any]:
        """测试辅助：创建 Assign → 下发 read_file command（不经 Member LLM；无产品 HTTP）。"""
        assign = self.store.create_assign(
            workgroup_id,
            AssignCreateRequest(member_id=member_id, instruction=instruction),
        )
        return self.dispatch_read_file_for_assign(
            workgroup_id,
            assign_id=assign.assign_id,
            member_id=member_id,
            tool_call_id="call_read_1",
            path=path,
        )

    def apply_tool_result(self, workgroup_id: str, req: ToolResultApplyRequest) -> dict[str, Any]:
        # 工具结果只进 RunHistory/assign，不进公开 Timeline 原文
        assign = self.store.get_assign(req.assign_id)
        if assign is None or assign.workgroup_id != workgroup_id:
            raise WorkgroupError("not_found", "assign not found", http_status=404)
        ignored_assign_update = False
        if assign.status in {"succeeded", "failed", "indeterminate", "canceled"}:
            # 迟到 result：仍唤醒 waiter，禁止把已终态 assign 拉回 running
            ignored_assign_update = True
        elif req.status == "indeterminate":
            assign = self.store.set_assign_status(
                req.assign_id, "indeterminate", error_code=req.error_code or "indeterminate"
            )
        elif req.status == "canceled":
            # Node 侧 cancel 回执：assign 终态由 cancel_turn / completer 写入
            ignored_assign_update = True
        else:
            # 中间工具成败保持 running；终态由 Member loop / completer 写入
            assign = self.store.set_assign_status(req.assign_id, "running")
        self._signal_command_result(
            req.command_id,
            {
                "status": req.status,
                "result_text": req.result_text or "",
                "error_code": req.error_code,
                "assign_id": req.assign_id,
                "member_id": req.member_id,
                "command_id": req.command_id,
            },
        )
        self._forget_pending_command(workgroup_id, req.command_id)
        return {
            "assign": assign,
            "leader_tool_paired": True,
            "raw_tool_on_timeline": False,
            "ignored_assign_update": ignored_assign_update,
        }

    def wait_command_result(
        self,
        command_id: str,
        *,
        timeout_s: float | None = None,
        cancel_check: Any | None = None,
    ) -> dict[str, Any]:
        """阻塞等待 Node/HTTP 回传的 tool.result（由 apply_tool_result / cancel 唤醒）。"""
        timeout = self.command_timeout_s if timeout_s is None else max(0.1, float(timeout_s))
        with self._lock:
            cached = self._command_results.get(command_id)
            if cached is not None:
                return dict(cached)
            ev = self._command_waiters.setdefault(command_id, threading.Event())
        deadline = time.monotonic() + timeout
        while True:
            if cancel_check is not None and callable(cancel_check) and cancel_check():
                self._signal_command_result(
                    command_id,
                    {
                        "status": "canceled",
                        "result_text": "",
                        "error_code": "canceled",
                        "command_id": command_id,
                    },
                )
                break
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise WorkgroupError(
                    "conflict",
                    f"tool command timed out after {timeout:g}s",
                    http_status=409,
                    retryable=True,
                    details={"command_id": command_id},
                )
            slice_s = min(0.2, remaining)
            if ev.wait(slice_s):
                break
        with self._lock:
            result = self._command_results.get(command_id)
        if result is None:
            raise WorkgroupError("conflict", "tool command result missing after wait", http_status=500)
        return dict(result)

    def cancel_pending_commands(self, workgroup_id: str) -> list[str]:
        """下发 tool.cancel（若可）+ 合成 canceled result，唤醒 wait_command_result。"""
        with self._lock:
            pending = dict(self._wg_pending_commands.get(workgroup_id) or {})
        woke: list[str] = []
        for command_id, meta in pending.items():
            with self._lock:
                if command_id in self._command_results:
                    continue
            assign_id = str((meta or {}).get("assign_id") or "")
            member_id = str((meta or {}).get("member_id") or "")
            home_node_id = str((meta or {}).get("home_node_id") or "")
            cancel_payload = {
                "command_id": command_id,
                "workgroup_id": workgroup_id,
                "assign_id": assign_id,
                "member_id": member_id,
                "status": "canceled",
                "error_code": "canceled",
            }
            try:
                frame = self.store.enqueue_outbox(
                    workgroup_id, type="tool.cancel", payload=cancel_payload
                )
                if self.hub is not None and home_node_id:
                    self.hub.deliver_outbox_frame(frame, home_node_id=home_node_id)
            except Exception:  # noqa: BLE001 — 仍要唤醒本地 waiter
                pass
            self._signal_command_result(
                command_id,
                {
                    "status": "canceled",
                    "result_text": "",
                    "error_code": "canceled",
                    "command_id": command_id,
                    "assign_id": assign_id,
                    "member_id": member_id,
                },
            )
            woke.append(command_id)
        with self._lock:
            self._wg_pending_commands.pop(workgroup_id, None)
        return woke

    def handle_inbound(self, node_id: str, mtype: str, payload: dict[str, Any]) -> None:
        """WS 入站业务：provision_result / tool.result → 状态机 + 唤醒 waiters。"""
        _ = node_id
        if mtype == "member.provision_result":
            workgroup_id = str(payload.get("workgroup_id") or "").strip()
            member_id = str(payload.get("member_id") or "").strip()
            provision_id = str(payload.get("provision_id") or "").strip()
            if not workgroup_id or not member_id or not provision_id:
                return
            status_raw = str(payload.get("status") or "ready").strip().lower()
            status = "ready" if status_raw in {"ready", "ok", "succeeded"} else "error"
            self.complete_provision(
                workgroup_id,
                ProvisionCompleteRequest(
                    member_id=member_id,
                    provision_id=provision_id,
                    workspace_path=str(payload.get("workspace_path") or ""),
                    tool_catalog_revision=str(payload.get("tool_catalog_revision") or ""),
                    status=status,
                ),
            )
            return
        if mtype == "tool.result":
            workgroup_id = str(payload.get("workgroup_id") or "").strip()
            command_id = str(payload.get("command_id") or "").strip()
            assign_id = str(payload.get("assign_id") or "").strip()
            member_id = str(payload.get("member_id") or "").strip()
            if not workgroup_id or not command_id or not assign_id or not member_id:
                return
            status_raw = str(payload.get("status") or "failed").strip().lower()
            if status_raw not in {"succeeded", "failed", "indeterminate", "rejected", "canceled"}:
                status_raw = "failed"
            self.apply_tool_result(
                workgroup_id,
                ToolResultApplyRequest(
                    command_id=command_id,
                    assign_id=assign_id,
                    member_id=member_id,
                    status=status_raw,  # type: ignore[arg-type]
                    result_text=str(payload.get("result_text") or ""),
                    error_code=payload.get("error_code"),
                ),
            )

    def make_assign_completer(self, kernel: TurnKernel, *, timeout_s: float | None = None):
        """供 Leader `assign_workgroup_task`：创建 Member ActorRun 并跑 LLM loop。"""

        def tool_runner(
            workgroup_id: str,
            assign_id: str,
            member_id: str,
            tool_name: str,
            tool_call_id: str,
            arguments_json: str,
        ) -> str:
            dispatched = self.dispatch_tool_command_for_assign(
                workgroup_id,
                assign_id=assign_id,
                member_id=member_id,
                tool_call_id=tool_call_id or "call_member_tool",
                tool_name=tool_name,
                arguments_json=arguments_json,
            )
            tool_result = dispatched.get("tool_result")
            if tool_result is None:
                cmd_id = str(dispatched["command"]["command_id"])
                tool_result = self.wait_command_result(
                    cmd_id,
                    timeout_s=timeout_s,
                    cancel_check=lambda: kernel._is_cancelled(workgroup_id),
                )
            status = str(tool_result.get("status") or "failed")
            err_code = str(tool_result.get("error_code") or "")
            if status == "canceled" or err_code == "canceled" or kernel._is_cancelled(workgroup_id):
                raise WorkgroupError("canceled", "workgroup turn cancelled", http_status=409)
            text = str(tool_result.get("result_text") or "").strip()
            if status != "succeeded":
                err = err_code or status
                # 错误路径保留上限，避免异常爆炸；成功路径交给 kernel package_tool_result
                return f"ERROR ({status}): {err}: {text}"[:16000]
            return text or f"({tool_name}: empty)"

        def completer(
            workgroup_id: str,
            assign_id: str,
            member_id: str,
            instruction: str,
            tool_call_id: str = "",
        ) -> str:
            _ = tool_call_id
            kernel._raise_if_cancelled(workgroup_id)
            assign = self.store.get_assign(assign_id)
            if assign is None or assign.workgroup_id != workgroup_id:
                raise WorkgroupError("not_found", "assign not found", http_status=404)
            if assign.status in {"failed", "canceled", "succeeded", "indeterminate"}:
                raise WorkgroupError(
                    assign.error_code or "canceled",
                    assign.result_summary or "assign already finished",
                    http_status=409,
                )
            member = self.store.get_member(member_id)
            if member is None or member.workgroup_id != workgroup_id:
                raise WorkgroupError("not_found", "member not found", http_status=404)
            if member.status != "ready":
                raise WorkgroupError("conflict", "member not ready", http_status=409)
            spec = self.store.get_spec(member_id)
            if spec is None:
                raise WorkgroupError("not_found", "member spec not found", http_status=404)

            run = self.store.create_actor_run(
                workgroup_id,
                ActorRunCreateRequest(
                    actor_id=member_id,
                    assign_id=assign_id,
                    llm_profile_revision=spec.llm_profile_revision,
                ),
            )
            try:
                kernel._update_turn(workgroup_id, member_run_id=run.run_id)
            except Exception:  # noqa: BLE001
                pass
            self.store.ensure_run_history(run)
            self.store.append_run_history(
                run.run_id,
                [RunHistoryMessage(role="user", content=(instruction or "").strip() or "(empty)")],
            )
            out = kernel.run_member_until_idle(
                workgroup_id,
                run.run_id,
                tool_runner=tool_runner,
            )
            kernel._raise_if_cancelled(workgroup_id)
            current = self.store.get_assign(assign_id)
            if current is not None and current.status in {"failed", "canceled"}:
                raise WorkgroupError(
                    current.error_code or "canceled",
                    current.result_summary or "assign cancelled",
                    http_status=409,
                )
            text = str(out.get("final_text") or "").strip() or "(empty)"
            return text[:8000]

        return completer

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

    def _register_command_waiter(
        self,
        command_id: str,
        *,
        workgroup_id: str = "",
        assign_id: str = "",
        member_id: str = "",
        home_node_id: str = "",
    ) -> None:
        with self._lock:
            self._command_waiters.setdefault(command_id, threading.Event())
            wg = str(workgroup_id or "").strip()
            if wg:
                self._wg_pending_commands.setdefault(wg, {})[command_id] = {
                    "assign_id": str(assign_id or ""),
                    "member_id": str(member_id or ""),
                    "home_node_id": str(home_node_id or ""),
                }

    def _forget_pending_command(self, workgroup_id: str, command_id: str) -> None:
        with self._lock:
            pending = self._wg_pending_commands.get(workgroup_id)
            if not pending:
                return
            pending.pop(command_id, None)
            if not pending:
                self._wg_pending_commands.pop(workgroup_id, None)

    def _signal_command_result(self, command_id: str, result: dict[str, Any]) -> None:
        with self._lock:
            self._command_results[command_id] = dict(result)
            ev = self._command_waiters.get(command_id)
            if ev is None:
                ev = threading.Event()
                self._command_waiters[command_id] = ev
            ev.set()
