"""工作组 human 消息入队（对齐 Node session MessageQueue 的单飞语义）。

Leader / 直连 turn 进行中时，新 human 入队而非并行开 loop；
上一轮产出最终 assistant 后按 FIFO 出队消费。
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any

from manage.workgroup.d3_models import QueuedHumanRecord


def _now() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


@dataclass
class QueuedHuman:
    queue_id: str
    workgroup_id: str
    text: str
    from_node_id: str
    client_message_id: str | None = None
    direct_member_id: str | None = None
    disable_tools: bool = False
    priority: int = 0
    created_at: str = field(default_factory=_now)
    updated_at: str = field(default_factory=_now)

    def to_public(self, position: int) -> dict[str, Any]:
        """position 为 1-based 排队位次。"""
        return {
            "queue_id": self.queue_id,
            "workgroup_id": self.workgroup_id,
            "text": self.text,
            "from_node_id": self.from_node_id,
            "client_message_id": self.client_message_id,
            "direct_member_id": self.direct_member_id,
            "disable_tools": self.disable_tools,
            "priority": self.priority,
            "position": max(1, int(position)),
            "created_at": self.created_at,
            "updated_at": self.updated_at,
        }

    def to_record(self) -> QueuedHumanRecord:
        return QueuedHumanRecord(
            queue_id=self.queue_id,
            workgroup_id=self.workgroup_id,
            text=self.text,
            from_node_id=self.from_node_id,
            client_message_id=self.client_message_id,
            direct_member_id=self.direct_member_id,
            disable_tools=self.disable_tools,
            priority=self.priority,
            created_at=self.created_at,
            updated_at=self.updated_at,
        )

    @classmethod
    def from_record(cls, record: QueuedHumanRecord) -> "QueuedHuman":
        return cls(
            queue_id=record.queue_id,
            workgroup_id=record.workgroup_id,
            text=record.text,
            from_node_id=record.from_node_id,
            client_message_id=record.client_message_id,
            direct_member_id=record.direct_member_id,
            disable_tools=record.disable_tools,
            priority=record.priority,
            created_at=record.created_at,
            updated_at=record.updated_at,
        )
