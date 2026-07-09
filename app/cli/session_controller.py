from __future__ import annotations

import asyncio
from collections.abc import Awaitable, Callable
from contextlib import suppress
from dataclasses import dataclass
from typing import Any, Literal

from app.cli.api_client import DAgentsApiClient, StreamEvent
from app.cli.log import get_session_controller_logger
from app.cli.approval import (
    ApprovalDecision,
    build_approval_resume,
)
from app.cli.child_agent import (
    ChildAgentTracker,
    approval_queue_key,
    child_session_id_from_data,
    format_child_lifecycle_line,
    is_a2a_relay_hitl,
    should_skip_child_runtime_display,
)
from app.cli.hitl_batch import expand_hitl_required
from app.cli.last_session import save_last_session
from app.cli.user_information import (
    UserInformationAnswer,
    UserInformationCancelled,
    UserInformationRequest,
    extract_user_information_request,
)
from app.cli.render import (
    TranscriptKind,
    TranscriptUpdate,
    UsageStripSnapshot,
    format_assistant_delta,
    format_assistant_end,
    format_compact_token_count,
    format_context_compression,
    format_error,
    format_input_strip_usage,
    format_reasoning,
    format_system_line,
    format_tool_call,
    format_tool_result,
    format_user_information_required,
    parse_usage_strip,
    parse_usage_round,
    accumulate_usage_strip,
)

