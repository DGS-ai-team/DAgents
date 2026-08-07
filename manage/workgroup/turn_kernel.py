"""Manage Turn Kernel：Leader / Member LLM loop + Assign / Projector / HITL 门禁。"""

from __future__ import annotations

import json
import threading
from collections.abc import Callable
from typing import Any

from manage.llm.store import LLMConfigStore
from manage.workgroup.builtin_hooks import (
    TODAY_DATE_MESSAGE_NAME,
    ensure_today_date_in_messages,
    package_tool_result,
)
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
    host_env_from_registry,
    member_openai_tools,
)
from manage.workgroup.mentions import resolve_direct_member
from manage.workgroup.models import (
    ActorRun,
    ActorRunCreateRequest,
    Assign,
    AssignCreateRequest,
    WorkGroup,
)
from manage.workgroup.native_tools import AssignCompleter, NativeToolDispatcher, leader_native_tools
from manage.workgroup.projector import project_actor_context
from manage.workgroup.protocol_names import protocol_name_for_actor
from manage.workgroup.store import WorkGroupStore


_DEFAULT_MAX_TOOL_LOOPS = 16

_TOOL_LOOP_LIMIT_EXCEEDED_MESSAGE = (
    "已超过单轮工具调用次数，请先给出当前结论以及进度，"
    "询问用户是否要继续后续的推进，下一轮开始时工具累计次数会重置。"
)

