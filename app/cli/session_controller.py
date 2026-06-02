from __future__ import annotations

import asyncio
from collections.abc import Awaitable, Callable
from contextlib import suppress
from dataclasses import dataclass
from typing import Any, Literal

from app.cli.api_client import DAgentsApiClient, StreamEvent
from app.cli.approval import (
    ApprovalDecision,
    build_approval_resume,
)
from app.cli.child_agent import (
    ChildAgentTracker,
    child_session_id_from_data,
    format_child_lifecycle_line,
    should_skip_child_runtime_display,
)
from app.cli.user_information import (
    UserInformationAnswer,
    UserInformationCancelled,
    UserInformationRequest,
    extract_user_information_request,
)
from app.cli.render import (
    TranscriptKind,
    TranscriptUpdate,
    format_assistant_delta,
    format_assistant_end,
    format_context_compression,
    format_error,
    format_reasoning,
    format_system_line,
    format_tool_call,
    format_tool_result,
    format_user_information_required,
)

TranscriptCallback = Callable[[TranscriptUpdate], None]
UserInformationCallback = Callable[[UserInformationRequest], Awaitable[UserInformationAnswer]]
StatusCallback = Callable[[str], None]
HitlPendingCallback = Callable[[], None]
ChildStripCallback = Callable[[], None]


@dataclass
class PendingHITL:
    """待 UI 处理的 HITL 项（审批或用户询问）。"""

    kind: Literal["approval", "user_information"]
    data: dict[str, Any]


