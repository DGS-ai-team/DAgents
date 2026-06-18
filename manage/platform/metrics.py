"""Manage Prometheus 指标。"""

from __future__ import annotations

from prometheus_client import CONTENT_TYPE_LATEST, Counter, generate_latest

REGISTRY_OPERATIONS_TOTAL = Counter(
    "dagents_manage_registry_operations_total",
    "Manage Registry 操作次数",
    labelnames=("operation", "status"),
)

A2A_OPERATIONS_TOTAL = Counter(
    "dagents_manage_a2a_operations_total",
    "Manage A2A Task 操作次数",
    labelnames=("operation", "status"),
)


def record_registry_operation(*, operation: str, status: str) -> None:
    REGISTRY_OPERATIONS_TOTAL.labels(operation or "unknown", status or "unknown").inc()


def record_a2a_operation(*, operation: str, status: str) -> None:
    A2A_OPERATIONS_TOTAL.labels(operation or "unknown", status or "unknown").inc()


def metrics_text() -> tuple[bytes, str]:
    return generate_latest(), CONTENT_TYPE_LATEST
