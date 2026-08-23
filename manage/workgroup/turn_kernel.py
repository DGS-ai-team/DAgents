"""Manage Turn Kernel：Leader / Member LLM loop + Assign / Projector / HITL 门禁。"""

from __future__ import annotations

import json
import threading
import time
from concurrent.futures import Future, ThreadPoolExecutor
from collections.abc import Callable
from datetime import datetime
from typing import Any

from manage.llm.store import LLMConfigStore
from manage.workgroup.builtin_hooks import (
    TODAY_DATE_MESSAGE_NAME,
    ensure_today_date_in_messages,
    format_today_date_message,
    package_tool_result,
)
from manage.workgroup.context_compression import (
    DEFAULT_CONTEXT_COMPRESSION_BLOCKING_TRIGGER_TOKENS,
    DEFAULT_CONTEXT_COMPRESSION_KEEP_TOKENS,
    DEFAULT_CONTEXT_COMPRESSION_TRIGGER_TOKENS,
    build_compression_plan,
    build_summary_request,
    make_snapshot,
    snapshot_is_current,
)
from manage.workgroup.d3_models import HITLRequest, TurnCheckpoint
from manage.workgroup.errors import WorkgroupError
from manage.workgroup.human_queue import QueuedHuman
from manage.workgroup import ids as wg_ids
from manage.workgroup.history import (
    RunHistoryMessage,
    ToolCall,
    ToolCallFunction,
    can_invoke_llm_after_tools,
    open_tool_call_ids,
)
from manage.workgroup.llm_chat import ChatResult, ChatToolCall, LLMChatClient, resolve_chat_client
from manage.workgroup.member_tools import (
    build_member_system_prompt,
    call_purpose_from_arguments,
    host_env_from_registry,
    member_openai_tools,
    purpose_for_tool,
)
from manage.workgroup.mentions import resolve_direct_member
from manage.workgroup.models import (
    ActorRun,
    Assign,
    AssignCreateRequest,
    WorkGroup,
)
from manage.workgroup.native_tools import (
    AssignCompleter,
    NativeToolDispatcher,
    format_hitl_resolution,
    leader_native_tools,
)
from manage.workgroup.projector import project_actor_context
from manage.workgroup.protocol_names import protocol_name_for_actor
from manage.workgroup.store import WorkGroupStore


_DEFAULT_MAX_TOOL_LOOPS = 16
_MAX_PARALLEL_MEMBER_ASSIGNMENTS = 8

_TOOL_LOOP_LIMIT_EXCEEDED_MESSAGE = (
    "已超过单轮工具调用次数，请先给出当前结论以及进度，"
    "询问用户是否要继续后续的推进，下一轮开始时工具累计次数会重置。"
)

_LEADER_TOOL_PURPOSE = {
    "list_workgroup_members": "查看成员",
    "assign_workgroup_task": "分派成员任务",
    "ask_workgroup_user": "询问用户",
}


def leader_tool_purpose(tool_name: str) -> str:
    """Return a parameter-free purpose for a Supervisor progress bubble."""
    return _LEADER_TOOL_PURPOSE.get((tool_name or "").strip(), "协调工作组")

_LEADER_SYSTEM_RULES = (
    "你是工作组 Leader（Supervisor）。"
    "你只通过 Manage 侧编排工具进行协调，绝不亲自执行 shell / 文件系统 / 浏览器操作。"
    "用 list_workgroup_members 查看成员状态与工具白名单。"
    "用 assign_workgroup_task 把实际工作委派给就绪成员；"
    "成员会跑自己的 LLM 循环，并调用自己的工具完成你发布的任务。"
    "指令写清楚；不要编造宿主机绝对路径——"
    "发布任务时，请写清楚任务内容、注意事项，以及结论的结构要求；"
    "如果你有任务的路径要求，也一并写清楚，特别是成员过去没有成功执行的任务。"
    "成员沙箱只允许工作区相对路径。"
    "重要：工作组内各成员所在的运行环境（操作系统、工作区、可达路径等）不一定一致；"
    "对待路径、文件位置、本机命令等与所属环境相关联的信息，必须按成员各自的 Home Node / 工作区区分其正确性，"
    "不要把某一成员环境下的路径或假设套用到其他成员或全局。"
    "任务完成后，向组内给出简洁的最终答复。"
)

_WG_STATUS_ZH = {
    "configuring": "配置中",
    "active": "活跃",
    "archiving": "归档中",
    "archived": "已归档",
}


def build_leader_system_prompt(*, workgroup: WorkGroup) -> str:
    """组装 Supervisor system：固定编排规则 + 当前工作组基本信息。"""
    status = _WG_STATUS_ZH.get(workgroup.status, workgroup.status)
    return "\n".join(
        [
            _LEADER_SYSTEM_RULES,
            "",
            "## 当前工作组",
            f"- 名称：{workgroup.display_name}",
            f"- ID：{workgroup.workgroup_id}",
            f"- 状态：{status}",
            f"- 创建者 Node：{workgroup.created_by_node_id}",
            f"- LLM 配置：{workgroup.llm_profile_id}",
        ]
    )


# 兼容旧引用名（测试 / 外部若仍导入）
_LEADER_SYSTEM = _LEADER_SYSTEM_RULES

# (workgroup_id, assign_id, member_id, tool_name, tool_call_id, arguments_json) -> tool result content
MemberToolRunner = Callable[[str, str, str, str, str, str], str]


