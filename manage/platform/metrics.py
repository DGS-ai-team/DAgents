"""Manage Prometheus 指标。"""

from __future__ import annotations

from prometheus_client import CONTENT_TYPE_LATEST, Counter, generate_latest

REGISTRY_OPERATIONS_TOTAL = Counter(
    "dagents_manage_registry_operations_total",
    "Manage Registry 操作次数",
    labelnames=("operation", "status"),
)

WORKGROUP_WS_EVENTS_TOTAL = Counter(
    "dagents_manage_workgroup_ws_events_total",
    "Workgroup WebSocket 事件次数（标签值受控）",
    labelnames=("direction", "event"),
)

WORKGROUP_TIMELINE_EVENTS_TOTAL = Counter(
    "dagents_manage_workgroup_timeline_events_total",
    "Workgroup Timeline 持久化事件次数",
    labelnames=("event_type",),
)

_WS_EVENT_LABELS = frozenset(
    {
        "session.hello",
        "session.welcome",
        "resume.offer",
        "resume.complete",
        "resume.error",
        "timeline.event",
        "delivery.ack",
        "delivery.acked",
        "agent.session.open",
        "agent.session.ready",
        "agent.session.error",
        "agent.session.closed",
        "agent.turn.start",
        "agent.turn.accepted",
        "agent.turn.cancel",
        "agent.turn.cancelled",
        "agent.tool.cancel",
        "agent.tool.cancelled",
        "agent.turn.resume",
        "agent.turn.resumed",
        "agent.turn.event",
        "agent.turn.result",
        "invalid_json",
        "disconnect",
    }
)
_TIMELINE_EVENT_LABELS = frozenset(
    {
        "human_message",
        "assistant_content",
        "actor_final_text",
        "system_notice",
        "tool_started",
        "tool_finished",
        "assign_started",
        "assign_finished",
    }
)


def record_registry_operation(*, operation: str, status: str) -> None:
    REGISTRY_OPERATIONS_TOTAL.labels(operation or "unknown", status or "unknown").inc()


def record_workgroup_ws_event(*, direction: str, event: str) -> None:
    direction_label = direction if direction in {"inbound", "outbound", "lifecycle"} else "other"
    event_label = event if event in _WS_EVENT_LABELS else "other"
    WORKGROUP_WS_EVENTS_TOTAL.labels(direction_label, event_label).inc()


def record_workgroup_timeline_event(event_type: str) -> None:
    label = event_type if event_type in _TIMELINE_EVENT_LABELS else "other"
    WORKGROUP_TIMELINE_EVENTS_TOTAL.labels(label).inc()


def metrics_text() -> tuple[bytes, str]:
    return generate_latest(), CONTENT_TYPE_LATEST