_LEADER_SYSTEM_RULES = (
    "你是工作组 Leader（Supervisor）。"
    "你只通过 Manage 侧编排工具进行协调，绝不亲自执行 shell / 文件系统 / 浏览器操作。"
    "用 list_workgroup_members 查看成员状态与工具白名单。"
    "用 assign_workgroup_task 把实际工作委派给就绪成员；"
    "成员会跑自己的 LLM 循环，并调用自己的工具完成你发布的任务"
    "指令写清楚；不要编造宿主机绝对路径——"
    "发布任务时，请写清楚任务内容，注意事项，以及结论的结构要求，如果你有任务的路径要求，也一并写清楚，特别是成员过去没有成功执行的任务。"
    "成员沙箱只允许工作区相对路径"
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
    ) -> None:
        self._store = store
        self._llm_store = llm_store
        self._chat_client = chat_client
        self._member_chat_client = member_chat_client
        self._assign_completer = assign_completer
        self._registry_store = registry_store
        self._max_tool_loops = max(1, max_tool_loops)
        self._mock_llm = mock_llm
        self._hitl_resolutions: dict[str, dict[str, Any]] = {}
        # workgroup_id -> cancel flag（用户中断当前 turn）
        self._cancel_flags: dict[str, threading.Event] = {}
        self._active_turn: dict[str, dict[str, Any]] = {}
        self._turn_lock = threading.Lock()
        # workgroup_id -> FIFO human 队列（进程内；对齐 Node MessageQueue 单飞）
        self._human_queues: dict[str, list[QueuedHuman]] = {}
        self._command_cancel_hook: Callable[[str], None] | None = None

    def set_assign_completer(self, completer: AssignCompleter | None) -> None:
        self._assign_completer = completer

    def set_command_cancel_hook(self, hook: Callable[[str], None] | None) -> None:
        """cancel_turn 时唤醒 VerticalLoop.wait_command_result（合成 canceled）。"""
        self._command_cancel_hook = hook

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
            self._active_turn[workgroup_id] = {"mode": mode, "turn_token": token, **meta}
        return flag

    def _end_turn(self, workgroup_id: str, *, turn_token: str | None = None) -> QueuedHuman | None:
        """结束当前 turn；若队列非空则在同一把锁内认领下一条并返回。"""
        with self._turn_lock:
            cur = self._active_turn.get(workgroup_id)
            if cur is not None:
                if turn_token and str(cur.get("turn_token") or "") != str(turn_token):
                    return None
                self._active_turn.pop(workgroup_id, None)
            flag = self._cancel_flags.get(workgroup_id)
            if flag is not None:
                flag.clear()
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
                "queue_id": item.queue_id,
            }
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
        if nxt is not None:
            self._schedule_queued_human(nxt)

    def _update_turn(self, workgroup_id: str, **meta: Any) -> None:
        with self._turn_lock:
            cur = self._active_turn.get(workgroup_id)
            if cur is not None:
                cur.update(meta)

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
                return item.to_public(idx + 1)
        raise WorkgroupError("not_found", "queued message not found", http_status=404)

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
                return {"cancelled": True, "queue_id": qid, "depth": len(items)}
        raise WorkgroupError("not_found", "queued message not found", http_status=404)

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
                return "queued", item, pos
            token = wg_ids.new_ulid()
            self._active_turn[workgroup_id] = {
                "mode": "claiming",
                "turn_token": token,
                "queue_id": item.queue_id,
            }
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
            failed_ids = self._store.fail_active_assigns(
                workgroup_id,
                reason="cancelled by user",
                error_code="canceled",
            )
            try:
                self._store.cancel_pending_hitls(workgroup_id)
            except Exception:  # noqa: BLE001
                pass
            if self._command_cancel_hook is not None:
                try:
                    self._command_cancel_hook(workgroup_id)
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
        failed_ids = self._store.fail_active_assigns(
            workgroup_id,
            reason="cancelled by user",
            error_code="canceled",
        )
        try:
            self._store.cancel_pending_hitls(workgroup_id)
        except Exception:  # noqa: BLE001
            pass
        if self._command_cancel_hook is not None:
            try:
                self._command_cancel_hook(workgroup_id)
            except Exception:  # noqa: BLE001
                pass
        leader_run_id = meta.get("leader_run_id")
        member_run_id = meta.get("member_run_id")
        for rid in (leader_run_id, member_run_id):
            if not rid:
                continue
            try:
                self._heal_open_tool_calls(str(rid), reason="turn cancelled by user")
                run = self._store.get_actor_run(str(rid))
                if run and run.status in {"running", "awaiting_hitl"}:
                    self._store.update_actor_run(str(rid), status="canceled")
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
        }

    def start_leader_run(self, workgroup_id: str, *, llm_profile_revision: str | None = None) -> ActorRun:
        return self._store.create_actor_run(
            workgroup_id,
            ActorRunCreateRequest(actor_id="leader", llm_profile_revision=llm_profile_revision),
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
        )

    def resolve_hitl_cas(
        self,
        hitl_id: str,
        *,
        expected_status: str = "pending",
        resolution: dict[str, Any],
    ) -> dict[str, Any]:
        """HITL 乐观 CAS 占位：同 id 二次决议 → already_resolved。"""
        existing = self._hitl_resolutions.get(hitl_id)
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
        self._hitl_resolutions[hitl_id] = stored
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
            yield {
                "event": "queued",
                "data": {
                    **item.to_public(position),
                    "queue": self.list_human_queue(workgroup_id),
                },
            }
            return

        with self._turn_lock:
            token = str((self._active_turn.get(workgroup_id) or {}).get("turn_token") or "")
        yield from self._execute_human_turn_events(item, turn_token=token)

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
            )
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
                )
                return

            self._begin_turn(
                workgroup_id, mode="leader", turn_token=turn_token, queue_id=item.queue_id
            )
            run = self._store.find_running_leader_run(workgroup_id) or self.start_leader_run(
                workgroup_id
            )
            self._update_turn(workgroup_id, leader_run_id=run.run_id)
            self._store.ensure_run_history(run)
            loop_result: dict[str, Any] | None = None
            try:
                for ev in self.run_leader_until_idle_events(
                    workgroup_id, run.run_id, disable_tools=item.disable_tools
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
    ):
        """@直连：跳过 Leader LLM，创建 Assign + Member run。"""
        mid = member.member_id
        brief = instruction.replace("\n", " ").strip()
        if len(brief) > 96:
            brief = brief[:93] + "…"

        begin_kwargs: dict[str, Any] = {"member_id": mid}
        if turn_token:
            begin_kwargs["turn_token"] = turn_token
        self._begin_turn(workgroup_id, mode="direct", **begin_kwargs)
        yield {"event": "status", "data": {"phase": "tool", "tool": "直达成员", "mode": "direct"}}

        tool_call_id = "call_direct_1"
        assign = self._store.create_assign(
            workgroup_id,
            AssignCreateRequest(
                member_id=mid,
                instruction=instruction,
                leader_tool_call_id=tool_call_id,
            ),
        )
        self._store.set_assign_status(assign.assign_id, "running")
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

    def run_leader_until_idle(self, workgroup_id: str, run_id: str, *, disable_tools: bool = False) -> dict[str, Any]:
        for ev in self.run_leader_until_idle_events(workgroup_id, run_id, disable_tools=disable_tools):
            if ev.get("event") == "loop_final":
                return ev["data"]
        raise WorkgroupError("conflict", "leader loop produced no result", http_status=500)

    def run_leader_until_idle_events(self, workgroup_id: str, run_id: str, *, disable_tools: bool = False):
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

            projected = project_actor_context(
                actor_id="leader",
                run=run,
                timeline_events=self._store.list_timeline(workgroup_id),
                own_run_history=hist.messages,
            )
            group = self._store.require_active(workgroup_id)
            system = build_leader_system_prompt(workgroup=group)
            messages = [{"role": "system", "content": system}] + list(projected["messages"])
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

            tool_msgs: list[RunHistoryMessage] = []
            for tc in result.tool_calls:
                tool_label = tc.name
                if tc.name == "assign_workgroup_task":
                    tool_label = "成员执行任务"
                elif tc.name == "ask_workgroup_user":
                    tool_label = "询问用户"
                yield {
                    "event": "status",
                    "data": {
                        "phase": "tool",
                        "tool": tool_label,
                        "tool_name": tc.name,
                        "tool_call_id": tc.id,
                    },
                }
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
                tool_msgs.append(
                    RunHistoryMessage(
                        role="tool",
                        tool_call_id=tc.id,
                        name=tc.name,
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

            projected = project_actor_context(
                actor_id=member_id,
                run=run,
                member=member,
                timeline_events=self._store.list_timeline(workgroup_id),
                own_run_history=hist.messages,
            )
            messages = [{"role": "system", "content": system}] + list(projected["messages"])
            messages = self._apply_today_date_hook(run_id, messages)
            over_budget = tool_loops >= max_loops
            step_tools: list[dict[str, Any]] = [] if over_budget else list(tools)
            result = client.chat(messages, tools=step_tools or None)
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
                # 轻量进度：公开 Timeline 只写工具名，不含参数/结果；名字由 UI 按 actor 聚合展示
                try:
                    hint = name
                    if name in {
                        "read_file",
                        "write_file",
                        "glob_files",
                        "grep_file",
                        "grep_files",
                        "search_replace",
                        "show_image",
                        "read_image",
                        "bash_run",
                    }:
                        try:
                            args = json.loads(tc.arguments or "{}")
                        except (TypeError, json.JSONDecodeError):
                            args = {}
                        path = str(
                            args.get("path")
                            or args.get("directory")
                            or args.get("command")
                            or args.get("pattern")
                            or ""
                        ).strip()
                        if path:
                            if len(path) > 48:
                                path = path[:45] + "…"
                            hint = f"{name} · {path}"
                    self._store.append_timeline(
                        workgroup_id,
                        type="system_notice",
                        actor_id=member_id,
                        text=hint,
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

    def _heal_open_tool_calls(self, run_id: str, *, reason: str) -> list[str]:
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
    final_text: str = "已读完",
) -> list[ChatResult]:
    """测试用：Member 先 read_file，再终态文本。"""
    import json

    args = json.dumps({"path": path}, ensure_ascii=False)
    return [
        ChatResult(
            content="",
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