class TurnKernel:
    """Manage 侧 turn 编排。

    Leader LLM loop（Manage-native 工具）+ Member LLM loop（Node-executable 经 tool.command）。
    Assign 默认同步 scripted completer；生产路径由 VerticalLoop.make_assign_completer
    创建 Member ActorRun 并跑 run_member_until_idle。
    """

    def __init__(
        self,
        store: WorkGroupStore,
        *,
        llm_store: LLMConfigStore | None = None,
        chat_client: LLMChatClient | None = None,
        member_chat_client: LLMChatClient | None = None,
        assign_completer: AssignCompleter | None = None,
        registry_store: Any | None = None,
        max_tool_loops: int = _DEFAULT_MAX_TOOL_LOOPS,
        mock_llm: bool = False,
        context_silent_trigger_tokens: int = DEFAULT_CONTEXT_COMPRESSION_TRIGGER_TOKENS,
        context_blocking_trigger_tokens: int = DEFAULT_CONTEXT_COMPRESSION_BLOCKING_TRIGGER_TOKENS,
        context_keep_tokens: int = DEFAULT_CONTEXT_COMPRESSION_KEEP_TOKENS,
    ) -> None:
        self._store = store
        self._llm_store = llm_store
        self._chat_client = chat_client
        self._member_chat_client = member_chat_client
        self._assign_completer = assign_completer
        self._registry_store = registry_store
        self._max_tool_loops = max(1, max_tool_loops)
        self._mock_llm = mock_llm
        self._context_silent_trigger_tokens = max(0, int(context_silent_trigger_tokens))
        self._context_blocking_trigger_tokens = max(0, int(context_blocking_trigger_tokens))
        self._context_keep_tokens = max(0, int(context_keep_tokens))
        if (
            self._context_blocking_trigger_tokens > 0
            and self._context_silent_trigger_tokens > 0
            and self._context_blocking_trigger_tokens < self._context_silent_trigger_tokens
        ):
            raise ValueError("context_blocking_trigger_tokens must be >= context_silent_trigger_tokens")
        # Compatibility for the old standalone CAS skeleton. Real HITL rows
        # always use the durable Store path in resolve_hitl_cas below.
        self._legacy_hitl_resolutions: dict[str, dict[str, Any]] = {}
        # workgroup_id -> cancel flag（用户中断当前 turn）
        self._cancel_flags: dict[str, threading.Event] = {}
        self._active_turn: dict[str, dict[str, Any]] = {}
        self._turn_lock = threading.Lock()
        # workgroup_id -> FIFO human 队列（进程内；对齐 Node MessageQueue 单飞）
        self._human_queues: dict[str, list[QueuedHuman]] = {
            workgroup_id: [QueuedHuman.from_record(record) for record in records]
            for workgroup_id in self._store.list_human_queue_workgroups()
            for records in [self._store.list_human_queue_records(workgroup_id)]
        }
        self._command_cancel_hook: Callable[[str], None] | None = None
        self._realtime_event_listener: Callable[
            [str, str, dict[str, Any], str | None], None
        ] | None = None
        self._hitl_resume_lock = threading.Lock()
        self._resuming_hitls: set[str] = set()
        self._context_executor = ThreadPoolExecutor(max_workers=2, thread_name_prefix="wg-context")
        self._context_task_lock = threading.Lock()
        self._context_tasks: dict[str, Future[Any]] = {}

    def set_assign_completer(self, completer: AssignCompleter | None) -> None:
        self._assign_completer = completer

    def set_command_cancel_hook(self, hook: Callable[[str], None] | None) -> None:
        """cancel_turn 时唤醒 Node command/AgentRef waiters。"""
        self._command_cancel_hook = hook

    def set_realtime_event_listener(
        self,
        listener: Callable[[str, str, dict[str, Any], str | None], None] | None,
    ) -> None:
        self._realtime_event_listener = listener

    def _publish_realtime(
        self,
        workgroup_id: str,
        event_type: str,
        data: Any,
        *,
        client_message_id: str | None,
    ) -> None:
        listener = self._realtime_event_listener
        if listener is None:
            return
        public = _public_realtime_data(event_type, data)
        if public is None:
            return
        try:
            listener(workgroup_id, event_type, public, client_message_id)
        except Exception:  # noqa: BLE001 - realtime fan-out must not break a turn
            return

    def _cancel_event(self, workgroup_id: str) -> threading.Event:
        with self._turn_lock:
            ev = self._cancel_flags.get(workgroup_id)
            if ev is None:
                ev = threading.Event()
                self._cancel_flags[workgroup_id] = ev
            return ev

    def _begin_turn(self, workgroup_id: str, *, mode: str, **meta: Any) -> threading.Event:
        flag = self._cancel_event(workgroup_id)
        flag.clear()
        token = str(meta.get("turn_token") or wg_ids.new_ulid())
        with self._turn_lock:
            self._active_turn[workgroup_id] = {
                "mode": mode,
                "turn_token": token,
                "turn_started_at": time.monotonic(),
                **meta,
            }
            self._save_turn_checkpoint_unlocked(workgroup_id)
        return flag

    def _save_turn_checkpoint_unlocked(self, workgroup_id: str) -> None:
        meta = dict(self._active_turn.get(workgroup_id) or {})
        token = str(meta.pop("turn_token", "") or "")
        mode = str(meta.pop("mode", "") or "")
        if not token or not mode:
            return
        try:
            self._store.save_turn_checkpoint(
                TurnCheckpoint(
                    workgroup_id=workgroup_id,
                    turn_token=token,
                    mode=mode,
                    metadata=meta,
                    updated_at=datetime.now().astimezone().isoformat(),
                )
            )
        except Exception:  # noqa: BLE001 - checkpoint must not break the turn
            pass

    def _on_hitl_created(self, hitl: HITLRequest) -> None:
        if not hitl.run_id:
            return
        run = self._store.get_actor_run(hitl.run_id)
        if run is not None and run.status == "running":
            self._store.update_actor_run(hitl.run_id, status="awaiting_hitl")

    def _on_hitl_resolved(self, hitl: HITLRequest) -> None:
        if not hitl.run_id:
            return
        run = self._store.get_actor_run(hitl.run_id)
        if run is not None and run.status == "awaiting_hitl":
            self._store.update_actor_run(hitl.run_id, status="running")

    def resume_resolved_hitl(self, hitl: HITLRequest) -> dict[str, Any]:
        """Continue a leader loop whose waiting thread was lost on restart."""
        hid = str(hitl.hitl_id or "").strip()
        run_id = str(hitl.run_id or "").strip()
        tool_call_id = str(hitl.tool_call_id or "").strip()
        if not run_id or not tool_call_id:
            return {"scheduled": False, "reason": "hitl_not_bound_to_run"}
        with self._hitl_resume_lock:
            if hid in self._resuming_hitls:
                return {"scheduled": False, "reason": "already_resuming"}
            self._resuming_hitls.add(hid)
        run = self._store.get_actor_run(run_id)
        if run is None or run.actor_id != "leader":
            with self._hitl_resume_lock:
                self._resuming_hitls.discard(hid)
            return {"scheduled": False, "reason": "run_not_found"}
        if run.status not in {"running", "awaiting_hitl"}:
            with self._hitl_resume_lock:
                self._resuming_hitls.discard(hid)
            return {"scheduled": False, "reason": "run_not_resumable"}
        history = self._store.ensure_run_history(run)
        has_tool_call = any(
            message.role == "assistant"
            and any(call.id == tool_call_id for call in (message.tool_calls or []))
            for message in history.messages
        )
        if not has_tool_call:
            with self._hitl_resume_lock:
                self._resuming_hitls.discard(hid)
            return {"scheduled": False, "reason": "tool_call_not_found"}
        if not any(m.role == "tool" and m.tool_call_id == tool_call_id for m in history.messages):
            content = self._package_tool_content(
                format_hitl_resolution(hitl),
                tool_name="ask_workgroup_user",
                run_id=run_id,
                tool_call_id=tool_call_id,
            )
            self._store.append_run_history(
                run_id,
                [
                    RunHistoryMessage(
                        role="tool",
                        tool_call_id=tool_call_id,
                        name="ask_workgroup_user",
                        content=content,
                    )
                ],
            )
        self._store.update_actor_run(run_id, status="running")

        def _worker() -> None:
            try:
                for _ in self.run_leader_until_idle_events(hitl.workgroup_id, run_id):
                    pass
            except Exception:  # noqa: BLE001 - durable state remains auditable
                try:
                    current = self._store.get_actor_run(run_id)
                    if current is not None and current.status == "running":
                        self._store.update_actor_run(run_id, status="indeterminate")
                except Exception:  # noqa: BLE001
                    pass
            finally:
                with self._hitl_resume_lock:
                    self._resuming_hitls.discard(hid)

        threading.Thread(
            target=_worker,
            name=f"wg-hitl-resume-{hid[-8:]}",
            daemon=True,
        ).start()
        return {"scheduled": True, "run_id": run_id, "hitl_id": hid}

    def resume_persisted_hitls(self) -> list[dict[str, Any]]:
        """Resume resolved HITLs left behind by a crash before dispatch."""
        resumed: list[dict[str, Any]] = []
        for hitl in self._store.list_resolved_bound_hitls():
            if self._store.has_hitl_waiter(hitl.hitl_id):
                continue
            if not hitl.run_id:
                continue
            run = self._store.get_actor_run(hitl.run_id)
            if run is None or run.status not in {"running", "awaiting_hitl"}:
                continue
            history = self._store.get_run_history(hitl.run_id)
            if history is not None and any(
                message.role == "tool" and message.tool_call_id == hitl.tool_call_id
                for message in history.messages
            ):
                continue
            result = self.resume_resolved_hitl(hitl)
            if result.get("scheduled"):
                resumed.append(result)
        return resumed

    def _end_turn(self, workgroup_id: str, *, turn_token: str | None = None) -> QueuedHuman | None:
        """结束当前 turn；若队列非空则在同一把锁内认领下一条并返回。"""
        with self._turn_lock:
            cur = self._active_turn.get(workgroup_id)
            if cur is not None:
                if turn_token and str(cur.get("turn_token") or "") != str(turn_token):
                    return None
                self._active_turn.pop(workgroup_id, None)
                self._store.clear_turn_checkpoint(workgroup_id)
            flag = self._cancel_flags.get(workgroup_id)
            if flag is not None:
                flag.clear()
            return self._claim_next_human_unlocked(workgroup_id)

    def _claim_next_human_unlocked(self, workgroup_id: str) -> QueuedHuman | None:
        """Claim the next queued item; caller must hold ``_turn_lock``."""
        bucket = self._human_queues.get(workgroup_id) or []
        if not bucket:
            self._human_queues.pop(workgroup_id, None)
            return None
        item = bucket.pop(0)
        if bucket:
            self._human_queues[workgroup_id] = bucket
        else:
            self._human_queues.pop(workgroup_id, None)
        token = wg_ids.new_ulid()
        self._active_turn[workgroup_id] = {
            "mode": "claiming",
            "turn_token": token,
            "turn_started_at": time.monotonic(),
            "queue_id": item.queue_id,
            "client_message_id": item.client_message_id,
        }
        self._save_turn_checkpoint_unlocked(workgroup_id)
        item._claim_token = token  # type: ignore[attr-defined]
        return item

    def _schedule_queued_human(self, item: QueuedHuman) -> None:
        token = str(getattr(item, "_claim_token", "") or "")

        def _worker() -> None:
            try:
                for _ in self._execute_human_turn_events(item, turn_token=token):
                    pass
            except Exception:  # noqa: BLE001
                pass

        threading.Thread(
            target=_worker,
            name=f"wg-human-queue-{item.workgroup_id[-8:]}",
            daemon=True,
        ).start()

    def _finish_human_turn(self, workgroup_id: str, *, turn_token: str) -> None:
        nxt = self._end_turn(workgroup_id, turn_token=turn_token)
        self._publish_queue_state(workgroup_id)
        if nxt is not None:
            self._schedule_queued_human(nxt)

    def _publish_queue_state(self, workgroup_id: str) -> None:
        """Broadcast the complete queue snapshot to every subscribed UI."""
        self._publish_realtime(
            workgroup_id,
            "queue",
            {"queue": self.list_human_queue(workgroup_id)},
            client_message_id=None,
        )

    def resume_persisted_queues(self) -> int:
        """Resume FIFO messages that survived a Manage restart.

        The previous worker is fenced by WorkGroupStore before this method is
        called. Only queued human messages are safe to retry; the interrupted
        LLM/assign turn itself is never replayed automatically.
        """
        scheduled = 0
        for workgroup_id in self._store.list_human_queue_workgroups():
            group = self._store.get_workgroup(workgroup_id)
            if group is None or group.status != "active":
                continue
            with self._turn_lock:
                if workgroup_id in self._active_turn:
                    continue
                bucket = self._human_queues.get(workgroup_id) or []
                if not bucket:
                    continue
                item = bucket.pop(0)
                if bucket:
                    self._human_queues[workgroup_id] = bucket
                else:
                    self._human_queues.pop(workgroup_id, None)
                token = wg_ids.new_ulid()
                self._active_turn[workgroup_id] = {
                    "mode": "claiming",
                    "turn_token": token,
                    "queue_id": item.queue_id,
                    "client_message_id": item.client_message_id,
                }
                self._save_turn_checkpoint_unlocked(workgroup_id)
                item._claim_token = token  # type: ignore[attr-defined]
            self._schedule_queued_human(item)
            scheduled += 1
        return scheduled

    def _update_turn(self, workgroup_id: str, **meta: Any) -> None:
        with self._turn_lock:
            cur = self._active_turn.get(workgroup_id)
            if cur is not None:
                cur.update(meta)
                self._save_turn_checkpoint_unlocked(workgroup_id)

    def _append_turn_meta(self, workgroup_id: str, key: str, value: Any) -> None:
        """Append repeatable turn metadata without clobbering peer workers."""
        with self._turn_lock:
            cur = self._active_turn.get(workgroup_id)
            if cur is None:
                return
            values = list(cur.get(key) or [])
            if value not in values:
                values.append(value)
            cur[key] = values
            self._save_turn_checkpoint_unlocked(workgroup_id)

    def _active_client_message_id(self, workgroup_id: str) -> str | None:
        with self._turn_lock:
            value = (self._active_turn.get(workgroup_id) or {}).get("client_message_id")
        value = str(value or "").strip()
        return value or None

    def _is_turn_busy(self, workgroup_id: str) -> bool:
        with self._turn_lock:
            return workgroup_id in self._active_turn

    def _queue_snapshot(self, workgroup_id: str) -> list[dict[str, Any]]:
        with self._turn_lock:
            items = list(self._human_queues.get(workgroup_id) or [])
        return [item.to_public(i + 1) for i, item in enumerate(items)]

    def list_human_queue(self, workgroup_id: str) -> dict[str, Any]:
        if self._store.get_workgroup(workgroup_id) is None:
            raise WorkgroupError("not_found", "workgroup not found", http_status=404)
        busy = self._is_turn_busy(workgroup_id)
        items = self._queue_snapshot(workgroup_id)
        with self._turn_lock:
            meta = dict(self._active_turn.get(workgroup_id) or {})
        return {
            "busy": busy,
            "active_mode": str(meta.get("mode") or "") or None,
            "items": items,
            "depth": len(items),
        }

    def patch_human_queue_item(self, workgroup_id: str, queue_id: str, *, text: str) -> dict[str, Any]:
        from manage.workgroup.human_queue import _now

        body = (text or "").strip()
        if not body:
            raise WorkgroupError("invalid_argument", "text required", http_status=400)
        qid = (queue_id or "").strip()
        with self._turn_lock:
            items = self._human_queues.get(workgroup_id) or []
            for idx, item in enumerate(items):
                if item.queue_id != qid:
                    continue
                item.text = body
                item.updated_at = _now()
                self._store.save_human_queue_record(item.to_record())
                out = item.to_public(idx + 1)
                break
            else:
                raise WorkgroupError("not_found", "queued message not found", http_status=404)
        self._publish_queue_state(workgroup_id)
        return out

    def cancel_human_queue_item(self, workgroup_id: str, queue_id: str) -> dict[str, Any]:
        qid = (queue_id or "").strip()
        with self._turn_lock:
            items = self._human_queues.get(workgroup_id) or []
            for idx, item in enumerate(items):
                if item.queue_id != qid:
                    continue
                items.pop(idx)
                if items:
                    self._human_queues[workgroup_id] = items
                else:
                    self._human_queues.pop(workgroup_id, None)
                self._store.delete_human_queue_record(workgroup_id, qid)
                out = {"cancelled": True, "queue_id": qid, "depth": len(items)}
                break
            else:
                raise WorkgroupError("not_found", "queued message not found", http_status=404)
        self._publish_queue_state(workgroup_id)
        return out

    def _active_turn_has_live_work(self, workgroup_id: str) -> bool:
        """Return whether an active turn still has durable work to cancel."""
        with self._turn_lock:
            meta = dict(self._active_turn.get(workgroup_id) or {})
        if not meta:
            return False
        if str(meta.get("mode") or "") == "claiming":
            return True
        if self._store.list_assigns(workgroup_id, active_only=True):
            return True
        if any(
            run.status in {"running", "awaiting_hitl"}
            for run in self._store.list_actor_runs(workgroup_id, limit=100)
        ):
            return True
        if self._store.list_hitl(workgroup_id, pending_only=True):
            return True
        started = float(meta.get("turn_started_at") or 0)
        if started and time.monotonic() - started < 1.0:
            return True
        return False

    def send_human_queue_item_now(self, workgroup_id: str, queue_id: str) -> dict[str, Any]:
        """Promote a queued message and interrupt the current turn for it."""
        qid = (queue_id or "").strip()
        if not qid:
            raise WorkgroupError("invalid_argument", "queue_id required", http_status=400)
        from manage.workgroup.human_queue import _now

        with self._turn_lock:
            items = self._human_queues.get(workgroup_id) or []
            item = next((candidate for candidate in items if candidate.queue_id == qid), None)
            if item is None:
                raise WorkgroupError("not_found", "queued message not found", http_status=404)
            item.priority = max((int(candidate.priority or 0) for candidate in items), default=0) + 1
            item.updated_at = _now()
            items.sort(
                key=lambda candidate: (-int(candidate.priority or 0), candidate.created_at, candidate.queue_id)
            )
            self._human_queues[workgroup_id] = items
            self._store.save_human_queue_record(item.to_record())
            old_token = str((self._active_turn.get(workgroup_id) or {}).get("turn_token") or "")

        was_live = self._active_turn_has_live_work(workgroup_id)
        cancel_result = self.cancel_turn(workgroup_id)
        claimed: QueuedHuman | None = None
        if not was_live:
            with self._turn_lock:
                current = self._active_turn.get(workgroup_id)
                current_token = str((current or {}).get("turn_token") or "")
                if current is None or current_token == old_token:
                    if current is not None:
                        self._active_turn.pop(workgroup_id, None)
                        self._store.clear_turn_checkpoint(workgroup_id)
                    flag = self._cancel_flags.get(workgroup_id)
                    if flag is not None:
                        flag.clear()
                    claimed = self._claim_next_human_unlocked(workgroup_id)
        self._publish_queue_state(workgroup_id)
        if claimed is not None:
            self._schedule_queued_human(claimed)
        return {
            "sent_now": True,
            "queue_id": qid,
            "cancel": cancel_result,
            "queue": self.list_human_queue(workgroup_id),
        }

    def _enqueue_human_unlocked(self, item: QueuedHuman) -> int:
        """调用方须持有 _turn_lock。返回 1-based position。"""
        cid = (item.client_message_id or "").strip()
        if cid:
            for idx, existing in enumerate(self._human_queues.get(item.workgroup_id) or []):
                if (existing.client_message_id or "").strip() == cid:
                    return idx + 1
        bucket = self._human_queues.setdefault(item.workgroup_id, [])
        bucket.append(item)
        return len(bucket)

    def _claim_or_enqueue(
        self,
        workgroup_id: str,
        *,
        text: str,
        from_node_id: str,
        client_message_id: str | None,
        disable_tools: bool,
        direct_member_id: str | None,
    ) -> tuple[str, QueuedHuman, int]:
        """返回 (\"run\"|\"queued\", item, position)。"""
        item = QueuedHuman(
            queue_id=wg_ids.queue_human_id(),
            workgroup_id=workgroup_id,
            text=text,
            from_node_id=from_node_id,
            client_message_id=client_message_id,
            direct_member_id=direct_member_id,
            disable_tools=disable_tools,
        )
        with self._turn_lock:
            busy = workgroup_id in self._active_turn
            pending = list(self._human_queues.get(workgroup_id) or [])
            if busy or pending:
                pos = self._enqueue_human_unlocked(item)
                self._store.save_human_queue_record(item.to_record())
                return "queued", item, pos
            token = wg_ids.new_ulid()
            self._active_turn[workgroup_id] = {
                "mode": "claiming",
                "turn_token": token,
                "queue_id": item.queue_id,
                "client_message_id": item.client_message_id,
            }
            self._save_turn_checkpoint_unlocked(workgroup_id)
            item_meta = item
            # stash token on item via active_turn only
            return "run", item_meta, 0

    def _is_cancelled(self, workgroup_id: str) -> bool:
        return self._cancel_event(workgroup_id).is_set()

    def _raise_if_cancelled(self, workgroup_id: str) -> None:
        if self._is_cancelled(workgroup_id):
            raise WorkgroupError("canceled", "workgroup turn cancelled", http_status=409)

    def cancel_turn(self, workgroup_id: str) -> dict[str, Any]:
        """取消当前工作组活跃 turn：置位 cancel flag + fail active assigns + heal open tools。"""
        if self._store.get_workgroup(workgroup_id) is None:
            raise WorkgroupError("not_found", "workgroup not found", http_status=404)
        with self._turn_lock:
            meta = dict(self._active_turn.get(workgroup_id) or {})
        mode = str(meta.get("mode") or "")
        if not mode:
            # 无内存 turn 时仍尝试释放卡住的 assign / 工具等待
            if self._command_cancel_hook is not None:
                try:
                    self._command_cancel_hook(workgroup_id)
                except Exception:  # noqa: BLE001
                    pass
            failed_ids = self._store.fail_active_assigns(
                workgroup_id,
                reason="cancelled by user",
                error_code="canceled",
            )
            try:
                self._store.cancel_pending_hitls(workgroup_id)
            except Exception:  # noqa: BLE001
                pass
            return {
                "cancelled": bool(failed_ids),
                "mode": "idle" if not failed_ids else "orphan_assign",
                "failed_assign_ids": list(failed_ids),
                "leader_run_id": None,
                "member_run_id": None,
            }

        self._cancel_event(workgroup_id).set()
        if self._command_cancel_hook is not None:
            try:
                # The hook must see active assigns before the durable store
                # releases them below, so it can send AgentRef cancellation
                # frames with the correct session identity.
                self._command_cancel_hook(workgroup_id)
            except Exception:  # noqa: BLE001
                pass
        failed_ids = self._store.fail_active_assigns(
            workgroup_id,
            reason="cancelled by user",
            error_code="canceled",
        )
        try:
            self._store.cancel_pending_hitls(workgroup_id)
        except Exception:  # noqa: BLE001
            pass
        leader_run_id = meta.get("leader_run_id")
        member_run_id = meta.get("member_run_id")
        member_run_ids = list(meta.get("member_run_ids") or [])
        if member_run_id:
            member_run_ids.append(member_run_id)
        seen_run_ids: set[str] = set()
        for rid in [leader_run_id, *member_run_ids]:
            if not rid:
                continue
            rid = str(rid)
            if rid in seen_run_ids:
                continue
            seen_run_ids.add(rid)
            try:
                self._heal_open_tool_calls(rid, reason="turn cancelled by user")
                run = self._store.get_actor_run(rid)
                if run and run.status in {"running", "awaiting_hitl"}:
                    self._store.update_actor_run(rid, status="canceled")
            except Exception:  # noqa: BLE001
                pass
        for assign_id in failed_ids:
            try:
                assign = self._store.get_assign(assign_id)
                actor = (assign.member_id if assign else None) or "leader"
                self._store.append_timeline(
                    workgroup_id,
                    type="assign_finished",
                    actor_id=actor,
                    text="已中断",
                    protocol_name=protocol_name_for_actor(actor),
                    assign_id=assign_id,
                )
            except Exception:  # noqa: BLE001
                pass
        return {
            "cancelled": True,
            "mode": mode,
            "failed_assign_ids": list(failed_ids),
            "leader_run_id": leader_run_id,
            "member_run_id": member_run_id,
            "member_run_ids": member_run_ids,
        }

    def start_leader_run(self, workgroup_id: str, *, llm_profile_revision: str | None = None) -> ActorRun:
        """Return the workgroup's persistent Supervisor session."""
        return self._store.get_or_create_actor_session(
            workgroup_id,
            actor_id="leader",
            llm_profile_revision=llm_profile_revision,
        )

    def assign_member(self, workgroup_id: str, req: AssignCreateRequest) -> Assign:
        return self._store.create_assign(workgroup_id, req)

    def project(self, *, actor_id: str, run_id: str | None = None, member_id: str | None = None) -> dict[str, Any]:
        run = self._store.get_actor_run(run_id) if run_id else None
        member = self._store.get_member(member_id) if member_id else None
        hist = self._store.get_run_history(run_id) if run_id else None
        timeline = self._store.list_timeline(run.workgroup_id) if run else []
        return project_actor_context(
            actor_id=actor_id,
            run=run,
            member=member,
            timeline_events=timeline,
            own_run_history=hist.messages if hist else [],
            context_snapshot=self._store.get_context_snapshot(run_id) if run_id else None,
        )

    def _context_snapshot_for_request(
        self,
        *,
        run: ActorRun,
        history: list[RunHistoryMessage],
        client: LLMChatClient,
        actor_label: str,
    ):
        """Harvest a ready snapshot, then schedule silent or blocking work."""
        snapshot = self._harvest_context_task(run)
        if self._context_compression_blocked(run, history):
            return snapshot

        silent_plan = build_compression_plan(
            history,
            snapshot=snapshot,
            trigger_tokens=self._context_silent_trigger_tokens,
            keep_tokens=self._context_keep_tokens,
        )
        if silent_plan is None:
            return snapshot
        task = self._ensure_context_task(
            run=run,
            history=history,
            snapshot=snapshot,
            plan=silent_plan,
            client=client,
            actor_label=actor_label,
        )
        if self._context_blocking_trigger_tokens <= 0:
            return snapshot

        blocking_plan = build_compression_plan(
            history,
            snapshot=snapshot,
            trigger_tokens=self._context_blocking_trigger_tokens,
            keep_tokens=self._context_keep_tokens,
        )
        if blocking_plan is None:
            return snapshot
        if task is None:
            task = self._ensure_context_task(
                run=run,
                history=history,
                snapshot=snapshot,
                plan=blocking_plan,
                client=client,
                actor_label=actor_label,
            )
        if task is None:
            return snapshot
        # The blocking tier only waits for the already-started snapshot task.
        # It never directly mutates the primary message history.
        try:
            task.result()
        except Exception:
            return snapshot
        # A silent task may have summarized an older prefix.  Harvest it and,
        # if the current history still needs blocking compression, schedule a
        # fresh task from the current source boundary and wait for that task.
        snapshot = self._harvest_context_task(run)
        current_history = self._store.get_run_history(run.run_id)
        if current_history is None or self._context_compression_blocked(run, current_history.messages):
            return snapshot
        current_plan = build_compression_plan(
            current_history.messages,
            snapshot=snapshot,
            trigger_tokens=self._context_blocking_trigger_tokens,
            keep_tokens=self._context_keep_tokens,
        )
        if current_plan is None:
            return snapshot
        task = self._ensure_context_task(
            run=run,
            history=current_history.messages,
            snapshot=snapshot,
            plan=current_plan,
            client=client,
            actor_label=actor_label,
        )
        if task is None:
            return snapshot
        try:
            task.result()
        except Exception:
            return snapshot
        return self._harvest_context_task(run)

    def _ensure_context_task(
        self,
        *,
        run: ActorRun,
        history: list[RunHistoryMessage],
        snapshot: Any,
        plan: Any,
        client: LLMChatClient,
        actor_label: str,
    ) -> Future[Any] | None:
        with self._context_task_lock:
            existing = self._context_tasks.get(run.run_id)
            if existing is not None and not existing.done():
                return existing

            frozen_history = list(history)
            frozen_snapshot = snapshot

            def summarize() -> Any:
                result = client.chat(
                    build_summary_request(
                        frozen_history,
                        plan,
                        previous_summary=frozen_snapshot.summary_content if frozen_snapshot else "",
                        actor_label=actor_label,
                    )
                )
                summary = str(result.content or "").strip()
                if not summary:
                    return None
                return make_snapshot(
                    run_id=run.run_id,
                    workgroup_id=run.workgroup_id,
                    actor_id=run.actor_id,
                    history=frozen_history,
                    plan=plan,
                    summary=summary,
                    previous=frozen_snapshot,
                    timeline_seq=max(
                        (event.seq for event in self._store.list_timeline(run.workgroup_id)),
                        default=0,
                    ),
                )

            future = self._context_executor.submit(summarize)
            self._context_tasks[run.run_id] = future
            return future

    def _harvest_context_task(self, run: ActorRun):
        with self._context_task_lock:
            task = self._context_tasks.get(run.run_id)
            if task is None or not task.done():
                return self._store.get_context_snapshot(run.run_id)
            self._context_tasks.pop(run.run_id, None)
        try:
            candidate = task.result()
        except Exception:
            return self._store.get_context_snapshot(run.run_id)
        if candidate is None:
            return self._store.get_context_snapshot(run.run_id)
        current_history = self._store.get_run_history(run.run_id)
        if current_history is None or not snapshot_is_current(candidate, current_history.messages):
            return self._store.get_context_snapshot(run.run_id)
        current_snapshot = self._store.get_context_snapshot(run.run_id)
        if current_snapshot is not None and current_snapshot.context_epoch >= candidate.context_epoch:
            return current_snapshot
        return self._store.save_context_snapshot(candidate)

    def _context_compression_blocked(
        self,
        run: ActorRun,
        history: list[RunHistoryMessage],
    ) -> bool:
        if open_tool_call_ids(history):
            return True
        if run.assign_id:
            assign = self._store.get_assign(run.assign_id)
            if assign is not None and assign.status in {"queued", "running", "awaiting_hitl"}:
                return True
        if run.actor_id == "leader" and self._store.list_assigns(run.workgroup_id, active_only=True):
            return True
        # Do not replace the context while a human decision can still add a
        # tool result to this actor's message sequence.
        return any(
            hitl.status == "pending" and (not hitl.run_id or hitl.run_id == run.run_id)
            for hitl in self._store.list_hitl(run.workgroup_id, pending_only=True)
        )

    def resolve_hitl_cas(
        self,
        hitl_id: str,
        *,
        expected_status: str = "pending",
        resolution: dict[str, Any],
    ) -> dict[str, Any]:
        """HITL 乐观 CAS 占位：同 id 二次决议 → already_resolved。"""
        durable = self._store.get_hitl(hitl_id)
        if durable is not None:
            if expected_status != "pending":
                raise WorkgroupError(
                    "conflict",
                    f"unexpected HITL status={expected_status}",
                    http_status=409,
                )
            return self._store.resolve_hitl_cas(
                durable.workgroup_id,
                hitl_id,
                resolution=resolution,
            ).model_dump(mode="json")
        existing = self._legacy_hitl_resolutions.get(hitl_id)
        if existing is not None:
            raise WorkgroupError(
                "already_resolved",
                "HITL already resolved",
                http_status=409,
                details={"hitl_id": hitl_id, "existing": existing},
            )
        if expected_status != "pending":
            raise WorkgroupError("conflict", f"unexpected HITL status={expected_status}", http_status=409)
        stored = {"hitl_id": hitl_id, "status": "resolved", "resolution": resolution}
        self._legacy_hitl_resolutions[hitl_id] = stored
        return stored

    def handle_human_message(
        self,
        workgroup_id: str,
        *,
        text: str,
        from_node_id: str,
        client_message_id: str | None = None,
        disable_tools: bool = False,
        direct_member_id: str | None = None,
    ) -> dict[str, Any]:
        """入队或驱动 Leader / @直连至空闲。忙碌时返回 queued，不并行开 loop。"""
        final: dict[str, Any] | None = None
        queued: dict[str, Any] | None = None
        for ev in self.handle_human_message_events(
            workgroup_id,
            text=text,
            from_node_id=from_node_id,
            client_message_id=client_message_id,
            disable_tools=disable_tools,
            direct_member_id=direct_member_id,
        ):
            if ev.get("event") == "final":
                final = ev.get("data") or {}
            elif ev.get("event") == "queued":
                queued = ev.get("data") or {}
        if queued is not None:
            return {"queued": True, **queued}
        if final is None:
            raise WorkgroupError("conflict", "leader loop produced no final event", http_status=500)
        return final

    def handle_human_message_events(
        self,
        workgroup_id: str,
        *,
        text: str,
        from_node_id: str,
        client_message_id: str | None = None,
        disable_tools: bool = False,
        direct_member_id: str | None = None,
    ):
        """空闲则立即消费；忙碌则 FIFO 入队（对齐 Node MessageQueue 单飞）。"""
        self._store.assert_acl_member(workgroup_id, from_node_id)
        self._store.require_active(workgroup_id)

        action, item, position = self._claim_or_enqueue(
            workgroup_id,
            text=text,
            from_node_id=from_node_id,
            client_message_id=client_message_id,
            disable_tools=disable_tools,
            direct_member_id=direct_member_id,
        )
        if action == "queued":
            queued_event = {
                "event": "queued",
                "data": {
                    **item.to_public(position),
                    "queue": self.list_human_queue(workgroup_id),
                },
            }
            self._publish_realtime(
                workgroup_id,
                queued_event["event"],
                queued_event["data"],
                client_message_id=client_message_id,
            )
            yield queued_event
            return

        with self._turn_lock:
            token = str((self._active_turn.get(workgroup_id) or {}).get("turn_token") or "")
        for event in self._execute_human_turn_events(item, turn_token=token):
            self._publish_realtime(
                workgroup_id,
                str(event.get("event") or "message"),
                event.get("data") or {},
                client_message_id=item.client_message_id,
            )
            yield event

    def _execute_human_turn_events(self, item: QueuedHuman, *, turn_token: str):
        """真正执行一轮 human：写 Timeline + Leader / 直连；结束后泵队列。"""
        workgroup_id = item.workgroup_id
        try:
            try:
                self._store.fail_active_assigns(
                    workgroup_id,
                    reason="previous assign superseded by new human message",
                    error_code="canceled",
                )
            except Exception:  # noqa: BLE001
                pass

            member, instruction = resolve_direct_member(
                self._store,
                workgroup_id,
                direct_member_id=item.direct_member_id,
                timeline_text=item.text,
            )

            event = self._store.append_timeline(
                workgroup_id,
                type="human_message",
                actor_id=item.from_node_id,
                text=item.text,
                client_message_id=item.client_message_id,
                protocol_name=protocol_name_for_actor(item.from_node_id),
                direct_member_id=item.direct_member_id,
            )
            # Remove the durable queue item only after the human message has
            # been committed to Timeline + outbox. A crash before this point
            # leaves the message recoverable instead of silently dropping it.
            self._store.delete_human_queue_record(workgroup_id, item.queue_id)
            yield {
                "event": "human",
                "data": {"timeline_event": event.model_dump(mode="json")},
            }

            if member is not None:
                yield from self._run_direct_member_events(
                    workgroup_id,
                    member=member,
                    instruction=instruction,
                    human_event=event,
                    turn_token=turn_token,
                    client_message_id=item.client_message_id,
                )
                return

            self._begin_turn(
                workgroup_id,
                mode="leader",
                turn_token=turn_token,
                queue_id=item.queue_id,
                client_message_id=item.client_message_id,
            )
            run = self._store.get_or_create_actor_session(
                workgroup_id,
                actor_id="leader",
            )
            run = self._store.prepare_actor_session(run.run_id)
            self._update_turn(workgroup_id, leader_run_id=run.run_id)
            self._store.ensure_run_history(run)
            self._heal_open_tool_calls(
                run.run_id,
                reason="previous leader tool turn interrupted; synthetic error result",
            )
            self._append_session_user_message(
                run.run_id,
                content=item.text,
                name=event.protocol_name or protocol_name_for_actor(item.from_node_id),
                timeline_event_seq=event.seq,
            )
            loop_result: dict[str, Any] | None = None
            try:
                for ev in self.run_leader_until_idle_events(
                    workgroup_id,
                    run.run_id,
                    disable_tools=item.disable_tools,
                    required_human_event=event,
                ):
                    if ev.get("event") == "loop_final":
                        loop_result = ev.get("data") or {}
                        continue
                    yield ev
            except WorkgroupError as exc:
                if exc.code == "canceled":
                    yield {
                        "event": "final",
                        "data": {
                            "timeline_event": event,
                            "leader_run": self._store.find_running_leader_run(workgroup_id),
                            "loop": {"steps": 0, "status": "canceled", "final_text": ""},
                            "mode": "leader",
                        },
                    }
                    return
                raise
            if loop_result is None:
                raise WorkgroupError(
                    "conflict", "leader loop produced no result", http_status=500
                )
            yield {
                "event": "final",
                "data": {
                    "timeline_event": event,
                    "leader_run": loop_result["run"],
                    "loop": {
                        "steps": loop_result.get("steps"),
                        "status": loop_result.get("status"),
                        "final_text": loop_result.get("final_text"),
                    },
                    "mode": "leader",
                },
            }
        finally:
            self._finish_human_turn(workgroup_id, turn_token=turn_token)

    def _run_direct_member_events(
        self,
        workgroup_id: str,
        *,
        member: Any,
        instruction: str,
        human_event: Any,
        turn_token: str | None = None,
        client_message_id: str | None = None,
    ):
        """@直连：跳过 Leader LLM，创建 Assign + Member run。"""
        mid = member.member_id
        brief = instruction.replace("\n", " ").strip()
        if len(brief) > 96:
            brief = brief[:93] + "…"

        begin_kwargs: dict[str, Any] = {
            "member_id": mid,
            "client_message_id": client_message_id,
        }
        if turn_token:
            begin_kwargs["turn_token"] = turn_token
        self._begin_turn(workgroup_id, mode="direct", **begin_kwargs)
        yield {"event": "status", "data": {"phase": "tool", "purpose": "直连成员", "mode": "direct"}}

        # A direct mention skips the Supervisor LLM, but it still belongs to
        # the same persistent Supervisor session for future context.
        leader_run = self._store.get_or_create_actor_session(
            workgroup_id,
            actor_id="leader",
        )
        tool_call_id = "call_direct_1"
        assign = self._store.create_assign(
            workgroup_id,
            AssignCreateRequest(
                member_id=mid,
                leader_run_id=leader_run.run_id,
                instruction=instruction,
                leader_tool_call_id=tool_call_id,
            ),
        )
        self._store.set_assign_status(assign.assign_id, "running")
        leader_run = self._store.prepare_actor_session(
            leader_run.run_id,
            assign_id=assign.assign_id,
        )
        self._append_session_user_message(
            leader_run.run_id,
            content=human_event.text,
            name=human_event.protocol_name or protocol_name_for_actor(human_event.actor_id),
            timeline_event_seq=human_event.seq,
        )
        self._update_turn(workgroup_id, assign_id=assign.assign_id, leader_run_id=assign.leader_run_id)

        # Timeline 挂在成员下，不经 Supervisor 展示
        self._store.append_timeline(
            workgroup_id,
            type="assign_started",
            actor_id=mid,
            text=f"直达 · {brief}",
            protocol_name=protocol_name_for_actor(mid),
            assign_id=assign.assign_id,
        )

        completer = self._assign_completer
        if completer is None:
            raise WorkgroupError("conflict", "assign completer not configured", http_status=500)

        final_text = ""
        status = "succeeded"
        try:
            self._raise_if_cancelled(workgroup_id)
            # completer 内部会 create member ActorRun；尽量在之后更新 turn meta
            final_text = completer(
                workgroup_id,
                assign.assign_id,
                mid,
                instruction,
                tool_call_id,
            )
            self._raise_if_cancelled(workgroup_id)
            assign = self._store.set_assign_status(
                assign.assign_id, "succeeded", result_summary=final_text, error_code=None
            )
            self._store.append_timeline(
                workgroup_id,
                type="assign_finished",
                actor_id=mid,
                text="已完成",
                protocol_name=protocol_name_for_actor(mid),
                assign_id=assign.assign_id,
            )
        except WorkgroupError as exc:
            status = "canceled" if exc.code == "canceled" else "failed"
            msg = exc.message
            cur = self._store.get_assign(assign.assign_id)
            already_closed = cur is not None and cur.status not in {
                "queued",
                "pending",
                "running",
                "awaiting_hitl",
            }
            if not already_closed:
                assign = self._store.set_assign_status(
                    assign.assign_id,
                    "failed",
                    result_summary=msg,
                    error_code=exc.code,
                )
                self._store.append_timeline(
                    workgroup_id,
                    type="assign_finished",
                    actor_id=mid,
                    text="已中断" if status == "canceled" else f"失败：{msg}",
                    protocol_name=protocol_name_for_actor(mid),
                    assign_id=assign.assign_id,
                )
            final_text = msg
            if exc.code != "canceled":
                raise
        except Exception as exc:  # noqa: BLE001
            msg = str(exc) or exc.__class__.__name__
            self._store.set_assign_status(
                assign.assign_id,
                "failed",
                result_summary=msg,
                error_code="conflict",
            )
            self._store.append_timeline(
                workgroup_id,
                type="assign_finished",
                actor_id=mid,
                text=f"失败：{msg}",
                protocol_name=protocol_name_for_actor(mid),
                assign_id=assign.assign_id,
            )
            raise WorkgroupError("conflict", msg, http_status=500) from exc

        # Direct member turns do not produce a Supervisor tool result. Record
        # the member's terminal response in the canonical Supervisor history
        # so the next Supervisor turn can reason over the completed task.
        result_event = next(
            (
                event
                for event in reversed(self._store.list_timeline(workgroup_id))
                if event.assign_id == assign.assign_id
                and event.type in {"actor_final_text", "assign_finished"}
            ),
            None,
        )
        leader_history = self._store.ensure_run_history(
            self._store.get_actor_run(assign.leader_run_id)
            or leader_run
        )
        if not any(
            message.role == "user" and message.assign_id == assign.assign_id
            for message in leader_history.messages
        ):
            self._store.append_run_history(
                assign.leader_run_id,
                [
                    RunHistoryMessage(
                        role="user",
                        name=(
                            result_event.protocol_name
                            if result_event is not None and result_event.protocol_name
                            else protocol_name_for_actor(mid)
                        ),
                        content=final_text,
                        timeline_event_seq=(result_event.seq if result_event is not None else None),
                        assign_id=assign.assign_id,
                    )
                ],
                timeline_watermark_seq=max(
                    (event.seq for event in self._store.list_timeline(workgroup_id)),
                    default=0,
                ),
            )

        # 占位 leader run 仅内部收口，不写入 Timeline
        try:
            leader = self._store.get_actor_run(assign.leader_run_id)
            if leader and leader.status == "running":
                self._store.update_actor_run(
                    assign.leader_run_id,
                    status="canceled" if status == "canceled" else "succeeded",
                )
                leader = self._store.get_actor_run(assign.leader_run_id)
        except Exception:  # noqa: BLE001
            leader = self._store.get_actor_run(assign.leader_run_id)

        yield {
            "event": "assistant_final",
            "data": {"text": final_text, "mode": "direct", "member_id": mid},
        }
        yield {
            "event": "final",
            "data": {
                "timeline_event": human_event,
                "leader_run": leader,
                "loop": {
                    "steps": 1,
                    "status": status,
                    "final_text": final_text,
                },
                "mode": "direct",
                "member_id": mid,
                "assign_id": assign.assign_id,
            },
        }

    def run_leader_until_idle(
        self,
        workgroup_id: str,
        run_id: str,
        *,
        disable_tools: bool = False,
        required_human_event: Any | None = None,
    ) -> dict[str, Any]:
        for ev in self.run_leader_until_idle_events(
            workgroup_id,
            run_id,
            disable_tools=disable_tools,
            required_human_event=required_human_event,
        ):
            if ev.get("event") == "loop_final":
                return ev["data"]
        raise WorkgroupError("conflict", "leader loop produced no result", http_status=500)

    def run_leader_until_idle_events(
        self,
        workgroup_id: str,
        run_id: str,
        *,
        disable_tools: bool = False,
        required_human_event: Any | None = None,
    ):
        run = self._store.get_actor_run(run_id)
        if run is None or run.workgroup_id != workgroup_id:
            raise WorkgroupError("not_found", "actor run not found", http_status=404)
        if run.actor_id != "leader":
            raise WorkgroupError("invalid_request", "run is not a leader run")
        if run.status not in {"running", "awaiting_hitl"}:
            yield {
                "event": "loop_final",
                "data": {"run": run, "steps": 0, "status": run.status},
            }
            return

        group = self._store.require_active(workgroup_id)
        client = self._chat_client or resolve_chat_client(
            self._llm_store,
            profile_id=group.llm_profile_id,
            mock=self._mock_llm,
        )
        dispatcher = NativeToolDispatcher(
            self._store,
            leader_run_id=run_id,
            assign_completer=self._assign_completer,
            registry_store=self._registry_store,
            on_hitl_created=self._on_hitl_created,
            on_hitl_resolved=self._on_hitl_resolved,
        )
        tools = [] if disable_tools else leader_native_tools()
        steps = 0
        tool_loops = 0

        while True:
            self._raise_if_cancelled(workgroup_id)
            hist = self._store.ensure_run_history(run)
            healed = self._heal_open_tool_calls(
                run_id,
                reason="previous tool turn interrupted; synthetic error result",
            )
            if healed:
                hist = self._store.ensure_run_history(run)

            context_snapshot = self._context_snapshot_for_request(
                run=run,
                history=hist.messages,
                client=client,
                actor_label="Supervisor",
            )

            projected = project_actor_context(
                actor_id="leader",
                run=run,
                timeline_events=self._store.list_timeline(workgroup_id),
                own_run_history=hist.messages,
                context_snapshot=context_snapshot,
            )
            group = self._store.require_active(workgroup_id)
            system = build_leader_system_prompt(workgroup=group)
            messages = [{"role": "system", "content": system}] + list(projected["messages"])
            if not projected["open_tool_calls"] and steps == 0:
                messages = self._ensure_required_human_event(
                    messages,
                    required_human_event,
                    projected_timeline_seqs=projected.get("projected_timeline_seqs") or [],
                )
            messages = self._apply_today_date_hook(run_id, messages)
            yield {"event": "status", "data": {"phase": "thinking"}}
            # 超额后禁用 tools，迫使给出结论；若模型仍发起 tool_calls 则写入 soft tool_result。
            over_budget = tool_loops >= self._max_tool_loops
            step_tools: list[dict[str, Any]] = [] if (disable_tools or over_budget) else list(tools)
            result = None
            stream = getattr(client, "stream_chat", None)
            if callable(stream):
                for piece in stream(messages, tools=step_tools or None):
                    if piece.delta:
                        yield {"event": "delta", "data": {"text": piece.delta}}
                    if piece.result is not None:
                        result = piece.result
            else:
                result = client.chat(messages, tools=step_tools or None)
            if result is None:
                raise WorkgroupError("conflict", "llm stream produced no result", http_status=502)

            steps += 1
            tool_loops += 1

            assistant = self._assistant_message(result, name="leader")
            wm = max((e.seq for e in self._store.list_timeline(workgroup_id)), default=0)
            self._store.append_run_history(run_id, [assistant], timeline_watermark_seq=wm)
            run = self._store.get_actor_run(run_id) or run

            if not result.tool_calls:
                final_text = (result.content or "").strip() or "(empty)"
                final_event = self._store.append_timeline(
                    workgroup_id,
                    type="actor_final_text",
                    actor_id="leader",
                    text=final_text,
                    protocol_name="leader",
                )
                run = self._store.update_actor_run(run_id, status="succeeded", timeline_watermark_seq=wm)
                yield {
                    "event": "assistant_final",
                    "data": {
                        "text": final_text,
                        "timeline_event": final_event.model_dump(mode="json"),
                    },
                }
                yield {
                    "event": "loop_final",
                    "data": {
                        "run": run,
                        "steps": steps,
                        "status": "succeeded",
                        "final_text": final_text,
                    },
                }
                return

            if disable_tools:
                raise WorkgroupError(
                    "invalid_request",
                    "leader tools are disabled for this message, but model returned tool_calls",
                    http_status=409,
                )

            # A provider may return assistant content together with tool_calls.
            # Persist that public text before dispatching the tool so the UI can
            # keep the same order as the provider message: content -> tool.
            self._append_assistant_content_timeline(
                workgroup_id,
                actor_id="leader",
                content=result.content,
                protocol_name="leader",
            )

            if tool_loops > self._max_tool_loops:
                soft = _TOOL_LOOP_LIMIT_EXCEEDED_MESSAGE
                tool_msgs = [
                    RunHistoryMessage(
                        role="tool",
                        tool_call_id=tc.id,
                        name=tc.name,
                        content=soft,
                    )
                    for tc in result.tool_calls
                ]
                wm = max((e.seq for e in self._store.list_timeline(workgroup_id)), default=wm)
                self._store.append_run_history(run_id, tool_msgs, timeline_watermark_seq=wm)
                run = self._store.get_actor_run(run_id) or run
                # 超额一步后仍反复 tool_calls 则收束，避免 soft-reject 死循环。
                if tool_loops > self._max_tool_loops + 1:
                    final_text = (result.content or "").strip() or soft
                    final_event = self._store.append_timeline(
                        workgroup_id,
                        type="actor_final_text",
                        actor_id="leader",
                        text=final_text,
                        protocol_name="leader",
                    )
                    run = self._store.update_actor_run(
                        run_id, status="succeeded", timeline_watermark_seq=wm
                    )
                    yield {
                        "event": "assistant_final",
                        "data": {
                            "text": final_text,
                            "timeline_event": final_event.model_dump(mode="json"),
                        },
                    }
                    yield {
                        "event": "loop_final",
                        "data": {
                            "run": run,
                            "steps": steps,
                            "status": "succeeded",
                            "final_text": final_text,
                            "tool_loop_limit_exceeded": True,
                        },
                    }
                    return
                continue

            tool_calls = list(result.tool_calls)
            for tc in tool_calls:
                purpose = call_purpose_from_arguments(
                    tc.arguments,
                    leader_tool_purpose(tc.name),
                )
                # Supervisor-native tools are public orchestration progress, but
                # their raw name/arguments/result must stay in RunHistory only.
                # assign_workgroup_task already has its own assign card, so do
                # not create a duplicate top-level tool bubble for it.
                if tc.name != "assign_workgroup_task":
                    try:
                        self._store.append_timeline(
                            workgroup_id,
                            type="system_notice",
                            actor_id="leader",
                            text=purpose,
                            protocol_name="leader",
                        )
                    except Exception:  # noqa: BLE001 - progress must not block the tool
                        pass
                yield {
                    "event": "status",
                    "data": {
                        "phase": "tool",
                        "purpose": purpose,
                    },
                }
            def dispatch_one(tc: ChatToolCall) -> RunHistoryMessage:
                try:
                    content = dispatcher.dispatch(
                        workgroup_id=workgroup_id,
                        tool_name=tc.name,
                        tool_call_id=tc.id,
                        arguments_json=tc.arguments,
                    )
                except WorkgroupError as exc:
                    content = json.dumps(
                        {"status": "failed", "code": exc.code, "error": exc.message},
                        ensure_ascii=False,
                    )
                except Exception as exc:  # noqa: BLE001 — 必须配对 tool result，避免卡死后续 human turn
                    content = json.dumps(
                        {"status": "failed", "error": str(exc) or exc.__class__.__name__},
                        ensure_ascii=False,
                    )
                content = self._package_tool_content(
                    content,
                    tool_name=tc.name,
                    run_id=run_id,
                    tool_call_id=tc.id,
                )
                return RunHistoryMessage(
                    role="tool",
                    tool_call_id=tc.id,
                    name=tc.name,
                    content=content,
                )
            member_ids: list[str] = []
            for tc in tool_calls:
                try:
                    member_ids.append(str(json.loads(tc.arguments or "{}").get("member_id") or ""))
                except (TypeError, json.JSONDecodeError):
                    member_ids.append("")
            can_parallelize = (
                len(tool_calls) > 1
                and len(tool_calls) <= _MAX_PARALLEL_MEMBER_ASSIGNMENTS
                and all(tc.name == "assign_workgroup_task" for tc in tool_calls)
                and all(member_ids)
                and len(member_ids) == len(set(member_ids))
            )
            if not can_parallelize:
                tool_msgs = [dispatch_one(tool_calls[0])]
                for tc in tool_calls[1:]:
                    tool_msgs.append(dispatch_one(tc))
            else:
                with ThreadPoolExecutor(
                    max_workers=min(len(tool_calls), _MAX_PARALLEL_MEMBER_ASSIGNMENTS),
                    thread_name_prefix="wg-leader-tool",
                ) as executor:
                    futures = [executor.submit(dispatch_one, tc) for tc in tool_calls]
                    tool_msgs = [future.result() for future in futures]
            ok, wait = can_invoke_llm_after_tools(
                [{"id": tc.id, "name": tc.name} for tc in result.tool_calls],
                [{"tool_call_id": m.tool_call_id} for m in tool_msgs],
            )
            if not ok:
                raise WorkgroupError(
                    "conflict",
                    "parallel tool_calls incompletely paired",
                    details={"wait_for": wait},
                )
            wm = max((e.seq for e in self._store.list_timeline(workgroup_id)), default=wm)
            self._store.append_run_history(run_id, tool_msgs, timeline_watermark_seq=wm)
            run = self._store.get_actor_run(run_id) or run

    @staticmethod
    def _ensure_required_human_event(
        messages: list[dict[str, Any]],
        event: Any | None,
        *,
        projected_timeline_seqs: list[int],
    ) -> list[dict[str, Any]]:
        """Ensure the current human turn cannot disappear between Timeline and projection.

        Human messages normally arrive through ``project_actor_context``.  The current
        event is passed explicitly as a safety net because a reused run can have a
        watermark that is ahead of the Timeline projection after an interrupted turn.
        """
        if event is None or str(getattr(event, "type", "")) != "human_message":
            return messages
        if int(getattr(event, "seq", 0) or 0) in {
            int(seq) for seq in projected_timeline_seqs
        }:
            return messages
        content = str(getattr(event, "text", "") or "")
        name = str(getattr(event, "protocol_name", "") or "").strip()
        if not name:
            name = protocol_name_for_actor(str(getattr(event, "actor_id", "") or ""))
        if any(
            str(message.get("role") or "") == "user"
            and str(message.get("content") or "") == content
            and str(message.get("name") or "") == name
            for message in messages
        ):
            return messages
        return [*messages, {"role": "user", "name": name, "content": content}]

    def run_member_until_idle(
        self,
        workgroup_id: str,
        run_id: str,
        *,
        tool_runner: MemberToolRunner,
    ) -> dict[str, Any]:
        """跑 Member ActorRun 至无 tool_calls；工具经 tool_runner → Node tool.command。"""
        run = self._store.get_actor_run(run_id)
        if run is None or run.workgroup_id != workgroup_id:
            raise WorkgroupError("not_found", "actor run not found", http_status=404)
        member_id = (run.actor_id or "").strip()
        if not member_id or member_id == "leader":
            raise WorkgroupError("invalid_request", "run is not a member run")
        if not run.assign_id:
            raise WorkgroupError("invalid_request", "member run requires assign_id")
        if run.status not in {"running", "awaiting_hitl"}:
            return {"run": run, "steps": 0, "status": run.status, "final_text": ""}

        self._update_turn(workgroup_id, member_run_id=run_id)

        member = self._store.get_member(member_id)
        if member is None or member.workgroup_id != workgroup_id:
            raise WorkgroupError("not_found", "member not found", http_status=404)
        spec = self._store.get_spec(member_id)
        if spec is None:
            raise WorkgroupError("not_found", "member spec not found", http_status=404)

        client = self._member_chat_client or resolve_chat_client(
            self._llm_store,
            profile_id=spec.llm_profile_id,
            mock=self._mock_llm,
        )
        tools = member_openai_tools(list(spec.tools.allow_names or []))
        allow = {str(n).strip() for n in (spec.tools.allow_names or []) if str(n).strip()}
        group = self._store.get_workgroup(workgroup_id)
        runtime = self._store.member_runtime(member_id)
        host_env = host_env_from_registry(self._registry_store, member.home_node_id)
        system = build_member_system_prompt(
            soul_md=spec.prompt.soul_md,
            custom_md=spec.prompt.custom_md,
            host_env=host_env,
            member_id=member_id,
            display_name=member.display_name,
            workgroup_id=workgroup_id,
            workgroup_name=(group.display_name if group is not None else ""),
            created_by_node_id=(group.created_by_node_id if group is not None else ""),
            workspace_path=str(runtime.get("workspace_path") or ""),
        )
        max_loops = max(1, int(spec.max_tool_loops or self._max_tool_loops))
        steps = 0
        tool_loops = 0

        while True:
            self._raise_if_cancelled(workgroup_id)
            hist = self._store.ensure_run_history(run)
            healed = self._heal_open_tool_calls(
                run_id,
                reason="previous member tool turn interrupted; synthetic error result",
            )
            if healed:
                hist = self._store.ensure_run_history(run)

            context_snapshot = self._context_snapshot_for_request(
                run=run,
                history=hist.messages,
                client=client,
                actor_label=member.display_name or member_id,
            )

            projected = project_actor_context(
                actor_id=member_id,
                run=run,
                member=member,
                timeline_events=self._store.list_timeline(workgroup_id),
                own_run_history=hist.messages,
                context_snapshot=context_snapshot,
            )
            messages = [{"role": "system", "content": system}] + list(projected["messages"])
            messages = self._apply_today_date_hook(run_id, messages)
            over_budget = tool_loops >= max_loops
            step_tools: list[dict[str, Any]] = [] if over_budget else list(tools)
            client_message_id = self._active_client_message_id(workgroup_id)
            self._publish_realtime(
                workgroup_id,
                "status",
                {"phase": "thinking", "mode": "member", "member_id": member_id},
                client_message_id=client_message_id,
            )
            result = None
            stream = getattr(client, "stream_chat", None)
            if callable(stream):
                for piece in stream(messages, tools=step_tools or None):
                    if piece.delta:
                        self._publish_realtime(
                            workgroup_id,
                            "delta",
                            {
                                "text": piece.delta,
                                "mode": "member",
                                "member_id": member_id,
                            },
                            client_message_id=client_message_id,
                        )
                    if piece.result is not None:
                        result = piece.result
            else:
                result = client.chat(messages, tools=step_tools or None)
            if result is None:
                raise WorkgroupError("conflict", "member llm stream produced no result", http_status=502)
            steps += 1
            tool_loops += 1

            assistant = self._assistant_message(result, name=member_id)
            wm = max((e.seq for e in self._store.list_timeline(workgroup_id)), default=0)
            self._store.append_run_history(run_id, [assistant], timeline_watermark_seq=wm)
            run = self._store.get_actor_run(run_id) or run

            if not result.tool_calls:
                final_text = (result.content or "").strip() or "(empty)"
                self._store.append_timeline(
                    workgroup_id,
                    type="actor_final_text",
                    actor_id=member_id,
                    text=final_text,
                    protocol_name=protocol_name_for_actor(member_id),
                    assign_id=run.assign_id,
                )
                run = self._store.update_actor_run(run_id, status="succeeded", timeline_watermark_seq=wm)
                return {
                    "run": run,
                    "steps": steps,
                    "status": "succeeded",
                    "final_text": final_text,
                }

            if not tools:
                raise WorkgroupError(
                    "invalid_request",
                    "member has no tools but model returned tool_calls",
                    http_status=409,
                )

            # Keep member pre-tool text in the public timeline as well as in
            # RunHistory.  For direct mentions this event is rendered directly
            # before the member's tool bubble; for assigned work it is attached
            # to the assign and rendered before its tool steps.
            self._append_assistant_content_timeline(
                workgroup_id,
                actor_id=member_id,
                content=result.content,
                protocol_name=protocol_name_for_actor(member_id),
                assign_id=run.assign_id,
            )

            if tool_loops > max_loops:
                soft = _TOOL_LOOP_LIMIT_EXCEEDED_MESSAGE
                tool_msgs = [
                    RunHistoryMessage(
                        role="tool",
                        tool_call_id=tc.id,
                        name=tc.name,
                        content=soft,
                    )
                    for tc in result.tool_calls
                ]
                wm = max((e.seq for e in self._store.list_timeline(workgroup_id)), default=wm)
                self._store.append_run_history(run_id, tool_msgs, timeline_watermark_seq=wm)
                run = self._store.get_actor_run(run_id) or run
                if tool_loops > max_loops + 1:
                    final_text = (result.content or "").strip() or soft
                    self._store.append_timeline(
                        workgroup_id,
                        type="actor_final_text",
                        actor_id=member_id,
                        text=final_text,
                        protocol_name=protocol_name_for_actor(member_id),
                        assign_id=run.assign_id,
                    )
                    run = self._store.update_actor_run(
                        run_id, status="succeeded", timeline_watermark_seq=wm
                    )
                    return {
                        "run": run,
                        "steps": steps,
                        "status": "succeeded",
                        "final_text": final_text,
                        "tool_loop_limit_exceeded": True,
                    }
                continue

            tool_msgs: list[RunHistoryMessage] = []
            for tc in result.tool_calls:
                name = (tc.name or "").strip()
                self._publish_realtime(
                    workgroup_id,
                    "status",
                    {
                        "phase": "tool",
                        "purpose": call_purpose_from_arguments(
                            tc.arguments,
                            purpose_for_tool(name),
                        ),
                        "mode": "member",
                        "member_id": member_id,
                    },
                    client_message_id=client_message_id,
                )
                # 轻量进度：公开 Timeline 只写脱敏 purpose，不含工具名、参数或结果。
                try:
                    self._store.append_timeline(
                        workgroup_id,
                        type="system_notice",
                        actor_id=member_id,
                        text=call_purpose_from_arguments(
                            tc.arguments,
                            purpose_for_tool(name),
                        ),
                        protocol_name=protocol_name_for_actor(member_id),
                        assign_id=run.assign_id,
                    )
                except Exception:  # noqa: BLE001 — 进度事件失败不阻断执行
                    pass
                try:
                    if name not in allow:
                        content = f"ERROR: tool {name!r} is not in member allowlist"
                    else:
                        content = tool_runner(
                            workgroup_id,
                            run.assign_id or "",
                            member_id,
                            name,
                            tc.id,
                            tc.arguments or "{}",
                        )
                except WorkgroupError as exc:
                    content = f"ERROR ({exc.code}): {exc.message}"
                except Exception as exc:  # noqa: BLE001
                    content = f"ERROR: {exc or exc.__class__.__name__}"
                content = self._package_tool_content(
                    content,
                    tool_name=name,
                    run_id=run_id,
                    tool_call_id=tc.id,
                )
                tool_msgs.append(
                    RunHistoryMessage(
                        role="tool",
                        tool_call_id=tc.id,
                        name=name,
                        content=content,
                    )
                )
            ok, wait = can_invoke_llm_after_tools(
                [{"id": tc.id, "name": tc.name} for tc in result.tool_calls],
                [{"tool_call_id": m.tool_call_id} for m in tool_msgs],
            )
            if not ok:
                raise WorkgroupError(
                    "conflict",
                    "parallel tool_calls incompletely paired",
                    details={"wait_for": wait},
                )
            wm = max((e.seq for e in self._store.list_timeline(workgroup_id)), default=wm)
            self._store.append_run_history(run_id, tool_msgs, timeline_watermark_seq=wm)
            run = self._store.get_actor_run(run_id) or run

    def _apply_today_date_hook(
        self, run_id: str, messages: list[dict[str, Any]]
    ) -> list[dict[str, Any]]:
        """步进前注入当天日期；若新插入则持久化到 RunHistory 以免每步重复。"""
        messages, inserted = ensure_today_date_in_messages(messages)
        if inserted is None:
            return messages
        self._store.append_run_history(
            run_id,
            [
                RunHistoryMessage(
                    role="user",
                    name=TODAY_DATE_MESSAGE_NAME,
                    content=str(inserted.get("content") or ""),
                )
            ],
        )
        return messages

    def _append_session_user_message(
        self,
        run_id: str,
        *,
        content: str,
        name: str | None = None,
        timeline_event_seq: int | None = None,
        assign_id: str | None = None,
    ) -> None:
        """Append one durable actor input before starting/resuming a Turn.

        A persistent session must never rely on the temporary provider
        message list for the current input.  The date hook is persisted before
        the user/task message so subsequent tool loops see the same ordering.
        """
        run = self._store.get_actor_run(run_id)
        if run is None:
            raise WorkgroupError("not_found", "actor run not found", http_status=404)
        history = self._store.ensure_run_history(run)
        if timeline_event_seq is not None and any(
            m.timeline_event_seq == timeline_event_seq for m in history.messages
        ):
            return
        if assign_id and any(m.assign_id == assign_id for m in history.messages):
            return
        today = format_today_date_message(datetime.now().strftime("%Y%m%d"))
        if not any(
            m.role == "user" and (m.content or "").strip() == today
            for m in history.messages
        ):
            self._store.append_run_history(
                run_id,
                [RunHistoryMessage(role="user", name=TODAY_DATE_MESSAGE_NAME, content=today)],
            )
        self._store.append_run_history(
            run_id,
            [
                RunHistoryMessage(
                    role="user",
                    name=name,
                    content=(content or "").strip() or "(empty)",
                    timeline_event_seq=timeline_event_seq,
                    assign_id=assign_id,
                )
            ],
        )

    @staticmethod
    def _package_tool_content(
        content: str,
        *,
        tool_name: str,
        run_id: str,
        tool_call_id: str,
    ) -> str:
        packed = package_tool_result(
            content or "",
            tool_name=tool_name,
            run_id=run_id,
            tool_call_id=tool_call_id,
        )
        return packed.for_history

    def _append_assistant_content_timeline(
        self,
        workgroup_id: str,
        *,
        actor_id: str,
        content: str | None,
        protocol_name: str | None = None,
        assign_id: str | None = None,
    ) -> Any | None:
        text = str(content or "")
        if not text.strip():
            return None
        return self._store.append_timeline(
            workgroup_id,
            type="assistant_content",
            actor_id=actor_id,
            text=text,
            protocol_name=protocol_name,
            assign_id=assign_id,
        )

    def _heal_open_tool_calls(
        self,
        run_id: str,
        *,
        reason: str,
        preserve_assign_id: str | None = None,
    ) -> list[str]:
        """为中断留下的未配对 tool_call 补失败 result，并释放对应 active assign。"""
        run = self._store.get_actor_run(run_id)
        if run is None:
            return []
        hist = self._store.ensure_run_history(run)
        open_ids = open_tool_call_ids(hist.messages)
        if not open_ids:
            return []
        names: dict[str, str] = {}
        for m in hist.messages:
            if m.role != "assistant" or not m.tool_calls:
                continue
            for tc in m.tool_calls:
                names[tc.id] = (tc.function.name if tc.function else "") or "unknown"
        # 中断的 Leader 工具轮常伴随卡住的 active assign；一并释放以免永久 conflict
        try:
            self._store.fail_active_assigns(
                run.workgroup_id,
                reason=reason,
                error_code="canceled",
                exclude_assign_ids={preserve_assign_id} if preserve_assign_id else None,
            )
        except Exception:  # noqa: BLE001
            pass
        msgs = [
            RunHistoryMessage(
                role="tool",
                tool_call_id=cid,
                name=names.get(cid) or "unknown",
                content=json.dumps({"status": "failed", "error": reason}, ensure_ascii=False),
            )
            for cid in open_ids
        ]
        self._store.append_run_history(run_id, msgs)
        return list(open_ids)

    @staticmethod
    def _assistant_message(result: ChatResult, *, name: str = "leader") -> RunHistoryMessage:
        tool_calls = None
        if result.tool_calls:
            tool_calls = [
                ToolCall(
                    id=tc.id,
                    function=ToolCallFunction(name=tc.name, arguments=tc.arguments or "{}"),
                )
                for tc in result.tool_calls
            ]
        return RunHistoryMessage(
            role="assistant",
            name=name,
            content=result.content or ("" if tool_calls else ""),
            tool_calls=tool_calls,
        )