TranscriptCallback = Callable[[TranscriptUpdate], None]
TranscriptClearCallback = Callable[[], None]
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
    - submit 后以 `_turn_seq_fence` 过滤在途 turn 的陈旧 `done`；本轮 turn 的 `done`（含 HITL 暂停、`seq > fence`）结束 `wait_user_turn`；
    - `done` 仅表示语义 B（编排暂停、轮到用户）；段落换行由 assistant/tool 等事件收束，不依赖 done。
    """

    def __init__(
        self,
        *,
        api_base: str,
        session_id: str | None,
        show_reasoning: bool,
        config_path: str | None = None,
    ) -> None:
        self.api_base = api_base # 后端地址
        self.config_path = config_path # 配置文件路径
        self.initial_session_id = session_id # 初始会话ID
        self.show_reasoning = show_reasoning # 是否显示推理
        self.session_id = "" # 当前会话ID
        self._client: DAgentsApiClient | None = None # API客户端
        self._events: asyncio.Queue[StreamEvent] = asyncio.Queue() # SSE事件队列
        self._stream_task: asyncio.Task[None] | None = None # SSE任务
        self._render_task: asyncio.Task[None] | None = None # 渲染任务
        self._transcript_cb: TranscriptCallback | None = None # 对话区更新回调
        self._transcript_clear_cb: TranscriptClearCallback | None = None
        self._user_information_cb: UserInformationCallback | None = None # 用户询问回调
        self._status_cb: StatusCallback | None = None # 状态栏回调
        self._hitl_pending_cb: HitlPendingCallback | None = None # HITL入队后通知UI处理
        self._child_strip_cb: ChildStripCallback | None = None # 子Agent状态条变更时通知UI刷新
        self._done_counter = 0 # 完成计数
        self._user_turn_done = asyncio.Event() # 用户轮次完成事件
        self._awaiting_user_turn = False # 是否等待用户轮次
        self._user_turn_started = False # 用户轮次开始标志
        self._submit_pending_marker = False # 提交等待标志
        self._assistant_line_open = False # 助手行打开标志
        self._sse_connected = False # SSE连接状态
        self._hitl_queue: list[PendingHITL] = [] # HITL队列
        self._child_tracker = ChildAgentTracker() # 子Agent跟踪器
        self._last_event_seq = 0 # 最后事件序号
        self._turn_seq_fence = 0 # 轮次序号栅栏
        self._sse_ready = asyncio.Event() # SSE就绪事件
        self._messages_total_tokens: int | None = None # 消息总Token数
        self._usage_strip = UsageStripSnapshot() # 使用统计快照
        self._token_refresh_task: asyncio.Task[None] | None = None # 上下文Token刷新任务
        self._logger = get_session_controller_logger()
        self._resume_submit_seq = 0
        self._llm_info: dict[str, Any] = {}
        self._node_version = ""

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
        stale = seq > 0 and seq <= self._turn_seq_fence
        if stale:
            self._logger.debug(
                "skip stale sse event type=%s seq=%s fence=%s",
                event.event_type,
                seq,
                self._turn_seq_fence,
            )
        return stale

    def on_transcript(self, callback: TranscriptCallback) -> None:
        """注册 transcript 更新回调。"""
        self._transcript_cb = callback

    def on_transcript_clear(self, callback: TranscriptClearCallback) -> None:
        """hydrate 前清空 UI transcript。"""
        self._transcript_clear_cb = callback

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

    @property
    def awaiting_user_turn(self) -> bool:
        return self._awaiting_user_turn

    @property
    def llm_info(self) -> dict[str, Any]:
        return dict(self._llm_info)

    @property
    def node_version(self) -> str:
        return self._node_version

    async def patch_llm_settings(self, patch: dict[str, Any]) -> dict[str, Any]:
        """PATCH /v1/llm/settings 并更新本地缓存。"""
        assert self._client is not None
        result = await self._client.patch_llm_settings(patch)
        if isinstance(result, dict):
            self._llm_info = result
        return result

    async def start(self) -> None:
        """连接 Agent Node 并启动 SSE pump 与 render 循环。"""
        self._client = DAgentsApiClient(self.api_base)
        health = await self._client.get_health()
        if str(health.get("status") or "").strip().lower() != "ok":
            raise RuntimeError(f"node unhealthy: status={health.get('status')!r}")
        self._node_version = str(health.get("version") or "").strip()
        try:
            info = await self._client.get_agent_info()
            llm = info.get("llm")
            self._llm_info = llm if isinstance(llm, dict) else {}
        except Exception:
            self._llm_info = {}
        self._sse_ready.clear()
        # 创建会话
        self.session_id = await self._client.create_session(self.initial_session_id)
        self._logger.info("session started api=%s session_id=%s", self.api_base, self.session_id)
        # 启动SSE泵和渲染循环
        self._stream_task = asyncio.create_task(self._pump_stream())
        self._render_task = asyncio.create_task(self._render_loop())
        try:
            await asyncio.wait_for(self._sse_ready.wait(), timeout=15.0)
        except TimeoutError as exc:
            raise RuntimeError("SSE subscription timed out") from exc
        # 更新状态栏并启动上下文Token刷新
        self._emit_status()
        self._schedule_context_token_refresh()
        self.remember_last_session()
        await self.hydrate_session()

    async def hydrate_session(self) -> None:
        """GET /hydrate 恢复 transcript 与 pending HITL（F-H6）。"""
        assert self._client is not None
        if self._transcript_clear_cb is not None:
            self._transcript_clear_cb()
        try:
            data = await self._client.get_session_hydrate(self.session_id)
        except Exception as exc:
            self._logger.warning("hydrate failed session_id=%s err=%s", self.session_id, exc)
            self._emit_transcript(format_system_line(f"hydrate 失败: {exc}"))
            return
        from app.cli.hydrate import apply_session_hydrate

        await apply_session_hydrate(self, data)

    async def _post_session_ack(self, sse_seq: int) -> None:
        assert self._client is not None
        try:
            await self._client.post_session_ack(self.session_id, sse_seq)
        except Exception:
            pass

    async def stop(self) -> None:
        """停止后台任务并关闭 API 客户端。"""
        self._logger.info("session stopping session_id=%s", self.session_id)
        self.remember_last_session()
        for task in (self._render_task, self._stream_task):
            if task is not None:
                task.cancel()
                with suppress(asyncio.CancelledError):
                    await task
        if self._client is not None:
            await self._client.close()
            self._client = None

    async def submit_message(self, content: str) -> None:
        """投递用户消息并标记等待本轮 turn 完成。

        逻辑：
        1. 丢弃本地 HITL 队列（服务端会用 InterruptPending 清 pending，旧 resume 会 unknown id）；
        2. 重置 turn 栅栏并 POST /v1/messages。
        """
        assert self._client is not None
        if not self._sse_ready.is_set():
            raise RuntimeError("SSE not ready")
        self._drop_hitl_queue_for_user_interrupt()
        self._reset_user_turn_wait()
        self._logger.info(
            "submit message session_id=%s len=%s fence=%s",
            self.session_id,
            len(content),
            self._turn_seq_fence,
        )
        await self._client.submit_message(
            session_id=self.session_id,
            content=content,
        )

    async def wait_user_turn(self, *, timeout_seconds: float = 300.0) -> None:
        """阻塞直到本轮用户消息对应的语义 B `done`（编排暂停或链结束）。

        逻辑：
        1. 仅当 `_awaiting_user_turn` 为真时等待；
        2. `_handle_stream_event(done)` 在 `_user_turn_started` 或 `seq > _turn_seq_fence` 时 set 事件；
        3. 超时向上抛 RuntimeError。

        关键分支：submit 前在途 turn 的 `done`（seq ≤ fence 且无内容）被忽略。
        """
        if not self._awaiting_user_turn:
            return
        try:
            await asyncio.wait_for(self._user_turn_done.wait(), timeout=timeout_seconds)
        except TimeoutError as exc:
            self._awaiting_user_turn = False
            self._user_turn_done.set()
            self._logger.warning(
                "wait_user_turn timeout session_id=%s after=%ss",
                self.session_id,
                int(timeout_seconds),
            )
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
        result = await self._client.clear_session_context(self.session_id)
        self._messages_total_tokens = 0
        self._usage_strip = UsageStripSnapshot()
        self._emit_child_strip()
        return result

    async def list_sessions(self) -> dict[str, Any]:
        """查询后端 session 列表（GET /v1/sessions）。"""
        assert self._client is not None
        return await self._client.list_sessions()

    async def switch_session(self, requested_id: str | None = None) -> str:
        """切换当前 session 并重连 SSE（live 订阅新 session）。"""
        assert self._client is not None
        if self._stream_task is not None:
            self._stream_task.cancel()
            with suppress(asyncio.CancelledError):
                await self._stream_task
            self._stream_task = None
        self._drain_event_queue()
        old_id = self.session_id
        new_id = await self._client.create_session(requested_id)
        self.session_id = new_id
        self._logger.info("session switched old=%s new=%s", old_id, new_id)
        self._reset_session_local_state()
        self._sse_ready.clear()
        self._sse_connected = False
        self._stream_task = asyncio.create_task(self._pump_stream())
        try:
            await asyncio.wait_for(self._sse_ready.wait(), timeout=15.0)
        except TimeoutError as exc:
            raise RuntimeError("SSE subscription timed out after session switch") from exc
        self._emit_status()
        self._schedule_context_token_refresh()
        self.remember_last_session()
        await self.hydrate_session()
        return new_id

    def remember_last_session(self) -> None:
        """将当前 session_id 写入 Client 本地状态（按 api_base 区分）。"""
        sid = str(self.session_id or "").strip()
        if not sid:
            return
        save_last_session(self.api_base, sid, config_path=self.config_path)

    def set_show_reasoning(self, enabled: bool) -> None:
        """运行时开关 reasoning 流展示（默认由 --show-reasoning 初始化）。"""
        self.show_reasoning = enabled

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

    async def get_agent_update(self) -> dict[str, Any]:
        """查询 Local Assistant 更新状态；Windows delegate 时改读 Shell。"""
        from app.cli.desktop import resolve_agent_update

        assert self._client is not None
        return await resolve_agent_update(self._client)

    async def list_triggers(self) -> dict[str, Any]:
        """查询 Agent 已配置的触发器列表（GET /v1/triggers）。"""
        assert self._client is not None
        return await self._client.list_triggers()

    async def get_policy(self, *, shell: str = "") -> dict[str, Any]:
        assert self._client is not None
        return await self._client.get_policy(shell=shell)

    async def update_tool_policy(self, updates: list[dict[str, str]]) -> None:
        assert self._client is not None
        await self._client.update_tool_policy(updates)

    async def update_shell_policy(
        self,
        shell_type: str,
        updates: list[dict[str, str]] | None = None,
        *,
        deletes: list[str] | None = None,
    ) -> None:
        assert self._client is not None
        await self._client.update_shell_policy(shell_type, updates, deletes=deletes)

    async def compress_context(self) -> dict[str, Any]:
        """手动触发一次阻塞压缩（POST /v1/sessions/{session_id}/compress）。"""
        assert self._client is not None
        result = await self._client.compress_session_context(self.session_id)
        tokens = result.get("messages_total_tokens")
        if isinstance(tokens, int) and tokens >= 0:
            self._messages_total_tokens = tokens
        return result

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

    def input_strip_token_text(self) -> str:
        """input strip 右侧 token 统计：优先 SSE usage，回退 context 估算。"""
        usage_text = format_input_strip_usage(self._usage_strip)
        if usage_text:
            return usage_text
        return format_compact_token_count(self._messages_total_tokens)

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
        """提交审批 resume 并弹出队首。"""
        if not self._hitl_queue or self._hitl_queue[0].kind != "approval":
            self._logger.debug(
                "complete_hitl_approval skipped session_id=%s queue_len=%s head_kind=%s",
                self.session_id,
                len(self._hitl_queue),
                self._hitl_queue[0].kind if self._hitl_queue else None,
            )
            return
        data = self._hitl_queue[0].data
        child_id = child_session_id_from_data(data)
        if child_id:
            self._child_tracker.set_awaiting_approval(child_id, False)
            self._emit_child_strip()
        self._hitl_queue.pop(0)
        assert self._client is not None
        resume_value = build_approval_resume(data, decision)
        self._resume_submit_seq += 1
        seq = self._resume_submit_seq
        self._logger.info(
            "submit resume session_id=%s seq=%s hitl_kind=approval resume_type=%s "
            "resume_tool_call_id=%s resume_value=%s",
            self.session_id,
            seq,
            resume_value.get("type", ""),
            resume_value.get("tool_call_id", ""),
            resume_value,
        )
        await self._client.submit_resume(
            session_id=self.session_id,
            resume_value=resume_value,
            submit_seq=seq,
        )
        self._notify_hitl_pending()

    async def complete_hitl_user_info(self, answer: UserInformationAnswer) -> None:
        """提交用户询问 resume 并弹出队首。"""
        if not self._hitl_queue or self._hitl_queue[0].kind != "user_information":
            self._logger.debug(
                "complete_hitl_user_info skipped session_id=%s queue_len=%s head_kind=%s",
                self.session_id,
                len(self._hitl_queue),
                self._hitl_queue[0].kind if self._hitl_queue else None,
            )
            return
        self._hitl_queue.pop(0)
        assert self._client is not None
        resume_value = answer.to_resume_value()
        self._resume_submit_seq += 1
        seq = self._resume_submit_seq
        self._logger.info(
            "submit resume session_id=%s seq=%s hitl_kind=user_information resume_type=%s "
            "resume_tool_call_id=%s resume_value=%s",
            self.session_id,
            seq,
            resume_value.get("type", ""),
            resume_value.get("tool_call_id", ""),
            resume_value,
        )
        await self._client.submit_resume(
            session_id=self.session_id,
            resume_value=resume_value,
            submit_seq=seq,
        )
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

    def begin_implicit_turn(self) -> None:
        """被动续跑（side_effect_continue）：等同 submit 栅栏但不 POST message。"""
        self._reset_user_turn_wait()

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

    def _schedule_context_token_refresh(self) -> None:
        """异步拉取 GET /context 并更新 input strip token 统计。

        逻辑：
        1. 取消尚未完成的上一轮刷新（done/compression 可能连续触发）；
        2. 后台 GET context，读取 messages_total_tokens；
        3. 数值变化时通知 UI 刷新 input strip。

        关键边界：
        - 拉取失败静默忽略，保留上次展示值；
        - stop 后 client 可能已关闭，任务内须判空。
        """
        if self._token_refresh_task is not None and not self._token_refresh_task.done():
            self._token_refresh_task.cancel()
        self._token_refresh_task = asyncio.create_task(self._refresh_context_tokens())

    async def _refresh_context_tokens(self) -> None:
        """执行一次 context token 拉取并刷新 strip。"""
        if self._client is None or not self.session_id:
            return
        try:
            data = await self._client.get_session_context(self.session_id)
            tokens = int(data.get("messages_total_tokens") or 0)
        except asyncio.CancelledError:
            raise
        except Exception:
            return
        if self._messages_total_tokens != tokens:
            self._messages_total_tokens = tokens
            self._emit_child_strip()

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
                    self._logger.info("sse connected session_id=%s", self.session_id)
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
            self._logger.exception("sse stream failed session_id=%s", self.session_id)
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
            self._logger.info("sse disconnected session_id=%s", self.session_id)
            self._emit_status()

    async def _render_loop(self) -> None:
        """持续消费 SSE 队列并分发 transcript / approval / turn 栅栏。"""
        while True:
            event = await self._events.get()
            await self._handle_stream_event(event)

    async def _handle_stream_event(self, event: StreamEvent) -> None:
        """处理单条 SSE 事件并更新 UI / turn 状态。"""
        if event.is_stream_ready:
            return
        if self._is_stale_stream_event(event):
            return
        data = event.data
        event_type = event.event_type
        self._logger.debug(
            "sse event type=%s seq=%s session_id=%s data=%s",
            event_type,
            self._event_seq(event),
            event.session_id or self.session_id,
            data,
        )

        if should_skip_child_runtime_display(event_type, data):
            return

        if event_type in {
            "temporary_agent_created",
            "temporary_agent_completed",
            "temporary_agent_cancelled",
        }:
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
            # 始终下发 REASONING_DELTA，供 TUI 展示 thinking 等待态；正文由 show_reasoning 控制（对齐 Go TUI）。
            content = str(data.get("content") or "")
            self._ensure_assistant_end()
            self._emit_transcript(format_reasoning(content))
        elif event_type == "tool_call":
            self._ensure_assistant_end()
            self._child_tracker.note_tool_call(data)
            formatted = format_tool_call(data)
            if formatted is not None:
                self._emit_transcript(formatted)
        elif event_type == "hitl_required":
            self._ensure_assistant_end()
            user_infos, approval = expand_hitl_required(data)
            for ui in user_infos:
                self._enqueue_hitl(PendingHITL(kind="user_information", data=ui))
            if approval:
                self._enqueue_hitl(PendingHITL(kind="approval", data=approval))
        elif event_type == "approval_required":
            self._ensure_assistant_end()
            self._enqueue_hitl(PendingHITL(kind="approval", data=data))
        elif event_type == "user_information_required":
            self._ensure_assistant_end()
            self._enqueue_hitl(PendingHITL(kind="user_information", data=data))
        elif event_type == "tool_result":
            self._ensure_assistant_end()
            self._child_tracker.note_tool_result(
                str(data.get("tool_name") or ""),
                str(data.get("content") or ""),
            )
            self._emit_transcript(format_tool_result(data))
        elif event_type == "usage":
            self._usage_strip = accumulate_usage_strip(
                self._usage_strip,
                parse_usage_round(data),
            )
            self._emit_transcript(TranscriptUpdate(kind=TranscriptKind.USAGE, data=data))
            self._emit_child_strip()
        elif event_type in {"context_compression_blocking", "context_compression_silent"}:
            self._ensure_assistant_end()
            self._emit_transcript(format_context_compression(event_type, data))
            self._schedule_context_token_refresh()
        elif event_type == "side_effect_turn_start":
            self.begin_implicit_turn()
        elif event_type == "user_message_deferred":
            self._ensure_assistant_end()
            content = str(data.get("content") or "")
            user_name = str(data.get("user_name") or "").strip()
            prefix = f"[{user_name} deferred] " if user_name else "[deferred] "
            self._emit_transcript(format_system_line(prefix + content))
        elif event_type == "side_effect_applied":
            self._ensure_assistant_end()
            seqs = data.get("seqs") or []
            if seqs:
                joined = ", ".join(f"#{s}" for s in seqs)
                self._emit_transcript(format_system_line(f"旁路回调 已入库: {joined}"))
        elif event_type == "side_effects_cleared":
            self._ensure_assistant_end()
            seqs = data.get("seqs") or []
            if seqs:
                joined = ", ".join(f"#{s}" for s in seqs)
                self._emit_transcript(format_system_line(f"旁路回调 已失效: {joined}"))
        elif event_type == "error":
            self._ensure_assistant_end()
            message = str(data.get("message") or "unknown error")
            self._emit_transcript(format_error(message))
            if self._awaiting_user_turn:
                self._awaiting_user_turn = False
                self._user_turn_done.set()
        elif event_type == "done":
            # done 仅语义 B：编排暂停、轮到用户；assistant 换行由块级事件收束，此处仅兜底。
            self._ensure_assistant_end()
            self._done_counter += 1
            seq = self._event_seq(event)
            finish_reason = str(data.get("finish_reason") or "")
            turn_complete = data.get("turn_complete")
            awaiting = data.get("awaiting")
            if self._awaiting_user_turn:
                # seq > fence 表示 submit 之后的新 turn 已结束（即使 assistant 增量被漏收）。
                if self._user_turn_started or seq > self._turn_seq_fence:
                    self._awaiting_user_turn = False
                    self._user_turn_done.set()
                    self._logger.info(
                        "user turn idle session_id=%s seq=%s finish_reason=%s "
                        "turn_complete=%s awaiting=%s",
                        self.session_id,
                        seq,
                        finish_reason,
                        turn_complete,
                        awaiting,
                    )
                else:
                    self._logger.debug(
                        "done ignored before turn content session_id=%s seq=%s fence=%s",
                        self.session_id,
                        seq,
                        self._turn_seq_fence,
                    )
            else:
                self._logger.debug(
                    "done without awaiting user turn session_id=%s seq=%s "
                    "finish_reason=%s turn_complete=%s awaiting=%s",
                    self.session_id,
                    seq,
                    finish_reason,
                    turn_complete,
                    awaiting,
                )
            self._schedule_context_token_refresh()

    def _enqueue_hitl(self, item: PendingHITL) -> None:
        """HITL 入队并通知 UI；子审批时标记 awaiting_approval。

        逻辑：
        1. approval 按 `approval_queue_key` 去重，仅替换同目标旧项；
        2. 不同子 Agent / 父与子审批可并存，队首逐条展示；
        3. user_information 直接追加，不清理已有 approval。
        """
        if item.kind == "approval":
            key = approval_queue_key(item.data)
            self._hitl_queue = [
                q
                for q in self._hitl_queue
                if q.kind != "approval" or approval_queue_key(q.data) != key
            ]
            child_id = child_session_id_from_data(item.data)
            if child_id:
                self._child_tracker.set_awaiting_approval(child_id, True)
                self._emit_child_strip()
        self._hitl_queue.append(item)
        if is_a2a_relay_hitl(item.data):
            self._release_turn_wait_for_a2a_relay(item.data)
        self._notify_hitl_pending()

    def _release_turn_wait_for_a2a_relay(self, data: dict[str, Any]) -> None:
        """agent_invoke 同步等待期间对端 HITL 中继：释放 turn 等待以便 TUI 处理审批/询问。"""
        if not self._awaiting_user_turn:
            return
        self._awaiting_user_turn = False
        self._user_turn_done.set()
        self._logger.info(
            "a2a relay hitl released turn wait session_id=%s task_id=%s",
            self.session_id,
            str(data.get("a2a_task_id") or ""),
        )

    def _drop_hitl_queue_for_user_interrupt(self) -> None:
        """新用户消息会打断 server pending HITL；本地队列必须同步清空。"""
        if not self._hitl_queue:
            return
        for entry in self._child_tracker.entries.values():
            entry.awaiting_approval = False
        self._hitl_queue.clear()
        self._emit_child_strip()
        self._notify_hitl_pending()

    def _drain_event_queue(self) -> None:
        while True:
            try:
                self._events.get_nowait()
            except asyncio.QueueEmpty:
                break

    def _reset_session_local_state(self) -> None:
        """切换 session 后重置 turn / HITL / 子 Agent 等本地状态。"""
        self.reset_child_state()
        self._awaiting_user_turn = False
        self._submit_pending_marker = False
        self._user_turn_started = False
        self._user_turn_done.set()
        self._assistant_line_open = False
        self._messages_total_tokens = None
        self._usage_strip = UsageStripSnapshot()
        self._turn_seq_fence = 0
        self._last_event_seq = 0

    def _handle_child_lifecycle(self, event_type: str, data: dict[str, Any]) -> None:
        """处理子 Agent 生命周期 SSE，写入系统行并更新 tracker。"""
        if event_type == "temporary_agent_created":
            self._child_tracker.on_created(data)
        else:
            self._child_tracker.on_finished(child_session_id_from_data(data))
        self._emit_child_strip()
        child_id = child_session_id_from_data(data)
        if self._child_tracker.should_suppress_lifecycle(child_id, event_type):
            return
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
