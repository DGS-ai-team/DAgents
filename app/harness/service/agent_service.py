"""独立 Agent 服务（常驻进程）。"""

from __future__ import annotations

import asyncio
import logging
import time
from typing import Any, Awaitable, Callable, Literal
from uuid import uuid4

from pathlib import Path

from app.config.env import resolve_runtime_root
from app.config.runtime_layout import session_sqlite_path
from app.config.settings import get_settings
from app.context.models import OpenAIConversationContext, RunTurnPhase
from app.core.main_agent.model import get_model_config
from app.core.main_agent.agent import MainAgentTurnOrchestrator
from app.harness.memory.store import SqliteMessageStore
from app.harness.queue.message_queue import MessageEnvelope, MessagePriority, MessageQueue
from app.harness.service.interface import AgentEventEnvelope
from app.harness.tools.tool import build_openai_toolkit
from app.harness.tools.async_store import get_async_tool_result_store
from app.observability.metrics import refresh_session_context_metrics, refresh_session_queue_metrics

_logger = logging.getLogger(__name__)

# 与 `_emit_stream_event` 入参一致：`handle_stream_event` 自行从 `env` 取 `client_id`/`session_id` 并决定是否推送。
StreamEventHandler = Callable[[MessageEnvelope, str, dict[str, Any]], Awaitable[None]]


def _queue_maxsize(max_queue_size: int) -> int | None:
    """将配置中的 max_queue_size 规范化为 MessageQueue 可接受的参数。

    逻辑：
    - `<=0` 视为不限制，返回 ``None``（对应 ``PriorityQueue(maxsize=0)`` 语义）；
    - `>0` 原样返回，作为有界队列上限。
    """
    return None if max_queue_size <= 0 else max_queue_size