def _public_realtime_data(event_type: str, data: Any) -> dict[str, Any] | None:
    """Keep the live room useful without exposing private RunHistory/tool payloads."""
    raw = data if isinstance(data, dict) else {}
    if event_type == "human":
        return None
    if event_type == "queued":
        return {
            key: raw[key]
            for key in ("queue_id", "position", "text", "from_node_id", "client_message_id", "queue")
            if key in raw
        }
    if event_type == "queue":
        queue = raw.get("queue")
        return {"queue": queue} if isinstance(queue, dict) else {"queue": {}}
    if event_type == "status":
        return {
            key: raw[key]
            for key in ("phase", "purpose", "mode", "member_id")
            if key in raw
        }
    if event_type == "delta":
        return {
            key: raw[key]
            for key in ("text", "mode", "member_id")
            if key in raw
        }
    if event_type == "assistant_final":
        # The final text is committed to Timeline immediately before this event;
        # subscribers receive it through the reliable timeline channel.
        return None
    if event_type == "final":
        loop = raw.get("loop") if isinstance(raw.get("loop"), dict) else {}
        return {
            "mode": raw.get("mode"),
            "member_id": raw.get("member_id"),
            "assign_id": raw.get("assign_id"),
            "status": loop.get("status"),
            "steps": loop.get("steps"),
            "final_text": loop.get("final_text"),
        }
    return None


