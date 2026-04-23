"""流式事件总线抽象：当前内存实现，预留 Redis 替换位。"""

from __future__ import annotations

import asyncio
from datetime import datetime, timezone
from typing import Any, AsyncIterator, Protocol
from uuid import uuid4

from pydantic import BaseModel, ConfigDict, Field


class StreamEvent(BaseModel):
    """统一流事件模型。

    用于在 AgentService、API-SSE、后续 Redis 总线之间传递同一事件结构。
    """

    model_config = ConfigDict(frozen=True)

    client_id: str = Field(description="事件归属的客户端通道 ID。")
    session_id: str = Field(description="事件所属会话 ID。")
    type: str = Field(description="事件类型（如 assistant/reasoning/tool_call/done）。")
    seq: int = Field(description="同一 client_id 维度递增的事件序号。")
    ts: str = Field(description="事件生成时间（ISO 8601 UTC 字符串）。")
    data: dict[str, Any] = Field(description="事件业务载荷。")

    def to_dict(self) -> dict[str, Any]:
        """转为可序列化字典（供 SSE JSON 编码）。"""
        return self.model_dump()


class EventBus(Protocol):
    """事件总线协议：后续可替换为 Redis 实现。"""

    def create_stream(self, *, session_id: str, client_id: str) -> str:
        """创建事件流并返回内部 `stream_id`。"""
        ...

    def publish(self, *, stream_id: str, event_type: str, data: dict[str, Any]) -> StreamEvent:
        """发布一条事件到指定流并返回标准化事件对象。"""
        ...

    async def subscribe_all(self, *, client_id: str | None = None) -> AsyncIterator[StreamEvent]:
        """订阅全局事件流，按发布顺序异步产出事件。"""
        ...


class _RequestStream:
    """单条流的内存元信息（仅保存会话与客户端归属）。"""

    __slots__ = ("session_id", "client_id")

    def __init__(self, *, session_id: str, client_id: str) -> None:
        self.session_id = session_id
        self.client_id = client_id


class InMemoryEventBus:
    """内存事件总线（单进程）。"""

    def __init__(self) -> None:
        """初始化内存流容器。"""
        self._streams: dict[str, _RequestStream] = {}
        self._global_subscribers: set[asyncio.Queue[StreamEvent]] = set()
        self._client_seq: dict[str, int] = {}

    def create_stream(self, *, session_id: str, client_id: str) -> str:
        """创建新的事件流。

        逻辑：
        1. 生成 `stream_id`；
        2. 初始化该流的序号、历史列表与实时队列；
        3. 写入 `_streams` 并返回 `stream_id`。
        """
        stream_id = str(uuid4())
        self._streams[stream_id] = _RequestStream(session_id=session_id, client_id=client_id)
        return stream_id

    def publish(self, *, stream_id: str, event_type: str, data: dict[str, Any]) -> StreamEvent:
        """发布事件到指定流。

        逻辑：
        1. 读取流状态并构造 `StreamEvent`（含当前 `seq` 与 UTC 时间戳）；
        2. 更新 `client_id` 维度序号；
        3. 广播到全部全局订阅者队列；
        4. 返回事件对象。
        """
        stream = self._streams[stream_id]
        current_seq = self._client_seq.get(stream.client_id, 0)
        event = StreamEvent(
            client_id=stream.client_id,
            session_id=stream.session_id,
            type=event_type,
            seq=current_seq,
            ts=datetime.now(timezone.utc).isoformat(),
            data=data,
        )
        self._client_seq[stream.client_id] = current_seq + 1
        # 全局订阅者用于“单通道接收所有 session 事件”的前端模式。
        for subscriber_queue in list(self._global_subscribers):
            subscriber_queue.put_nowait(event)
        return event

    async def subscribe_all(self, *, client_id: str | None = None) -> AsyncIterator[StreamEvent]:
        """订阅全局事件流（仅实时，不补历史）。

        逻辑：
        1. 为当前订阅者创建独立 queue；
        2. 将 queue 注册到 `_global_subscribers`；
        3. 持续读取 queue 并按到达顺序输出事件；
        4. 订阅结束时注销 queue，避免内存泄漏。

        关键分支/边界：
        - 本方法不回放历史，仅推送订阅建立后的实时事件；
        - 即使某条流已 done，其他流仍会继续推送，因此不在这里按 done 结束。

        与外部交互：
        - 无网络/磁盘交互，仅使用进程内 asyncio 队列。

        异常说明：
        - 调用方取消订阅时（CancelledError）会进入 finally 并清理订阅者。

        副作用说明：
        - 会临时向 `_global_subscribers` 注册当前订阅者队列。
        """
        queue: asyncio.Queue[StreamEvent] = asyncio.Queue()
        self._global_subscribers.add(queue)
        try:
            while True:
                item = await queue.get()
                if client_id is None or item.client_id == client_id:
                    yield item
                else:
                    continue
        finally:
            if queue in self._global_subscribers:
                self._global_subscribers.remove(queue)
