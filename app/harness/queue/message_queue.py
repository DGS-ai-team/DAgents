"""进程内异步消息队列（MVP）。

仅负责**入队 / 出队**与消费闸门；**不**内嵌消费者协程，由上层（如 `AgentService`）循环 `receive` 并处理消息。

行为概要
--------
- **优先级队列**：`asyncio.PriorityQueue`，数值越小优先级越高。
- **四级优先级**：`tool_result=-1`、`human=0`、`resume=1`、`other=10`。
- **统一入队**：仅 `enqueue`，调用方直接传 `envelope` 对象，并通过 `priority` 指定 `tool_result/human/resume/other`。
- **出队**：`await receive()` 阻塞直到有元素；与 `pause_consuming` / `resume_consuming` 配合。
- **同优先级稳定顺序**：同一优先级下按入队先后顺序处理（内部递增序号保证稳定性）。
- **背压**：若构造时 `max_queue_size` 有限，入队使用 `put_nowait`，满队会抛 `asyncio.QueueFull`。

生命周期
--------
- 上层负责在合适时机 `await receive()`；`stop()` 后置 `_closed`，`receive` 在闸门唤醒后抛 `RuntimeError`。
- 暂停消费时仍允许 `enqueue`，消息会在队列中累积。

未覆盖（后续）
------------
持久化、死信队列、跨进程 broker 等。
"""

from __future__ import annotations

import asyncio
from typing import Any, Generic, Literal, Optional, TypeVar

from pydantic import BaseModel, ConfigDict, Field

import logging

_logger = logging.getLogger(__name__)


class MessageEnvelope(BaseModel):
    """单条入队载荷（与会话请求模型对齐）。

    Attributes:
        session_id: 会话标识，供上层做多会话隔离或日志关联（队列本身不解析）。
        request_type: 请求类型，`message` 为普通对话，`resume` 为恢复执行，`async_tool_result` 为异步工具完成事件，`tool_result` 为同步工具结果事件。
        content: 用户文本内容（普通消息时使用）。
        resume_value: resume 请求的恢复值（由上层透传；工具审批见 `app.schemas.approval`）。
        async_tool_result: 异步工具完成载荷（`request_type=async_tool_result` 时使用）。
        tool_result: 同步工具结果载荷（`request_type=tool_result` 时使用）。
        source: 投递来源标签（如 cli / service / http），仅用于观测与调试。
    """

    model_config = ConfigDict(frozen=True)

    session_id: str = Field(min_length=1)
    request_type: Literal["message", "resume", "async_tool_result", "tool_result"] = "message"
    content: str | None = None
    resume_value: Any = None
    async_tool_result: dict[str, Any] | None = None
    tool_result: dict[str, Any] | None = None
    source: str = "cli"
    client_id: str | None = None


EnvelopeT = TypeVar("EnvelopeT")
MessagePriority = Literal["tool_result", "human", "resume", "other"]