def mock_leader_script_assign_then_answer(
    *,
    member_id: str,
    instruction: str = "读 README",
    final_text: str = "已完成",
) -> list[ChatResult]:
    """测试用：第一步 assign，第二步终态文本。"""
    import json

    args = json.dumps({"member_id": member_id, "instruction": instruction}, ensure_ascii=False)
    return [
        ChatResult(
            content="",
            tool_calls=[
                ChatToolCall(
                    id="call_as1",
                    name="assign_workgroup_task",
                    arguments=args,
                )
            ],
            finish_reason="tool_calls",
        ),
        ChatResult(content=final_text, finish_reason="stop"),
    ]


def mock_member_script_read_file_then_answer(
    *,
    path: str = "README",
    call_purpose: str = "",
    first_content: str = "",
    final_text: str = "已读完",
) -> list[ChatResult]:
    """测试用：Member 先 read_file，再终态文本。"""
    import json

    payload = {"path": path}
    if call_purpose:
        payload["call_purpose"] = call_purpose
    args = json.dumps(payload, ensure_ascii=False)
    return [
        ChatResult(
            content=first_content,
            tool_calls=[
                ChatToolCall(
                    id="call_rf1",
                    name="read_file",
                    arguments=args,
                )
            ],
            finish_reason="tool_calls",
        ),
        ChatResult(content=final_text, finish_reason="stop"),
    ]
