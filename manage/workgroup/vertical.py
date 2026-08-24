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
from manage.workgroup.member_tools import side_effect_for_tool
from manage.workgroup.models import AssignCreateRequest
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
        # AgentRef session/turn waiters are keyed by assign_id. They are
        # process-local only; the reliable start frame remains in the outbox.
        self._agent_waiters: dict[str, threading.Event] = {}
        self._agent_results: dict[str, dict[str, Any]] = {}
        self._turn_kernel: TurnKernel | None = None

    def set_turn_kernel(self, kernel: TurnKernel | None) -> None:
        self._turn_kernel = kernel

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
            direct_member_id=req.direct_member_id,
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

    def enqueue_agent_session_open(self, workgroup_id: str, member_id: str) -> OutboxFrame:
        """Bind a Workgroup member to an existing Node Agent session."""
        ctx = self.store.member_execution_context(member_id)
        if str(ctx.get("execution_mode") or "") != "agent_ref":
            raise WorkgroupError("conflict", "member is not an AgentRef", http_status=409)
        frame = self.store.enqueue_outbox(
            workgroup_id,
            type="agent.session.open",
            payload={
                "workgroup_id": workgroup_id,
                "member_id": member_id,
                "agent_id": str(ctx.get("agent_id") or ""),
                "session_id": str(ctx.get("session_id") or ""),
                "home_node_id": str(ctx.get("home_node_id") or ""),
            },
        )
        if self.hub is not None:
            self.hub.deliver_outbox_frame(frame, home_node_id=ctx["home_node_id"])
            self.hub.request_resume(ctx["home_node_id"], workgroup_id)
        return frame

    def enqueue_agent_turn_start(self, workgroup_id: str, assign_id: str) -> OutboxFrame:
        assign = self.store.get_assign(assign_id)
        if assign is None or assign.workgroup_id != workgroup_id:
            raise WorkgroupError("not_found", "assign not found", http_status=404)
        ctx = self.store.member_execution_context(assign.member_id)
        if str(ctx.get("execution_mode") or "") != "agent_ref":
            raise WorkgroupError("conflict", "member is not an AgentRef", http_status=409)
        frame = self.store.enqueue_outbox(
            workgroup_id,
            type="agent.turn.start",
            payload={
                "workgroup_id": workgroup_id,
                "member_id": assign.member_id,
                "agent_id": str(ctx.get("agent_id") or ""),
                "session_id": str(ctx.get("session_id") or ""),
                "assign_id": assign_id,
                "turn_id": assign.assign_id,
                "user_message": assign.instruction,
                "client_message_id": assign.leader_tool_call_id,
                "home_node_id": str(ctx.get("home_node_id") or ""),
            },
        )
        with self._lock:
            self._agent_waiters[assign_id] = threading.Event()
        if self.hub is not None:
            self.hub.deliver_outbox_frame(frame, home_node_id=ctx["home_node_id"])
            self.hub.request_resume(ctx["home_node_id"], workgroup_id)
        return frame

    def enqueue_agent_turn_resume(
        self,
        workgroup_id: str,
        hitl_id: str,
        resolution: dict[str, Any],
    ) -> OutboxFrame:
        """Return a resolved Node AgentRef HITL to its owning session."""
        hitl = self.store.get_hitl(hitl_id)
        if hitl is None or hitl.workgroup_id != workgroup_id:
            raise WorkgroupError("not_found", "hitl not found", http_status=404)
        meta = dict(hitl.metadata or {})
        if meta.get("source") != "agent_ref":
            raise WorkgroupError("conflict", "hitl is not bound to an AgentRef", http_status=409)
        required = ("member_id", "agent_id", "session_id", "assign_id", "home_node_id")
        if any(not str(meta.get(key) or "").strip() for key in required):
            raise WorkgroupError("conflict", "agent_ref hitl routing metadata is incomplete", http_status=409)
        payload = {
            "workgroup_id": workgroup_id,
            "member_id": str(meta["member_id"]),
            "agent_id": str(meta["agent_id"]),
            "session_id": str(meta["session_id"]),
            "assign_id": str(meta["assign_id"]),
            "hitl_id": str(meta.get("node_hitl_id") or ""),
            "resume_value": dict(resolution or {}),
            "home_node_id": str(meta["home_node_id"]),
        }
        frame = self.store.enqueue_outbox(workgroup_id, type="agent.turn.resume", payload=payload)
        if self.hub is not None:
            self.hub.deliver_outbox_frame(frame, home_node_id=payload["home_node_id"])
            # `deliver_outbox_frame` is the live path. Do not immediately call
            # request_resume as well: on an already connected Node that would
            # replay the same resume before the live delivery ACK and queue a
            # second continuation. The durable outbox is replayed on the next
            # reconnect, which is the only gap-fill path needed here.
        return frame

    def wait_agent_turn(
        self,
        assign_id: str,
        *,
        timeout_s: float | None = None,
        cancel_check: Any | None = None,
    ) -> dict[str, Any]:
        with self._lock:
            event = self._agent_waiters.get(assign_id)
        if event is None:
            raise WorkgroupError("conflict", "agent turn waiter not found", http_status=500)
        deadline = time.monotonic() + (self.command_timeout_s if timeout_s is None else max(0.1, float(timeout_s)))
        while True:
            if cancel_check is not None and cancel_check():
                raise WorkgroupError("canceled", "workgroup turn cancelled", http_status=409)
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                with self._lock:
                    self._agent_waiters.pop(assign_id, None)
                raise WorkgroupError("conflict", "agent turn timed out", http_status=409, retryable=True)
            if event.wait(min(0.2, remaining)):
                break
        with self._lock:
            result = dict(self._agent_results.pop(assign_id, {}) or {})
            self._agent_waiters.pop(assign_id, None)
        if not result:
            raise WorkgroupError("conflict", "agent turn result missing after wait", http_status=500)
        return result

    def run_agent_ref_assign(
        self,
        workgroup_id: str,
        assign_id: str,
        member_id: str,
        instruction: str,
        *,
        cancel_check: Any | None = None,
        timeout_s: float | None = None,
    ) -> str:
        """Run one assignment on an existing Node Agent session."""
        member = self.store.get_member(member_id)
        if member is None or member.workgroup_id != workgroup_id:
            raise WorkgroupError("not_found", "member not found", http_status=404)
        if member.execution_mode != "agent_ref" or not member.agent_id:
            raise WorkgroupError("conflict", "member is not an AgentRef", http_status=409)
        if member.status != "ready" and not (
            member.status == "busy" and member.active_assign_id == assign_id
        ):
            raise WorkgroupError("conflict", "agent session is not ready", http_status=409)
        self.enqueue_agent_turn_start(workgroup_id, assign_id)
        result = self.wait_agent_turn(assign_id, timeout_s=timeout_s, cancel_check=cancel_check)
        status = str(result.get("status") or "failed").lower()
        if status in {"canceled", "cancelled"}:
            raise WorkgroupError("canceled", "agent turn cancelled", http_status=409)
        if status not in {"succeeded", "awaiting"}:
            raise WorkgroupError(
                str(result.get("error_code") or "agent_turn_failed"),
                str(result.get("message") or "agent turn failed"),
                http_status=409,
            )
        return str(result.get("final_text") or "").strip()[:8000] or "(empty)"

    def enqueue_member_tombstone(self, workgroup_id: str, member_id: str) -> OutboxFrame:
        """Close an AgentRef session or fence a legacy member binding."""
        member = self.store.get_member(member_id)
        if member is None or member.workgroup_id != workgroup_id:
            raise WorkgroupError("not_found", "member not found", http_status=404)
        ctx = self.store.member_execution_context(member_id)
        if member.execution_mode == "agent_ref" and member.agent_id and member.session_id:
            frame_type = "agent.session.close"
            payload = {
                "workgroup_id": workgroup_id,
                "member_id": member_id,
                "agent_id": member.agent_id,
                "session_id": member.session_id,
                "home_node_id": ctx["home_node_id"],
            }
        else:
            frame_type = "workgroup.tombstone"
            payload = {
                "workgroup_id": workgroup_id,
                "member_id": member_id,
                "lease_epoch_at_archive": ctx["lease_epoch"],
            }
        frame = self.store.enqueue_outbox(
            workgroup_id,
            type=frame_type,
            payload=payload,
        )
        if self.hub is not None:
            self.hub.deliver_outbox_frame(frame, home_node_id=ctx["home_node_id"])
            self.hub.request_resume(ctx["home_node_id"], workgroup_id)
        # The bridge is only a synchronous test/local integration.  Keep the
        # older bridge contract compatible while allowing member-scoped
        # fencing when it is implemented by the bridge.
        if self.bridge is not None:
            apply_member_tombstone = getattr(self.bridge, "apply_member_tombstone", None)
            if callable(apply_member_tombstone):
                apply_member_tombstone(payload)
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
            error_code=req.error_code,
            error_message=req.message,
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
        if member.status != "ready" and not (
            member.status == "busy" and member.active_assign_id == assign.assign_id
        ):
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
            delivered = self.hub.deliver_outbox_frame(frame, home_node_id=ctx["home_node_id"])
            # 与 provision 对齐：未在线时落库，在线但游标落后时补 resume gap-fill
            if delivered is None:
                self.hub.request_resume(ctx["home_node_id"], workgroup_id)

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
                hint = ""
                home_node_id = ""
                with self._lock:
                    for meta in (self._wg_pending_commands or {}).values():
                        for cid, m in (meta or {}).items():
                            if cid == command_id:
                                home_node_id = str((m or {}).get("home_node_id") or "")
                                break
                if self.hub is not None and home_node_id:
                    conn = self.hub.get_connection(home_node_id)
                    if conn is None or not getattr(conn, "active", False):
                        hint = (
                            f"; home node {home_node_id!r} workgroup dialer not connected "
                            "(enable manage.url on that Node; not tool-approval/HITL)"
                        )
                    else:
                        hint = (
                            "; dialer connected but no tool.result "
                            "(Node may have returned session.error, or command stuck)"
                        )
                elif not home_node_id:
                    hint = "; no home_node recorded for command"
                raise WorkgroupError(
                    "conflict",
                    f"tool command timed out after {timeout:g}s{hint}",
                    http_status=409,
                    retryable=True,
                    details={"command_id": command_id, "home_node_id": home_node_id or None},
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
        """取消 Node 工具与 AgentRef turn，并唤醒对应的本地 waiter。"""
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
        self.cancel_pending_agent_turns(workgroup_id)
        return woke

    def cancel_pending_agent_turns(self, workgroup_id: str) -> list[str]:
        """通过已建立的 Node→Manage WS 取消工作组内 AgentRef turn。"""
        canceled: list[str] = []
        for assign in self.store.list_assigns(workgroup_id, active_only=True):
            ctx = self.store.member_execution_context(assign.member_id)
            if str(ctx.get("execution_mode") or "") != "agent_ref":
                continue
            payload = {
                "workgroup_id": workgroup_id,
                "member_id": assign.member_id,
                "agent_id": str(ctx.get("agent_id") or ""),
                "session_id": str(ctx.get("session_id") or ""),
                "assign_id": assign.assign_id,
                "home_node_id": str(ctx.get("home_node_id") or ""),
            }
            try:
                frame = self.store.enqueue_outbox(
                    workgroup_id,
                    type="agent.turn.cancel",
                    payload=payload,
                )
                if self.hub is not None and payload["home_node_id"]:
                    self.hub.deliver_outbox_frame(frame, home_node_id=payload["home_node_id"])
            except Exception:  # noqa: BLE001 — 本地取消仍需唤醒 waiter
                pass
            self._signal_agent_result(
                assign.assign_id,
                {
                    **payload,
                    "status": "canceled",
                    "final_text": "",
                    "error_code": "canceled",
                    "message": "agent turn canceled",
                },
            )
            canceled.append(assign.assign_id)
        return canceled

    def _signal_agent_result(self, assign_id: str, result: dict[str, Any]) -> None:
        with self._lock:
            self._agent_results[assign_id] = dict(result)
            waiter = self._agent_waiters.get(assign_id)
        if waiter is not None:
            waiter.set()

    def handle_inbound(self, node_id: str, mtype: str, payload: dict[str, Any]) -> None:
        """WS 入站业务：provision_result / tool.result → 状态机 + 唤醒 waiters。"""
        _ = node_id
        if mtype in {"agent.session.ready", "agent.session.error", "agent.session.closed"}:
            workgroup_id = str(payload.get("workgroup_id") or "").strip()
            member_id = str(payload.get("member_id") or "").strip()
            if not workgroup_id or not member_id:
                return
            member = self.store.get_member(member_id)
            if member is None or member.workgroup_id != workgroup_id or member.status == "archived":
                # An archive is authoritative. A close/ready/error response
                # from the old connection must not resurrect that member.
                return
            status = str(payload.get("status") or "error").strip().lower()
            if mtype == "agent.session.ready" and status == "ready":
                self.store.mark_member_status(member_id, "ready", workgroup_id=workgroup_id)
                if self.hub is not None:
                    self.hub.publish_realtime_event(
                        workgroup_id,
                        "agent_status",
                        {"member_id": member_id, "status": status},
                    )
            elif mtype == "agent.session.closed":
                self.store.mark_member_status(member_id, "provisioning", workgroup_id=workgroup_id)
            else:
                self.store.mark_member_status(
                    member_id,
                    "error",
                    workgroup_id=workgroup_id,
                    error_code=str(payload.get("error_code") or "agent_session_error"),
                    error_message=str(payload.get("message") or "agent session failed"),
                )
            return
        if mtype == "agent.turn.event":
            workgroup_id = str(payload.get("workgroup_id") or "").strip()
            member_id = str(payload.get("member_id") or "").strip()
            if not workgroup_id or not member_id or self.hub is None:
                return
            event_type = str(payload.get("event_type") or "").strip()
            data = dict(payload.get("data") or {})
            data.update({"mode": "member", "member_id": member_id, "assign_id": payload.get("assign_id")})
            if event_type == "hitl_required":
                assign_id = str(payload.get("assign_id") or "").strip()
                node_hitl_id = str(data.get("hitl_id") or "").strip()
                if not assign_id or not node_hitl_id:
                    return
                existing = next(
                    (
                        item
                        for item in self.store.list_hitl(workgroup_id, pending_only=True)
                        if str((item.metadata or {}).get("source") or "") == "agent_ref"
                        and str((item.metadata or {}).get("assign_id") or "") == assign_id
                        and str((item.metadata or {}).get("node_hitl_id") or "") == node_hitl_id
                    ),
                    None,
                )
                hitl = existing or self.store.create_hitl(
                    workgroup_id,
                    prompt=str(data.get("message") or "成员请求确认工具执行"),
                    metadata={
                        "source": "agent_ref",
                        "node_hitl_id": node_hitl_id,
                        "member_id": member_id,
                        "agent_id": str(payload.get("agent_id") or ""),
                        "session_id": str(payload.get("session_id") or ""),
                        "assign_id": assign_id,
                        "home_node_id": node_id,
                        "items": list(data.get("items") or []),
                    },
                )
                self.hub.publish_realtime_event(
                    workgroup_id,
                    "hitl_required",
                    {
                        "mode": "member",
                        "member_id": member_id,
                        "assign_id": assign_id,
                        "hitl_id": hitl.hitl_id,
                        "prompt": hitl.prompt,
                        "items": list((hitl.metadata or {}).get("items") or []),
                    },
                )
            elif event_type == "assistant":
                self.hub.publish_realtime_event(
                    workgroup_id,
                    "delta",
                    {**data, "text": str(data.get("content") or "")},
                )
            elif event_type in {"reasoning", "turn_state"}:
                self.hub.publish_realtime_event(workgroup_id, "status", data)
            elif event_type == "tool_call":
                self.hub.publish_realtime_event(
                    workgroup_id,
                    "status",
                    {**data, "phase": "tool", "purpose": str(data.get("tool_name") or "执行工具")},
                )
            return
        if mtype == "agent.turn.result":
            assign_id = str(payload.get("assign_id") or "").strip()
            workgroup_id = str(payload.get("workgroup_id") or "").strip()
            member_id = str(payload.get("member_id") or "").strip()
            if not assign_id or not workgroup_id or not member_id:
                return
            result = dict(payload)
            assign = self.store.get_assign(assign_id)
            if assign is not None and assign.status in {
                "failed",
                "canceled",
                "succeeded",
                "indeterminate",
            }:
                # A cancellation or a completed retry may race with a late
                # Node frame. It must not revive the durable assign or emit a
                # misleading final answer after cancellation.
                return
            if self.hub is not None:
                self.hub.publish_realtime_event(
                    workgroup_id,
                    "assistant_final",
                    {
                        "mode": "member",
                        "member_id": member_id,
                        "assign_id": assign_id,
                        "text": str(payload.get("final_text") or ""),
                        "status": str(payload.get("status") or "failed"),
                    },
                )
            self._signal_agent_result(assign_id, result)
            return
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
                    error_code=str(payload.get("error_code") or "") or None,
                    message=str(payload.get("message") or "") or None,
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
            if member.status != "ready" and not (
                member.status == "busy" and member.active_assign_id == assign_id
            ):
                raise WorkgroupError("conflict", "member not ready", http_status=409)
            if member.execution_mode == "agent_ref" and member.agent_id:
                return self.run_agent_ref_assign(
                    workgroup_id,
                    assign_id,
                    member_id,
                    instruction,
                    cancel_check=lambda: kernel._is_cancelled(workgroup_id),
                    timeout_s=timeout_s,
                )
            spec = self.store.get_spec(member_id)
            if spec is None:
                raise WorkgroupError("not_found", "member spec not found", http_status=404)

            run = self.store.get_or_create_actor_session(
                workgroup_id,
                actor_id=member_id,
                llm_profile_revision=spec.llm_profile_revision,
            )
            run = self.store.prepare_actor_session(run.run_id, assign_id=assign_id)
            try:
                kernel._append_turn_meta(workgroup_id, "member_run_ids", run.run_id)
                kernel._update_turn(workgroup_id, member_run_id=run.run_id)
            except Exception:  # noqa: BLE001
                pass
            self.store.ensure_run_history(run)
            kernel._heal_open_tool_calls(
                run.run_id,
                reason="previous member tool turn interrupted; synthetic error result",
                preserve_assign_id=assign_id,
            )
            kernel._append_session_user_message(
                run.run_id,
                content=instruction,
                assign_id=assign_id,
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
        had_waiter = self.store.has_hitl_waiter(hitl_id)
        hitl = self.store.resolve_hitl_cas(workgroup_id, hitl_id, resolution=req.resolution)
        if not had_waiter and self._turn_kernel is not None:
            self._turn_kernel.resume_resolved_hitl(hitl)
        if (hitl.metadata or {}).get("source") == "agent_ref":
            resume = dict(req.resolution or {})
            # The current Manage composer submits an answer string. Keep that
            # UI compatible while mapping the common approval words to the
            # Node-native approval protocol. Callers that already send a
            # structured selection/approve/reject value pass through intact.
            if not str(resume.get("type") or "").strip():
                answer = str(resume.get("answer") or "").strip().lower()
                if answer in {"yes", "y", "ok", "approve", "approved", "allow", "同意", "批准", "允许", "确认"}:
                    resume = {"type": "approve"}
                else:
                    resume = {"type": "reject"}
            self.enqueue_agent_turn_resume(workgroup_id, hitl.hitl_id, resume)
        return hitl

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
