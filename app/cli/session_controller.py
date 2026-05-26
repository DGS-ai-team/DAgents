from __future__ import annotations

import asyncio
from collections.abc import Awaitable, Callable
from contextlib import suppress
from typing import Any
from uuid import uuid4

from app.cli.api_client import DAgentsApiClient, StreamEvent
from app.cli.approval import (
    ApprovalCancelled,
    ApprovalDecision,
    ToolApprovalRequest,
    build_all_rejected_decision,
    extract_tool_approval_requests,
)
from app.cli.render import (
    TranscriptUpdate,
    format_assistant_delta,
    format_assistant_end,
    format_error,
    format_reasoning,
    format_tool_call,
    format_tool_result,
)

TranscriptCallback = Callable[[TranscriptUpdate], None]
ApprovalCallback = Callable[[list[ToolApprovalRequest]], Awaitable[ApprovalDecision]]
StatusCallback = Callable[[str], None]


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
        client_id: str | None,
        show_reasoning: bool,
    ) -> None:
        self.api_base = api_base
        self.initial_session_id = session_id
        self.client_id = client_id or f"cli-{uuid4().hex[:12]}"
        self.show_reasoning = show_reasoning
        self.session_id = ""
        self._client: DAgentsApiClient | None = None
        self._events: asyncio.Queue[StreamEvent] = asyncio.Queue()
        self._stream_task: asyncio.Task[None] | None = None
        self._render_task: asyncio.Task[None] | None = None
        self._transcript_cb: TranscriptCallback | None = None
        self._approval_cb: ApprovalCallback | None = None
        self._status_cb: StatusCallback | None = None
        self._done_counter = 0
        self._user_turn_done = asyncio.Event()
        self._awaiting_user_turn = False
        self._user_turn_started = False
        self._submit_pending_marker = False
        self._assistant_line_open = False
        self._sse_connected = False
        self._approval_lock = asyncio.Lock()

    def on_transcript(self, callback: TranscriptCallback) -> None:
        """注册 transcript 更新回调。"""
        self._transcript_cb = callback

    def on_approval(self, callback: ApprovalCallback) -> None:
        """注册工具审批回调（由 UI 层实现交互）。"""
        self._approval_cb = callback

    def on_status(self, callback: StatusCallback) -> None:
        """注册状态栏文本回调。"""
        self._status_cb = callback

    @property
    def sse_connected(self) -> bool:
        return self._sse_connected

    async def start(self) -> None:
        """连接后端并启动 SSE pump 与 render 循环。"""
        self._client = DAgentsApiClient(self.api_base)
        if not await self._client.health():
            raise RuntimeError(f"backend health check failed: {self.api_base}/health")
        self.session_id = await self._client.create_session(self.initial_session_id)
        self._stream_task = asyncio.create_task(self._pump_stream())
        self._render_task = asyncio.create_task(self._render_loop())
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
        self._reset_user_turn_wait()
        await self._client.submit_message(
            session_id=self.session_id,
            client_id=self.client_id,
            content=content,
        )

    async def wait_user_turn(self) -> None:
        """阻塞直到本轮用户消息对应的 turn 收到 done（含 approval skip 语义）。"""
        if not self._awaiting_user_turn:
            return
        await self._user_turn_done.wait()

    async def bind_triggers_to_client(self) -> int:
        """将当前 session 下 client_id 缺失或不匹配的 trigger 绑定到本 CLI client_id。

        Returns:
            成功 PATCH 的 trigger 数量。
        """
        assert self._client is not None
        result = await self._client.list_triggers()
        triggers = result.get("triggers")
        if not isinstance(triggers, list):
            return 0
        bound = 0
        for raw in triggers:
            if not isinstance(raw, dict):
                continue
            trigger_id = str(raw.get("trigger_id") or "").strip()
            target_session = str(raw.get("target_session_id") or "").strip()
            existing_client = str(raw.get("client_id") or "").strip()
            if not trigger_id:
                continue
            if target_session and target_session != self.session_id:
                continue
            if existing_client == self.client_id:
                continue
            await self._client.patch_trigger(
                trigger_id,
                {"target_session_id": self.session_id, "client_id": self.client_id},
            )
            bound += 1
        return bound

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
        """查询后端当前 session 列表。

        逻辑：
        1. 调用 `GET /v1/sessions`；
        2. 原样返回 API 响应，UI 层负责选择展示 active/persisted。

        关键边界：
        - 需已 `start()`，否则 `_client` 不存在。
        """
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

    def _reset_user_turn_wait(self) -> None:
        """在用户 submit 后重置 turn 栅栏状态。"""
        self._awaiting_user_turn = True
        self._user_turn_started = False
        self._submit_pending_marker = True
        self._user_turn_done.clear()

    def _emit_transcript(self, update: TranscriptUpdate) -> None:
        if self._transcript_cb is not None:
            self._transcript_cb(update)

    def _emit_status(self) -> None:
        if self._status_cb is None:
            return
        sse = "connected" if self._sse_connected else "disconnected"
        self._status_cb(
            f"api={self.api_base} session={self.session_id} client={self.client_id} sse={sse}"
        )

    def _mark_user_turn_content_seen(self) -> None:
        """submit 之后首次见到 assistant/tool 等内容事件，标记用户 turn 已开始。"""
        if self._submit_pending_marker:
            self._submit_pending_marker = False
            self._user_turn_started = True

    async def _pump_stream(self) -> None:
        """后台 SSE 读取，过滤 session 后入队。"""
        assert self._client is not None
        self._sse_connected = True
        self._emit_status()
        try:
            async for event in self._client.stream_events(client_id=self.client_id):
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
        data = event.data
        event_type = event.event_type

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
            skip_holder["v"] = await self._handle_approval(data)
        elif event_type == "tool_result":
            self._ensure_assistant_end()
            self._emit_transcript(format_tool_result(data))
        elif event_type == "error":
            self._ensure_assistant_end()
            message = str(data.get("message") or "unknown error")
            self._emit_transcript(format_error(message))
        elif event_type == "done":
            self._ensure_assistant_end()
            self._done_counter += 1
            if skip_holder["v"]:
                skip_holder["v"] = False
            elif self._awaiting_user_turn and self._user_turn_started:
                self._awaiting_user_turn = False
                self._user_turn_done.set()

    async def _handle_approval(self, data: dict[str, Any]) -> bool:
        """处理 approval_required：回调 UI 审批并 submit resume；返回是否 skip 下一条 done。"""
        async with self._approval_lock:
            requests = extract_tool_approval_requests(data)
            if self._approval_cb is not None:
                try:
                    decision = await self._approval_cb(requests)
                except ApprovalCancelled:
                    return False
            else:
                decision = build_all_rejected_decision(requests)
            assert self._client is not None
            await self._client.submit_resume(
                session_id=self.session_id,
                client_id=self.client_id,
                resume_value=decision.to_resume_value(),
            )
            return True

    def _ensure_assistant_end(self) -> None:
        """assistant 流式段与块级事件之间补换行。"""
        if self._assistant_line_open:
            self._emit_transcript(format_assistant_end())
            self._assistant_line_open = False
