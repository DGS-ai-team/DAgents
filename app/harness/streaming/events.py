"""流式事件总线抽象：当前内存实现，预留 Redis 替换位。"""

from __future__ import annotations

import asyncio
from datetime import datetime, timezone
from typing import Any, AsyncIterator, Protocol
from uuid import uuid4

from pydantic import BaseModel, ConfigDict


class StreamEvent(BaseModel):
    """统一流事件模型。

    用于在 AgentService、API-SSE、后续 Redis 总线之间传递同一事件结构。
    """

    model_config = ConfigDict(frozen=True)

    request_id: str
    session_id: str
    type: str
    seq: int
    ts: str
    data: dict[str, Any]

    def to_dict(self) -> dict[str, Any]:
        """转为可序列化字典（供 SSE JSON 编码）。"""
        return self.model_dump()


class EventBus(Protocol):
    """事件总线协议：后续可替换为 Redis 实现。"""

    def create_request(self, *, session_id: str) -> str:
        """创建 request 事件流并返回 `request_id`。"""
        ...

    def has_request(self, request_id: str) -> bool:
        """判断 `request_id` 是否存在。"""
        ...

    def publish(self, *, request_id: str, event_type: str, data: dict[str, Any]) -> StreamEvent:
        """发布一条事件到指定 request 流并返回标准化事件对象。"""
        ...

    async def subscribe(self, *, request_id: str) -> AsyncIterator[StreamEvent]:
        """订阅指定 request 流，按顺序异步产出事件。"""
        ...


class _RequestStream:
    """单条 request 的内存缓冲（非 Pydantic：内含 `asyncio.Queue` 与可变 seq）。"""

    __slots__ = ("session_id", "seq", "closed", "history", "queue")

    def __init__(self, *, session_id: str) -> None:
        self.session_id = session_id
        self.seq = 0
        self.closed = False
        self.history: list[StreamEvent] = []
        self.queue: asyncio.Queue[StreamEvent] = asyncio.Queue()


class InMemoryEventBus:
    """内存事件总线（单进程）。"""

    def __init__(self) -> None:
        """初始化内存流容器。"""
        self._streams: dict[str, _RequestStream] = {}

    def create_request(self, *, session_id: str) -> str:
        """创建新的 request 事件流。

        逻辑：
        1. 生成 `request_id`；
        2. 初始化该 request 的序号、历史列表与实时队列；
        3. 写入 `_streams` 并返回 `request_id`。
        """
        request_id = str(uuid4())
        self._streams[request_id] = _RequestStream(session_id=session_id)
        return request_id

    def has_request(self, request_id: str) -> bool:
        """检查 request 流是否存在。"""
        return request_id in self._streams

    def publish(self, *, request_id: str, event_type: str, data: dict[str, Any]) -> StreamEvent:
        """发布事件到指定 request 流。

        逻辑：
        1. 读取 request 流状态并构造 `StreamEvent`（含当前 `seq` 与 UTC 时间戳）；
        2. `seq` 自增；
        3. 写入 `history`（供后续订阅者补发）与 `queue`（供实时消费）；
        4. 若事件为 `done`，标记流已关闭；
        5. 返回事件对象。
        """
        stream = self._streams[request_id]
        event = StreamEvent(
            request_id=request_id,
            session_id=stream.session_id,
            type=event_type,
            seq=stream.seq,
            ts=datetime.now(timezone.utc).isoformat(),
            data=data,
        )
        stream.seq += 1
        stream.history.append(event)
        stream.queue.put_nowait(event)
        if event_type == "done":
            stream.closed = True
        return event

    async def subscribe(self, *, request_id: str) -> AsyncIterator[StreamEvent]:
        """订阅 request 流（先补历史，再读实时队列）。

        逻辑：
        1. 先顺序输出 `history`，保证后来订阅者能补到已产生事件；
        2. 再阻塞读取实时 `queue`；
        3. 读到 `done` 事件后结束迭代。
        """
        stream = self._streams[request_id]
        for item in stream.history:
            yield item
        while True:
            item = await stream.queue.get()
            yield item
            if item.type == "done":
                break