class AgentService:
    """持续消费消息队列并调用 Agent 处理。"""

    def __init__(
        self,
        *,
        max_queue_size: int = 0,
        handle_stream_event: StreamEventHandler | None = None,
        message_store: SqliteMessageStore | None = None,
    ) -> None:
        """初始化服务状态；不启动任何消费者、不预建 session 队列。

        逻辑：
        1. 保存队列上限、闲置淘汰配置与可选 **`handle_stream_event`**；
        2. 初始化 session 队列/context/消费者 task 等空映射与 **`AsyncToolResultStore`** 发件人占位；
        3. 组装 **`MainAgentTurnOrchestrator`**（入队提交 + **`_emit_envelope`**）；
        4. 按入参或 **`Settings.agent_session_store_enabled`** + 固定 **`session_sqlite_path()`** 决定 **`_message_store`**（显式注入 / sqlite / 纯内存）。

        关键边界：
        - **`handle_stream_event` 为 ``None``** 时 **`_emit_stream_event`** 直接返回，不向任何订阅方推送；
        - 注入回调后，**是否因缺少 `client_id` 而忽略事件**由回调自行判断，服务层不再代劳。

        Args:
            max_queue_size: 单个 session 队列容量；<=0 表示不限制。
            handle_stream_event: 可选；签名与 **`_emit_stream_event`** 一致：`async (env, event_type, data)`，
                其中 **`env`** 为当前 **`MessageEnvelope`**（含 **`client_id`** / **`session_id`**），
                **`event_type`** / **`data`** 为 **`_map_event_envelope_to_stream`** 映射后的 SSE 语义字段。
            message_store: 可选注入存储（单测可传入或关闭）。
        """
        self._runtime: Any | None = None
        self._stop_event = asyncio.Event()
        settings = get_settings()
        self._max_queue_size = max_queue_size
        self._max_active_session_queues = max(1, int(settings.agent_max_active_session_queues))
        self._session_idle_evict_seconds = max(0, int(settings.agent_session_idle_evict_seconds))
        self._session_queues: dict[str, MessageQueue[MessageEnvelope]] = {}
        self._session_contexts: dict[str, OpenAIConversationContext] = {}
        self._session_last_activity: dict[str, float] = {}
        self._session_consumer_tasks: dict[str, asyncio.Task[None]] = {}
        self._session_active_handles: dict[str, asyncio.Task[None] | None] = {}
        self._async_store = get_async_tool_result_store()
        self._handle_stream_event = handle_stream_event
        _, self._tool_map = build_openai_toolkit()
        self._turn_orchestrator = MainAgentTurnOrchestrator(
            submit_message=self.submit_message,
            emit_envelope=self._emit_envelope,
            tool_map=self._tool_map
        )
        if message_store is not None:
            self._message_store = message_store
        else:
            # 开关关闭：纯内存会话，便于单测与无持久化部署；开启时使用固定 sqlite 路径（见 `runtime_layout`）。
            if settings.agent_session_store_enabled:
                self._message_store = SqliteMessageStore(session_sqlite_path())
            else:
                self._message_store = None

    async def start(self) -> None:
        """标记服务已启动（当前无额外预热逻辑）。

        逻辑：写 INFO 启动日志；session 队列仍按 `submit_message` 首次命中时创建。
        """
        self._async_store.register_message_queue_sender(self._enqueue_async_tool_result_message)
        _logger.info("agent-service started")

    async def stop(self) -> None:
        """停止服务：取消在途 turn、停止消费者与队列，并唤醒 `run_forever()`。

        逻辑：
        1. 对 `_session_active_handles` 中未完成的 `_handle_message` task 调用 `cancel` 并 `await`（触发
           runtime 的 `flush_cancelled_turn` 与 `finally` 落盘）；
        2. 对 `_session_consumer_tasks` 中每个 session 消费循环 `cancel` 并 `await`；
        3. 清空上述映射后，对每个 `MessageQueue` 调用 `stop()`，再清空 `_session_queues` 与 `_session_contexts`；
        4. `set` `_stop_event` 并写 INFO 停止日志。

        关键边界：
        - 若某 session 正阻塞在 `receive()`，先取消 consumer 可通过 `CancelledError` 退出循环，再 `queue.stop()` 兜底唤醒。
        """
        # 先结束「正在跑的一轮」：cancel 触发 _handle_message 内 flush + finally 落盘；分两遍避免迭代中改 dict。
        for ht in list(self._session_active_handles.values()):
            if ht is not None and not ht.done():
                ht.cancel()
        for ht in list(self._session_active_handles.values()):
            if ht is not None:
                try:
                    await ht
                except asyncio.CancelledError:
                    pass

        # 结束静默压缩后台任务，防止 stop 后仍持有旧 context 写入压缩状态。
        await self._turn_orchestrator.cancel_all_summary_tasks()

        # 再停「每 session 的 receive 循环」，避免 stop 后仍从已关闭队列取消息。
        for t in list(self._session_consumer_tasks.values()):
            if t and not t.done():
                t.cancel()
        for t in list(self._session_consumer_tasks.values()):
            if t:
                try:
                    await t
                except asyncio.CancelledError:
                    pass

        self._session_consumer_tasks.clear()
        self._session_active_handles.clear()
        self._async_store.register_message_queue_sender(None)
        for q in self._session_queues.values():
            await q.stop()
        self._session_queues.clear()
        self._session_contexts.clear()
        self._session_last_activity.clear()
        refresh_session_context_metrics({})
        refresh_session_queue_metrics({})
        self._stop_event.set()
        _logger.info("agent-service stopped")

    async def _enqueue_async_tool_result_message(self, session_id: str, payload: dict[str, Any]) -> None:
        """将异步工具完成结果投递到会话消息队列。

        逻辑：
        1. 从 **`payload`** 取出 **`client_id`**（与 **`AsyncToolJob`** 一致），缺省时抛错，避免 SSE 总线无法分桶；
        2. 复制 **`payload`** 为入队用字典，**剔除 `client_id`**，避免与 **`MessageEnvelope.client_id`** 重复语义污染 **`async_tool_result`** 业务载荷；
        3. 复用 **`submit_message`** 统一入队；
        4. 指定 **`request_type="async_tool_result"`** 并以 **`tool_result`** 优先级入队。

        关键边界：
        - 本方法为协程，供 **`AsyncToolResultStore`** 在终态通知路径中 **`await`**；
        - **`client_id`** 空白时抛 **`ValueError`**，与 **`submit_coroutine`** 前置约束一致。

        异常说明：
        - **`client_id`** 缺失时向上抛出，便于观测与单测断言。

        副作用说明：
        - 仅触发入队；不修改 **`AsyncToolResultStore`** 内任务表。
        """
        cid = str(payload.get("client_id") or "").strip()
        if not cid:
            raise ValueError(
                "async_tool_result 缺少 client_id：无法将异步工具终态路由到 SSE 客户端，"
                "请检查 AsyncToolJob 是否在 submit_coroutine 阶段写入了非空 client_id。"
            )
        body = dict(payload)
        body.pop("client_id", None)
        await self.submit_message(
            session_id=session_id,
            content="",
            request_type="async_tool_result",
            async_tool_result=body,
            source="async-store",
            priority="tool_result",
            client_id=cid,
        )

    async def run_forever(self) -> None:
        """启动后阻塞，直到 `stop()` 被调用。

        逻辑：先 `await start()`，再 `await _stop_event.wait()`，与信号处理里触发的 `stop()` 配合退出。
        """
        await self.start()
        await self._stop_event.wait()

    async def create_session(self, session_id: str | None = None) -> str:
        """创建并初始化会话：绑定队列并预热 `OpenAIConversationContext` 缓存。

        逻辑：
        1. 若未提供 `session_id`，自动生成 UUID；
        2. `await _get_or_create_session_queue_async` 确保会话队列存在（达上限时可按闲置策略淘汰旧会话）；
        3. 若缓存中尚无该 `session_id`，从 sqlite（若启用）同步加载或建空上下文；
        4. 返回最终 `session_id`。

        关键边界：
        - 若传入的 `session_id` 已存在，行为是幂等初始化并直接返回；
        - 当活跃会话达到上限且不存在可淘汰的闲置会话时，抛出与入队一致的 `RuntimeError`。
        """
        sid = (session_id or "").strip() or str(uuid4())
        await self._get_or_create_session_queue_async(sid)
        if sid not in self._session_contexts:
            self._session_contexts[sid] = self._load_context_from_store_sync(sid)
        refresh_session_context_metrics(self._session_contexts)
        return sid

    async def release_session(self, session_id: str, *, clear_persisted: bool = True) -> bool:
        """释放指定会话占用的服务端资源（内存 + 可选持久化）。

        逻辑：
        1. 规范化并校验 `session_id`，空值直接抛 `ValueError`；
        2. 若该会话当前存在队列/消费者/在途 turn，则复用 `_evict_session_for_capacity` 做统一拆除；
        3. 否则兜底清理内存映射（避免异常路径残留键）；
        4. `clear_persisted=True` 且启用 sqlite 时，在线程池调用 `clear_session` 删除持久化行。

        关键分支/边界：
        - 本方法幂等：会话不存在时也可安全调用；
        - 正在执行中的 turn 会被取消，取消后的上下文由 `_handle_message` 的取消收口逻辑处理；
        - 持久化清理失败将向上抛出异常，由 API 层统一转为 400。

        副作用说明：
        - 可能取消任务、停止队列、删除会话缓存及 sqlite 中的会话记录。
        """
        sid = (session_id or "").strip()
        if not sid:
            raise ValueError("session_id 不能为空。")
        existed_in_memory = sid in self._session_queues or sid in self._session_contexts
        if sid in self._session_queues:
            await self._evict_session_for_capacity(sid)
        else:
            self._session_consumer_tasks.pop(sid, None)
            self._session_active_handles.pop(sid, None)
            self._session_contexts.pop(sid, None)
            self._session_last_activity.pop(sid, None)
            refresh_session_context_metrics(self._session_contexts)
        if clear_persisted and self._message_store is not None:
            await asyncio.to_thread(self._message_store.clear_session, sid)
            return True
        return existed_in_memory

    async def submit_message(
        self,
        *,
        session_id: str,
        content: str,
        request_type: Literal["message", "async_tool_result", "tool_result"] = "message",
        async_tool_result: dict[str, Any] | None = None,
        tool_result: dict[str, Any] | None = None,
        source: str = "service",
        priority: MessagePriority = "other",
        client_id: str | None = None,
    ) -> None:
        """按 session 入队一条消息至agent处理队列，以在下一轮触发llm调用。

        逻辑：
        1. `await _get_or_create_session_queue_async(session_id)` 取队列（可能新建并 `create_task` 启动 `_session_consume_loop`，达上限时可淘汰闲置会话）；
        2. 构造 `MessageEnvelope` 并 **`enqueue(..., priority=...)`**；
        3. 队列满、或活跃 session 数超上限且无法淘汰时，异常向上抛出，由调用方处理。

        关键边界：
        - **不在此根据 `priority` 调用 `cancel_current_turn`**：是否打断在途 turn 由客户端（如 CLI）显式调取消 API；
        - **`human` 与 `resume`/`other` 的出队顺序**由 **`MessageQueue`** 决定（`human` 最优先）。

        Args:
            session_id: 会话标识，决定使用哪条 session 队列。
            content: 用户输入文本；`request_type in {"async_tool_result","tool_result"}` 时可为空。
            request_type: 入队消息类型；默认 `message`，内部可传 `async_tool_result` 或 `tool_result`。
            async_tool_result: 异步工具结果载荷；仅 `request_type="async_tool_result"` 时使用。
            tool_result: 同步工具结果载荷；仅 `request_type="tool_result"` 时使用。
            source: 来源标签，仅写入 envelope，便于日志/观测。
            priority: **`human`** 表示用户主输入，仅影响队列优先级，不触发服务端自动取消。
        """
        if request_type == "async_tool_result" and not isinstance(async_tool_result, dict):
            raise ValueError("request_type=async_tool_result 时必须提供 async_tool_result 字典。")
        if request_type == "tool_result" and not isinstance(tool_result, dict):
            raise ValueError("request_type=tool_result 时必须提供 tool_result 字典。")
        effective_priority = priority
        if request_type in {"async_tool_result", "tool_result"} and priority == "other":
            effective_priority = "tool_result"
        q = await self._get_or_create_session_queue_async(session_id)
        q.enqueue(
            envelope=MessageEnvelope(
                session_id=session_id,
                request_type=request_type,
                content=content,
                async_tool_result=async_tool_result,
                tool_result=tool_result,
                source=source,
                client_id=client_id,
            ),
            priority=effective_priority,
        )
        refresh_session_queue_metrics(self._session_queues)

    async def submit_resume(
        self,
        *,
        session_id: str,
        resume_value: Any,
        source: str = "service",
        priority: MessagePriority = "resume",
        client_id: str | None = None,
    ) -> None:
        """投递 resume 请求：将 `resume_value` 入队并交由消费者触发 `Command(resume=...)`。

        逻辑：
        1. `await _get_or_create_session_queue_async(session_id)` 取队列；
        2. 构造 `MessageEnvelope(request_type="resume", resume_value=...)`；
        3. 按默认 `resume` 优先级入队。
        """
        q = await self._get_or_create_session_queue_async(session_id)
        q.enqueue(
            envelope=MessageEnvelope(
                session_id=session_id,
                request_type="resume",
                resume_value=resume_value,
                source=source,
                client_id=client_id,
            ),
            priority=priority,
        )
        refresh_session_queue_metrics(self._session_queues)

    def _load_context_from_store_sync(self, session_id: str) -> OpenAIConversationContext:
        """同步从 sqlite 加载或构造空 `OpenAIConversationContext`（不写回）。

        逻辑：
        1. 无 `_message_store` 时返回空上下文；
        2. 否则 `load_conversation_content` 后 `OpenAIConversationContext.from_conversation_context`。

        Args:
            session_id: 与库表主键一致。
        """
        if self._message_store is None:
            return OpenAIConversationContext(session_id=session_id)
        cc = self._message_store.load_conversation_content(session_id)
        ctx = OpenAIConversationContext.from_conversation_context(cc)
        ctx.session_id = session_id
        return ctx

    async def _resolve_context(self, session_id: str) -> OpenAIConversationContext:
        """解析本进程内会话上下文：命中缓存则返回，否则在线程中读库并写入缓存。

        逻辑：
        1. `_session_contexts` 命中则直接返回（单 session 队列保证不会并发写同一键）；
        2. 未命中时 `to_thread` 调用 `_load_context_from_store_sync` 再写入缓存。

        副作用说明：
        - 可能向 `_session_contexts` 插入新条目。
        """
        if session_id in self._session_contexts:
            return self._session_contexts[session_id]
        ctx = await asyncio.to_thread(self._load_context_from_store_sync, session_id)
        ctx.session_id = session_id
        self._session_contexts[session_id] = ctx
        # 刚从库装入缓存：刷新 `/metrics` 中的 messages 快照（无 sqlite 时也会在首轮 persist 前可见）。
        refresh_session_context_metrics(self._session_contexts)
        return ctx

    async def _persist_context(self, session_id: str, ctx: OpenAIConversationContext) -> None:
        """将上下文写回 sqlite（若启用 store）。

        逻辑：
        1. 无 store 则返回；
        2. `ctx.to_conversation_context` 后在 `to_thread` 中 `save_conversation_content`。

        异常说明：
        - 持久化失败时打印日志，不吞掉已抛出的业务异常（在 `finally` 中调用）。
        """
        if self._message_store is None:
            return
        payload = ctx.to_conversation_context()

        def _save() -> None:
            self._message_store.save_conversation_content(session_id, payload)

        await asyncio.to_thread(_save)

    async def _handle_message(self, env: MessageEnvelope) -> None:
        """单条出队消息处理协程：负责基础设施收口，业务编排委托给 orchestrator。

        逻辑：
        1. `_resolve_context` 取 `OpenAIConversationContext`；
        2. `_get_runtime()` 后调用 `_turn_orchestrator.handle_message(...)` 执行业务分支；
        3. 捕获 `asyncio.CancelledError`：若 runtime 提供 `flush_cancelled_turn` 则调用以修补 `ctx.messages`，再向上 `raise`；
        4. 业务编排抛出未捕获异常时：由本方法发 **`error` + `done`**（保证 SSE 收口）；
        5. `finally` 中 `_persist_context`。

        关键边界：
        - 同一 session 队列串行，故同一 `session_id` 的 `ctx` 不会并发修改；
        - `CancelledError` 不吞掉：flush 后必须 `raise`，以便 consumer 区分「子 task 取消」与「自身取消」。
        """
        ctx = await self._resolve_context(env.session_id)
        # 仅当入站 envelope 显式携带 client_id 时刷新，避免 async_tool_result 等内部入队用 None 覆盖已建立的通道。
        _cid = (env.client_id or "").strip()
        ctx.active_client_id = _cid
        if _cid:
            ctx.sse_client_id = _cid
        self._touch_session_activity(env.session_id)
        runtime = self._get_runtime()
        try:
            base_meta = self._stream_base_meta(env)
            await self._turn_orchestrator.handle_message(
                ctx=ctx,
                runtime=runtime,
                env=env,
                base_meta=base_meta,
            )
            return
        except asyncio.CancelledError:
            # 单测替身可无 flush；真实 runtime 在此把半截 assistant/tool 写回 messages，再向上抛以便 consumer 识别。
            flush = getattr(runtime, "flush_cancelled_turn", None)
            if callable(flush):
                flush(ctx)
            raise
        except Exception as exc:
            _logger.error("%s: %s", env.session_id, exc)
            base_meta = self._stream_base_meta(env)
            err_t, err_d = self._map_event_envelope_to_stream(
                AgentEventEnvelope(event_type="error", payload={"message": str(exc)}, meta={}),
                base_meta=base_meta,
            )
            await self._emit_stream_event(env=env, event_type=err_t, data=err_d)
            done_t, done_d = self._map_event_envelope_to_stream(
                AgentEventEnvelope(event_type="done", payload={"finish_reason": "orchestrator_error"}, meta={}),
                base_meta=base_meta,
            )
            await self._emit_stream_event(env=env, event_type=done_t, data=done_d)
        finally:
            ctx.active_client_id = ""
            try:
                await self._persist_context(env.session_id, ctx)
            except Exception as exc:  # noqa: BLE001
                _logger.error("%s: persist failed: %s", env.session_id, exc)
            # 无论是否启用 sqlite，均以内存中的 ctx.messages 为准刷新 Prometheus（含 cancel/error 路径）。
            refresh_session_context_metrics(self._session_contexts)

    async def _emit_envelope(
        self,
        *,
        env: MessageEnvelope,
        envelope: AgentEventEnvelope,
        base_meta: dict[str, Any],
    ) -> None:
        """映射并发送单条 envelope 到流输出。

        逻辑：
        1. 用 **`_map_event_envelope_to_stream`** 转为 **`(event_type, data)`**；
        2. 写 **`[emit_envelope]`** 日志；
        3. 调用 **`_emit_stream_event`**，由注入的 **`handle_stream_event`**（若有）基于 **`env`** 自行路由。

        关键边界：
        - 无订阅回调时 **`_emit_stream_event`** 立即返回；不抛异常。
        """
        stream_type, stream_data = self._map_event_envelope_to_stream(
            envelope,
            base_meta=base_meta,
        )
        _logger.info("[emit_envelope] %s: type=%s data=%s", env.session_id, stream_type, stream_data)
        await self._emit_stream_event(
            env=env,
            event_type=stream_type,
            data=stream_data,
        )

    def _get_runtime(self) -> Any:
        """懒加载并复用 OpenAI runtime。"""
        if self._runtime is None:
            from app.core.main_agent.agent import init_agent

            self._runtime = init_agent()
        return self._runtime

    def _touch_session_activity(self, session_id: str) -> None:
        """更新指定 session 的最后活动时间（Unix 秒）。

        逻辑：写入 `_session_last_activity[session_id] = time.time()`。

        副作用说明：修改内存字典；用于闲置淘汰与「仍在处理中」会话的保护。
        """
        self._session_last_activity[session_id] = time.time()

    def _pick_idle_eviction_victim(self, *, exclude_session_id: str) -> str | None:
        """在达队列上限需接纳新 session 时，挑选一只可淘汰的闲置会话。

        逻辑：
        1. 若 `_session_idle_evict_seconds <= 0`，关闭闲置淘汰，返回 ``None``；
        2. 遍历 `_session_queues` 中除 `exclude_session_id` 外的 `sid`；
        3. 仅保留「当前时间 - 最后活动时间 >= 阈值」的候选；
        4. 在候选中取 **最后活动时间最小** 的 `sid`（闲置最久）并返回；无候选则返回 ``None``。

        关键边界：
        - 无 `_session_last_activity` 条目的 `sid` 视为 `0.0`，在阈值合理时极易被判为可淘汰；正常路径在创建队列或处理消息时会 touch。
        """
        threshold = self._session_idle_evict_seconds
        if threshold <= 0:
            return None
        now = time.time()
        candidates: list[tuple[float, str]] = []
        for sid in self._session_queues:
            if sid == exclude_session_id:
                continue
            active_handle = self._session_active_handles.get(sid)
            if active_handle is not None and not active_handle.done():
                continue
            ctx = self._session_contexts.get(sid)
            if ctx is not None:
                if ctx.pending_tool_calls or ctx.run_turn_phase != RunTurnPhase.IDLE:
                    continue
            last = float(self._session_last_activity.get(sid, 0.0))
            if now - last < float(threshold):
                continue
            candidates.append((last, sid))
        if not candidates:
            return None
        candidates.sort(key=lambda item: item[0])
        return candidates[0][1]

    async def _evict_session_for_capacity(self, session_id: str) -> None:
        """为接纳新 session 而拆除指定旧会话：取消在途 turn、停消费者与队列，并仅清理进程内状态。

        逻辑：
        1. 若存在未结束的 `_handle_message` task，则 `cancel` 并 `await`（触发 `flush_cancelled_turn` 与 `finally` 落盘）；
        2. 取消并 `await` 该 session 的 `_session_consume_loop` task；
        3. 从 `_session_queues` 取出 `MessageQueue` 并 `await stop()`；
        4. 从 `_session_consumer_tasks`、`_session_active_handles`、`_session_contexts`、`_session_last_activity` 移除对应键。

        关键边界：
        - 与 `stop()` 中单 session 子集逻辑一致，避免残留 task 持有已删 dict 键；
        - **不**调用 `SqliteMessageStore.clear_session`：sqlite 仅作持久化，淘汰只释放内存中的队列与 context，后续同一 `session_id` 再进入时可从库中恢复。

        副作用说明：该 `session_id` 在进程内不再活跃；若在途 turn 的 `finally` 中已落盘，则库中仍保留该行供再次加载。
        """
        ht = self._session_active_handles.get(session_id)
        if ht is not None and not ht.done():
            ht.cancel()
        if ht is not None:
            try:
                await ht
            except asyncio.CancelledError:
                pass

        await self._turn_orchestrator.cancel_session_summary_task(session_id=session_id)

        t = self._session_consumer_tasks.get(session_id)
        if t is not None and not t.done():
            t.cancel()
        if t is not None:
            try:
                await t
            except asyncio.CancelledError:
                pass

        self._session_consumer_tasks.pop(session_id, None)
        self._session_active_handles.pop(session_id, None)
        self._session_contexts.pop(session_id, None)
        self._session_last_activity.pop(session_id, None)

        q = self._session_queues.pop(session_id, None)
        if q is not None:
            await q.stop()

        refresh_session_context_metrics(self._session_contexts)
        refresh_session_queue_metrics(self._session_queues)

        _logger.info(
            "session evicted for capacity (idle session slot)",
            extra={"session_id": session_id},
        )

    async def _get_or_create_session_queue_async(self, session_id: str) -> MessageQueue[MessageEnvelope]:
        """按 session_id 取队列；不存在则创建队列并启动本服务内的 session 消费循环 task。

        逻辑：
        1. 若 `session_id` 已在 `_session_queues`，`_touch_session_activity` 后直接返回；
        2. 当 `len(_session_queues) >= _max_active_session_queues` 时循环：调用 `_pick_idle_eviction_victim` 得 `victim`，
           若存在则 `await _evict_session_for_capacity(victim)`，否则抛 `RuntimeError`；
        3. 新建带 `max_queue_size` 的 `MessageQueue`，写入 `_session_queues`，`create_task(_session_consume_loop)`；
        4. `_touch_session_activity` 后返回该队列。

        说明：同一 `session_id` 共用一个队列 + 单 consumer，保证该 session 内消息串行消费。
        """
        if session_id in self._session_queues:
            self._touch_session_activity(session_id)
            return self._session_queues[session_id]
        while len(self._session_queues) >= self._max_active_session_queues:
            victim = self._pick_idle_eviction_victim(exclude_session_id=session_id)
            if victim is None:
                raise RuntimeError(
                    "active session queue limit exceeded and no evictable idle session: "
                    f"max={self._max_active_session_queues}, "
                    f"idle_evict_seconds={self._session_idle_evict_seconds}, "
                    f"new_session_id={session_id!r}"
                )
            await self._evict_session_for_capacity(victim)
        q = MessageQueue[MessageEnvelope](
            max_queue_size=_queue_maxsize(self._max_queue_size),
        )
        self._session_queues[session_id] = q
        self._session_consumer_tasks[session_id] = asyncio.create_task(self._session_consume_loop(session_id))
        self._touch_session_activity(session_id)
        return q

    def cancel_current_turn(self, session_id: str) -> bool:
        """取消指定 session 当前正在执行的 `_handle_message`（若在跑）。

        逻辑：
        1. 从 `_session_active_handles` 取该 `session_id` 的 task；
        2. 若不存在或已结束，返回 `False`；
        3. 否则 `cancel()` 并返回 `True`。

        副作用：
        - 被取消的 `_handle_message` 会在 `except CancelledError` 中调用 `flush_cancelled_turn` 后 `raise`；
        - consumer 在 `await` 结束后继续 `receive` 下一条（若队列未关）。

        异常说明：
        - 本方法不等待 task 结束；不向外抛异常。
        """
        ht = self._session_active_handles.get(session_id)
        if ht is None or ht.done():
            return False
        ht.cancel()
        return True

    async def _session_consume_loop(self, session_id: str) -> None:
        """单 session 的出队循环：`receive` → `_handle_message` 串行执行。

        逻辑：
        1. 从 `_session_queues` 取队列；缺失则直接返回；
        2. `while True`：`await q.receive()`，遇 `RuntimeError`（队列已 `stop`）则 `break`；
        3. `create_task(_handle_message(env))` 写入 `_session_active_handles` 后 `await` 该 task；
        4. `await` 收到 `CancelledError` 时：若 `asyncio.current_task().cancelling()` 为真则取消尚未结束的子 task 并再 `raise`
           （服务整体 `stop`）；否则视为子 task 被取消，吞掉 `CancelledError` 并继续下一条。

        关键边界：
        - `cancel_current_turn` 只取消子 task，consumer 不退出；
        - `stop` 取消 consumer 时，子 task 一并取消并在 `_handle_message` 内 flush。
        """
        q = self._session_queues.get(session_id)
        if q is None:
            return
        while True:
            try:
                # 与 pause/stop 协作：pause 时阻塞在 gate；stop 后 receive 抛 RuntimeError 退出本循环。
                env = await q.receive()
                refresh_session_queue_metrics(self._session_queues)
            except RuntimeError:
                break
            except asyncio.CancelledError:
                raise
            # 创建一个任务来处理消息，核心逻辑入口
            ht = asyncio.create_task(self._handle_message(env))
            self._session_active_handles[session_id] = ht
            try:
                await ht
            except asyncio.CancelledError:
                # cancel_current_turn 只取消子 task：子结束后 consumer 继续；stop 取消本 task 时须连带取消子 task。
                me = asyncio.current_task()
                if me is not None and me.cancelling():
                    if not ht.done():
                        ht.cancel()
                        try:
                            await ht
                        except asyncio.CancelledError:
                            pass
                    raise
            finally:
                if self._session_active_handles.get(session_id) is ht:
                    self._session_active_handles[session_id] = None

    def _stream_base_meta(self, env: MessageEnvelope) -> dict[str, Any]:
        """构造每条 SSE `data.meta` 的公共字段（会话、当前模型）。

        逻辑：
        1. 从 **`get_model_config()`** 取 **`model`** 字符串；
        2. 与 **`env.session_id`** 组成 dict。
        """
        cfg = get_model_config()
        return {
            "session_id": env.session_id,
            "model": str(cfg.get("model") or ""),
        }

    async def _emit_stream_event(
        self,
        *,
        env: MessageEnvelope,
        event_type: str,
        data: dict[str, Any],
    ) -> None:
        """将已映射的一条流事件交给 **`handle_stream_event`**（若已注入）。

        逻辑：
        1. 若 **`_handle_stream_event` 为 ``None``** 则返回；
        2. 否则 **`await _handle_stream_event(env, event_type, data)`**。

        关键边界：
        - **不在此判断 `env.client_id`**：无订阅方、无 **`client_id`** 等策略由回调实现（如 API 层在写入总线前 return）。

        异常说明：
        - 回调内异常向上抛出，由 **`_handle_message`** 等调用方捕获或中断。

        副作用说明：
        - 仅转发调用；不修改 **`env`**。
        """
        if self._handle_stream_event is None:
            return
        await self._handle_stream_event(env, event_type, data)

    @staticmethod
    def _map_event_envelope_to_stream(
        envelope: AgentEventEnvelope,
        *,
        base_meta: dict[str, Any] | None = None,
    ) -> tuple[str, dict[str, Any]]:
        """将统一事件信封映射为语义化 SSE 事件结构。

        逻辑：
        1. 将 **`base_meta`** 与 **`envelope.meta`** 合并为每条 **`data.meta`**；
        2. 按 **`event_type`** 展开扁平 **`data`**（含 `usage` 的 token 字段；**`done`** 必带 **`finish_reason`**（缺省映射为 **`unspecified`**）；**`tool_call_delta`** 含 **`tool_calls`** 列表）；
        3. `done` 的 payload 为 dict 时与其余事件一致带 **`meta`**。

        关键边界：
        - **`base_meta`** 缺省视为空 dict（单测/工具调用方可不传）。
        """
        bm = dict(base_meta or {})
        em = dict(envelope.meta) if envelope.meta else {}

        def with_meta(data: dict[str, Any]) -> dict[str, Any]:
            return {**data, "meta": {**bm, **em}}

        et = envelope.event_type
        payload = envelope.payload
        # 以下为 HTTP 层 SSE 扁平字段约定，与 runtime 的 AgentEventEnvelope 解耦。
        if et == "assistant":
            return "assistant", with_meta(
                {
                    "content": payload.get("content", ""),
                    # AI 正文默认 Markdown；缺省与 runtime `infer_assistant_delta_display_type` 兜底一致。
                    "display_type": payload.get("display_type", "markdown"),
                }
            )
        if et == "reasoning":
            return "reasoning", with_meta(
                {
                    "content": payload.get("content", ""),
                    "display_type": payload.get("display_type", "reasoning"),
                }
            )
        if et == "usage":
            return "usage", with_meta(
                {
                    "prompt_tokens": int(payload.get("prompt_tokens", 0)),
                    "completion_tokens": int(payload.get("completion_tokens", 0)),
                    "total_tokens": payload.get("total_tokens"),
                    "prompt_audio_tokens": int(payload.get("prompt_audio_tokens", 0)),
                    "prompt_cached_tokens": int(payload.get("prompt_cached_tokens", 0)),
                    "prompt_cache_hit_tokens": int(payload.get("prompt_cache_hit_tokens", 0)),
                    "prompt_cache_miss_tokens": int(payload.get("prompt_cache_miss_tokens", 0)),
                }
            )
        if et == "tool_call_delta":
            return "tool_call_delta", with_meta({"tool_calls": list(payload.get("tool_calls") or [])})
        if et == "tool_call":
            return "tool_call", with_meta(
                {
                    "assistant_content": payload.get("assistant_content", ""),
                    "tool_calls": payload.get("tool_calls", []),
                    "display_type": payload.get("display_type", "normal_text"),
                }
            )
        if et == "tool_result":
            return "tool_result", with_meta(
                {
                    "content": payload.get("content", ""),
                    "tool_call_id": payload.get("tool_call_id"),
                    "tool_name": payload.get("tool_name"),
                    "display_type": payload.get("display_type", "normal_text"),
                    "rejected": bool(payload.get("rejected", False)),
                    "interrupted_by_user_message": bool(payload.get("interrupted_by_user_message", False)),
                    "partial": bool(payload.get("partial", False)),
                }
            )
        if et == "approval_required":
            return "approval_required", with_meta(
                {
                    "approval_type": payload.get("approval_type", "execute_tool"),
                    "content": payload.get("message", ""),
                    "approval_args": payload.get("args", {}),
                    "description": payload.get("description", ""),
                    "approval_id": payload.get("approval_id"),
                    # 与 `tool_call` 一致：缺省时按 `normal_text` 渲染（载荷通常已由编排层写入推断结果）。
                    "display_type": payload.get("display_type", "normal_text"),
                }
            )
        if et == "error":
            return "error", with_meta({"message": payload.get("message", "")})
        if et == "done":
            body = dict(payload if isinstance(payload, dict) else {})
            # 约定：每条 `done` 的 SSE `data` 均带 `finish_reason`（缺省由映射层补齐，避免前端分支漏判）。
            if not str(body.get("finish_reason") or "").strip():
                body["finish_reason"] = "unspecified"
            return "done", with_meta(body)
        return "chunk", with_meta({"raw": payload})