class SessionController:
    """CLI/TUI 会话控制器：SSE 泵、后台事件渲染、用户轮次栅栏。

    逻辑：
    1. `start` 建立 API 连接、创建 session、启动 pump 与 render 循环；
    2. render 循环持续消费 SSE 并回调 transcript；用户 submit 后 `wait_user_turn` 等待本轮 done；
    3. `stop` 取消后台任务并关闭 HTTP 客户端。

    关键分支：
    - 用户 submit 后若 session 仍有在途 turn（如 trigger），先忽略其 done，直到出现 submit 后的内容事件再认 turn 边界；
    - approval 后 skip 一条 done（编排层暂停 done），与旧 REPL 行为一致。
    """

    def __init__(
        self,
        *,
        api_base: str,
        session_id: str | None,
        show_reasoning: bool,
        config_path: str | None = None,
    ) -> None:
        self.api_base = api_base
        self.config_path = config_path
        self.initial_session_id = session_id
        self.show_reasoning = show_reasoning
        self.session_id = ""
        self._client: DAgentsApiClient | None = None
        self._events: asyncio.Queue[StreamEvent] = asyncio.Queue()
        self._stream_task: asyncio.Task[None] | None = None
        self._render_task: asyncio.Task[None] | None = None
        self._transcript_cb: TranscriptCallback | None = None
        self._user_information_cb: UserInformationCallback | None = None
        self._status_cb: StatusCallback | None = None
        self._hitl_pending_cb: HitlPendingCallback | None = None
        self._child_strip_cb: ChildStripCallback | None = None
        self._done_counter = 0
        self._user_turn_done = asyncio.Event()
        self._awaiting_user_turn = False
        self._user_turn_started = False
        self._submit_pending_marker = False
        self._assistant_line_open = False
        self._sse_connected = False
        self._hitl_queue: list[PendingHITL] = []
        self._skip_next_done = False
        self._child_tracker = ChildAgentTracker()
        self._last_event_seq = 0
        self._turn_seq_fence = 0
        self._sse_ready = asyncio.Event()

    def _event_seq(self, event: StreamEvent) -> int:
        """从 SSE id 或 envelope.seq 解析事件序号。"""
        if event.event_id:
            try:
                return int(event.event_id)
            except ValueError:
                pass
        raw = event.payload.get("seq")
        if isinstance(raw, int):
            return raw
        try:
            return int(raw)
        except (TypeError, ValueError):
            return 0

    def _is_stale_stream_event(self, event: StreamEvent) -> bool:
        """submit 之后仍可能收到 replay 的历史事件，须忽略 turn 栅栏与 transcript。"""
        seq = self._event_seq(event)
        if seq > 0:
            self._last_event_seq = max(self._last_event_seq, seq)
        return seq > 0 and seq <= self._turn_seq_fence

    def on_transcript(self, callback: TranscriptCallback) -> None:
        """注册 transcript 更新回调。"""
        self._transcript_cb = callback

    def on_hitl_pending(self, callback: HitlPendingCallback) -> None:
        """HITL 入队后通知 UI 处理（非阻塞，避免丢 SSE）。"""
        self._hitl_pending_cb = callback

    def on_child_strip(self, callback: ChildStripCallback) -> None:
        """子 Agent 状态条变更时通知 UI 刷新。"""
        self._child_strip_cb = callback

    def on_user_information(self, callback: UserInformationCallback) -> None:
        """注册用户询问回调（由 UI 层收集回答）。"""
        self._user_information_cb = callback

    def on_status(self, callback: StatusCallback) -> None:
        """注册状态栏文本回调。"""
        self._status_cb = callback

    @property
    def sse_connected(self) -> bool:
        return self._sse_connected

    async def start(self) -> None:
        """连接 Agent Node 并启动 SSE pump 与 render 循环。"""
        self._client = DAgentsApiClient(self.api_base)
        if not await self._client.health():
            raise RuntimeError(f"node health check failed: {self.api_base}/health")
        self._sse_ready.clear()
        self.session_id = await self._client.create_session(self.initial_session_id)
        self._stream_task = asyncio.create_task(self._pump_stream())
        self._render_task = asyncio.create_task(self._render_loop())
        try:
            await asyncio.wait_for(self._sse_ready.wait(), timeout=15.0)
        except TimeoutError as exc:
            raise RuntimeError("SSE subscription timed out") from exc
        self._emit_status()

    async def stop(self) -> None:
        """停止后台任务并关闭 API 客户端。"""
        for task in (self._render_task, self._stream_task):
            if task is not None:
                task.cancel()
                with suppress(asyncio.CancelledError):
                    await task
        if self._client is not None:
            await self._client.close()
            self._client = None

    async def submit_message(self, content: str) -> None:
        """投递用户消息并标记等待本轮 turn 完成。"""
        assert self._client is not None
        if not self._sse_ready.is_set():
            raise RuntimeError("SSE not ready")
        self._reset_user_turn_wait()
        await self._client.submit_message(
            session_id=self.session_id,
            content=content,
        )

    async def wait_user_turn(self, *, timeout_seconds: float = 300.0) -> None:
        """阻塞直到本轮用户消息对应的 turn 收到 done（含 approval skip 语义）。"""
        if not self._awaiting_user_turn:
            return
        try:
            await asyncio.wait_for(self._user_turn_done.wait(), timeout=timeout_seconds)
        except TimeoutError as exc:
            self._awaiting_user_turn = False
            self._user_turn_done.set()
            raise RuntimeError(
                f"turn timed out after {int(timeout_seconds)}s (no done event; check SSE / node logs)"
            ) from exc

    async def clear_context(self) -> dict[str, Any]:
        """清空当前 session 的服务端对话上下文。

        逻辑：
        1. 调用 `POST /v1/sessions/{session_id}/clear-context`；
        2. 返回 API 响应 dict。

        关键边界：
        - 需已 `start()` 且 `session_id` 非空。
        """
        assert self._client is not None
        return await self._client.clear_session_context(self.session_id)

    async def list_sessions(self) -> dict[str, Any]:
        """查询后端 session 列表（GET /v1/sessions）。"""
        assert self._client is not None
        return await self._client.list_sessions()

    async def get_context(self) -> dict[str, Any]:
        """查询当前 session 的 context 摘要。

        逻辑：
        1. 确认 controller 已 `start()` 并持有 API client；
        2. 使用当前 `session_id` 调用 context API；
        3. 原样返回后端摘要，由 TUI 负责格式化。

        关键边界：
        - 不修改本地 turn 栅栏与 SSE 状态，仅用于 `/context` 只读视图。
        """
        assert self._client is not None
        return await self._client.get_session_context(self.session_id)

    async def cancel_current_turn(self) -> dict[str, Any]:
        """取消当前 session 的在途 turn，并解除本地用户轮次等待。

        逻辑：
        1. 调用服务端 cancel 接口；
        2. 无论服务端是否存在 active turn，都结束本地 wait_user_turn；
        3. 返回 API 响应，供 UI 按需展示。

        关键边界：
        - 审批等待发生在 CLI render loop 内，服务端可能已无 active turn；本地仍需解除等待。
        """
        assert self._client is not None
        result = await self._client.cancel_current_turn(self.session_id)
        self._awaiting_user_turn = False
        self._submit_pending_marker = False
        self._user_turn_started = False
        self._user_turn_done.set()
        return result

    async def list_skills(self) -> dict[str, Any]:
        """查询当前会话 loaded skills 与后端可用 skills。"""
        assert self._client is not None
        return await self._client.list_session_skills(self.session_id)

    async def load_skill(self, skill_name: str) -> dict[str, Any]:
        """向当前会话加载一个 skill。"""
        assert self._client is not None
        return await self._client.load_session_skill(self.session_id, skill_name)

    async def unload_skill(self, skill_name: str) -> dict[str, Any]:
        """从当前会话卸载一个 skill。"""
        assert self._client is not None
        return await self._client.unload_session_skill(self.session_id, skill_name)

    async def list_child_agents(self) -> list[dict[str, Any]]:
        """查询当前父 session 下活跃子 Agent 列表。"""
        assert self._client is not None
        return await self._client.list_child_agents(self.session_id)

    @property
    def child_tracker(self) -> ChildAgentTracker:
        return self._child_tracker

    def hitl_queue_len(self) -> int:
        return len(self._hitl_queue)

    def peek_hitl(self) -> PendingHITL | None:
        """返回队首待处理 HITL，不弹出。"""
        return self._hitl_queue[0] if self._hitl_queue else None

    def discard_hitl_head(self) -> None:
        """取消审批/询问时丢弃队首，不 submit resume。"""
        if not self._hitl_queue:
            return
        item = self._hitl_queue.pop(0)
        if item.kind == "approval":
            child_id = child_session_id_from_data(item.data)
            if child_id:
                self._child_tracker.set_awaiting_approval(child_id, False)
                self._emit_child_strip()
        self._notify_hitl_pending()

    def clear_hitl_queue(self) -> None:
        """Esc 取消 turn 时清空 HITL 队列并重置子 Agent 待审批标记。"""
        for entry in self._child_tracker.entries.values():
            entry.awaiting_approval = False
        self._hitl_queue.clear()
        self._emit_child_strip()
        self._notify_hitl_pending()

    async def complete_hitl_approval(self, decision: ApprovalDecision) -> None:
        """提交审批 resume 并弹出队首；编排层随后会 emit 一条可 skip 的 done。"""
        if not self._hitl_queue or self._hitl_queue[0].kind != "approval":
            return
        data = self._hitl_queue[0].data
        child_id = child_session_id_from_data(data)
        if child_id:
            self._child_tracker.set_awaiting_approval(child_id, False)
            self._emit_child_strip()
        self._hitl_queue.pop(0)
        assert self._client is not None
        await self._client.submit_resume(
            session_id=self.session_id,
            resume_value=build_approval_resume(data, decision),
        )
        self._skip_next_done = True
        self._notify_hitl_pending()

    async def complete_hitl_user_info(self, answer: UserInformationAnswer) -> None:
        """提交用户询问 resume 并弹出队首。"""
        if not self._hitl_queue or self._hitl_queue[0].kind != "user_information":
            return
        self._hitl_queue.pop(0)
        assert self._client is not None
        await self._client.submit_resume(
            session_id=self.session_id,
            resume_value=answer.to_resume_value(),
        )
        self._skip_next_done = True
        self._notify_hitl_pending()

    def reset_child_state(self) -> None:
        """切换 session 或清屏时重置子 Agent 跟踪与 HITL 队列。"""
        self._child_tracker.reset()
        self._hitl_queue.clear()
        self._emit_child_strip()

    def _reset_user_turn_wait(self) -> None:
        """在用户 submit 后重置 turn 栅栏状态。"""
        self._awaiting_user_turn = True
        self._user_turn_started = False
        self._submit_pending_marker = True
        self._turn_seq_fence = self._last_event_seq
        self._user_turn_done.clear()

    def _emit_transcript(self, update: TranscriptUpdate) -> None:
        if self._transcript_cb is not None:
            self._transcript_cb(update)

    def _emit_status(self) -> None:
        if self._status_cb is None:
            return
        sse = "connected" if self._sse_connected else "disconnected"
        parts = [f"api={self.api_base}"]
        if self.config_path:
            parts.append(f"config={self.config_path}")
        parts.extend(
            [
                f"session={self.session_id}",
                f"sse={sse}",
            ]
        )
        self._status_cb(" ".join(parts))

    def _emit_child_strip(self) -> None:
        if self._child_strip_cb is not None:
            self._child_strip_cb()

    def _notify_hitl_pending(self) -> None:
        if self._hitl_pending_cb is not None:
            self._hitl_pending_cb()

    def _mark_user_turn_content_seen(self) -> None:
        """submit 之后首次见到 assistant/tool 等内容事件，标记用户 turn 已开始。"""
        if self._submit_pending_marker:
            self._submit_pending_marker = False
            self._user_turn_started = True

    async def _pump_stream(self) -> None:
        """后台 SSE 读取，过滤 session 后入队。"""
        assert self._client is not None
        try:
            stream = self._client.stream_events(session_id=self.session_id)
            async for event in stream:
                if event.is_stream_ready:
                    self._sse_connected = True
                    self._sse_ready.set()
                    self._emit_status()
                    await self._sync_child_agents_from_api()
                    continue
                if event.session_id and event.session_id != self.session_id:
                    continue
                await self._events.put(event)
        except asyncio.CancelledError:
            raise
        except Exception as exc:
            self._sse_connected = False
            self._emit_status()
            await self._events.put(
                StreamEvent(
                    event_type="error",
                    event_id=None,
                    payload={
                        "session_id": self.session_id,
                        "data": {"message": f"SSE stream failed: {exc}"},
                    },
                )
            )
            await self._events.put(
                StreamEvent(
                    event_type="done",
                    event_id=None,
                    payload={"session_id": self.session_id, "data": {}},
                )
            )
            if not self._sse_ready.is_set():
                self._sse_ready.set()
        finally:
            self._sse_connected = False
            self._emit_status()

    async def _render_loop(self) -> None:
        """持续消费 SSE 队列并分发 transcript / approval / turn 栅栏。"""
        skip_next_done = False
        while True:
            event = await self._events.get()
            await self._handle_stream_event(event, skip_next_done_holder := {"v": skip_next_done})
            skip_next_done = skip_next_done_holder["v"]

    async def _handle_stream_event(self, event: StreamEvent, skip_holder: dict[str, bool]) -> None:
        """处理单条 SSE 事件并更新 UI / turn 状态。"""
        if event.is_stream_ready:
            return
        if self._is_stale_stream_event(event):
            return
        data = event.data
        event_type = event.event_type

        if should_skip_child_runtime_display(event_type, data):
            return

        if event_type in {"child_agent_created", "child_agent_completed", "child_agent_cancelled"}:
            self._handle_child_lifecycle(event_type, data)
            return

        if event_type in {"assistant", "tool_call", "tool_result", "reasoning"}:
            self._mark_user_turn_content_seen()

        if event_type == "assistant":
            content = str(data.get("content") or "")
            if content:
                self._emit_transcript(format_assistant_delta(content))
                self._assistant_line_open = True
        elif event_type == "reasoning":
            content = str(data.get("content") or "")
            if content:
                self._ensure_assistant_end()
                self._emit_transcript(format_reasoning(content))
        elif event_type == "tool_call":
            self._ensure_assistant_end()
            formatted = format_tool_call(data)
            if formatted is not None:
                self._emit_transcript(formatted)
        elif event_type == "approval_required":
            self._ensure_assistant_end()
            self._enqueue_hitl(PendingHITL(kind="approval", data=data))
        elif event_type == "user_information_required":
            self._ensure_assistant_end()
            self._enqueue_hitl(PendingHITL(kind="user_information", data=data))
        elif event_type == "tool_result":
            self._ensure_assistant_end()
            self._emit_transcript(format_tool_result(data))
        elif event_type in {"context_compression_blocking", "context_compression_silent"}:
            self._ensure_assistant_end()
            self._emit_transcript(format_context_compression(event_type, data))
        elif event_type == "error":
            self._ensure_assistant_end()
            message = str(data.get("message") or "unknown error")
            self._emit_transcript(format_error(message))
            if self._awaiting_user_turn:
                self._awaiting_user_turn = False
                self._user_turn_done.set()
        elif event_type == "done":
            self._ensure_assistant_end()
            self._done_counter += 1
            if skip_holder["v"] or self._skip_next_done:
                skip_holder["v"] = False
                self._skip_next_done = False
            elif self._awaiting_user_turn:
                seq = self._event_seq(event)
                # seq > fence 表示 submit 之后的新 turn 已结束（即使 assistant 增量被漏收）。
                if self._user_turn_started or seq > self._turn_seq_fence:
                    self._awaiting_user_turn = False
                    self._user_turn_done.set()

    def _enqueue_hitl(self, item: PendingHITL) -> None:
        """HITL 入队并通知 UI；子审批时标记 awaiting_approval。"""
        if item.kind == "approval":
            child_id = child_session_id_from_data(item.data)
            if child_id:
                self._child_tracker.set_awaiting_approval(child_id, True)
                self._emit_child_strip()
        self._hitl_queue.append(item)
        self._notify_hitl_pending()

    def _handle_child_lifecycle(self, event_type: str, data: dict[str, Any]) -> None:
        """处理子 Agent 生命周期 SSE，写入系统行并更新 tracker。"""
        if event_type == "child_agent_created":
            self._child_tracker.on_created(data)
        else:
            self._child_tracker.on_finished(child_session_id_from_data(data))
        self._emit_child_strip()
        line = format_child_lifecycle_line(event_type, data)
        if line:
            self._emit_transcript(format_system_line(line))

    async def _sync_child_agents_from_api(self) -> None:
        """SSE 重连后 HTTP 对齐子 Agent 列表。"""
        assert self._client is not None
        try:
            items = await self._client.list_child_agents(self.session_id)
            self._child_tracker.replace_from_api(items)
            self._emit_child_strip()
        except Exception:
            pass

    def _ensure_assistant_end(self) -> None:
        """assistant 流式段与块级事件之间补换行。"""
        if self._assistant_line_open:
            self._emit_transcript(format_assistant_end())
            self._assistant_line_open = False