class MessageQueue(Generic[EnvelopeT]):
    """进程内优先级队列：仅入队与阻塞出队，无消费者 task。"""

    PRIORITY_TOOL_RESULT = -1
    PRIORITY_HUMAN = 0
    PRIORITY_RESUME = 1
    PRIORITY_OTHER = 10

    def __init__(
        self,
        *,
        max_queue_size: Optional[int] = None,
    ) -> None:
        """
        Args:
            max_queue_size: 队列最大长度；``None`` 表示不限制（对应 `PriorityQueue(maxsize=0)`）。
        """
        self._queue: asyncio.PriorityQueue[tuple[int, int, EnvelopeT]] = asyncio.PriorityQueue(
            maxsize=0 if max_queue_size is None else max_queue_size
        )
        # 同优先级时 _seq 递增，保证 PriorityQueue 出队顺序与入队先后一致（tuple 第二元比较）。
        self._seq = 0
        self._pending_items: dict[int, tuple[int, int, EnvelopeT]] = {}
        self._consume_gate = asyncio.Event()
        self._consume_gate.set()
        self._closed = False

    def enqueue(
        self,
        *,
        envelope: EnvelopeT,
        priority: MessagePriority = "other",
    ) -> None:
        """非阻塞入队；调用方需自行构建 envelope 对象。

        Raises:
            RuntimeError: 队列已 `stop`（`_closed`）后禁止再入队。
            asyncio.QueueFull: 有界队列已满时（仅当构造时限制了 `max_queue_size`）。
        """
        # 泛型 EnvelopeT 未必为 MessageEnvelope；日志用 getattr 避免自定义 envelope 入队即崩。
        sid = getattr(envelope, "session_id", None)
        req = getattr(envelope, "request_type", None)
        _logger.info("[enqueue] %s: request_type=%s", sid, req)
        if self._closed:
            raise RuntimeError("MessageQueue 已关闭，无法 enqueue")
        self._put_nowait(priority=self._priority_value(priority), env=envelope)

    async def receive(self) -> EnvelopeT:
        """阻塞直到取出一条消息（受 `pause_consuming` / `resume_consuming` 与 `stop` 影响）。

        逻辑：
        1. `await self._consume_gate.wait()`；
        2. 若已 `_closed`，抛 `RuntimeError`；
        3. `await self._queue.get()` 并返回 envelope。

        Raises:
            RuntimeError: 队列已关闭，不再出队。
        """
        await self._consume_gate.wait()
        # 须在 gate 通过后再判 closed：stop 时会 set gate，使阻塞中的 receive 能醒来并退出。
        if self._closed:
            raise RuntimeError("MessageQueue 已关闭，无法 receive")
        _, seq, env = await self._queue.get()
        self._pending_items.pop(seq, None)
        return env

    async def stop(self) -> None:
        """禁止新入队；唤醒可能在 `receive` 上阻塞的协程以便退出。

        逻辑：
        1. 置 `_closed`；
        2. `consume_gate.set()`，避免 pause 状态下永久阻塞。

        关键边界：
        - **不**清空队列内未处理消息（MVP）。
        """
        self._closed = True
        self._consume_gate.set()

    def pause_consuming(self) -> None:
        """暂停出队（允许继续入队）。"""
        self._consume_gate.clear()

    def resume_consuming(self) -> None:
        """恢复出队。"""
        self._consume_gate.set()

    @property
    def is_paused(self) -> bool:
        """当前是否处于消费暂停态。"""
        return not self._consume_gate.is_set()

    def _put_nowait(self, *, priority: int, env: EnvelopeT) -> None:
        """向优先级队列执行非阻塞入队，并维护稳定顺序序号。"""
        item = (priority, self._seq, env)
        self._queue.put_nowait(item)
        self._pending_items[self._seq] = item
        self._seq += 1

    def _priority_value(self, priority: MessagePriority) -> int:
        if priority == "tool_result":
            return self.PRIORITY_TOOL_RESULT
        if priority == "human":
            return self.PRIORITY_HUMAN
        if priority == "resume":
            return self.PRIORITY_RESUME
        return self.PRIORITY_OTHER

    def pending_metrics_rows(self) -> list[tuple[int, int, EnvelopeT]]:
        """观测用：列出尚未 `receive` 取出的条目，按真实出队顺序排序，不 dequeue。

        逻辑：
        1. 读取入队/出队同步维护的 `_pending_items` 镜像；
        2. 按 **`(priority_int, seq)`** 排序，与同优先级 FIFO 语义一致；
        3. 返回三元组列表。

        关键边界：
        - 不读取 `asyncio.PriorityQueue` 私有字段，避免依赖 CPython 内部堆结构；
        - `pause` 仅阻塞消费者，仍有条目时本方法照常反映积压。
        """
        return sorted(self._pending_items.values(), key=lambda t: (int(t[0]), int(t[1])))
