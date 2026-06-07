"""Register Center Prometheus 指标（A2A relay/broadcast 观测）。"""

from __future__ import annotations

import re

from prometheus_client import CONTENT_TYPE_LATEST, Counter, Histogram, generate_latest

A2A_OPERATIONS_TOTAL = Counter(
    "dagents_a2a_operations_total",
    "A2A 操作次数（按组件、操作与结果状态聚合）",
    labelnames=("component", "operation", "status"),
)
A2A_OPERATION_LATENCY_SECONDS = Histogram(
    "dagents_a2a_operation_latency_seconds",
    "A2A 操作耗时秒数（按组件、操作与结果状态聚合）",
    labelnames=("component", "operation", "status"),
)
A2A_TERMINAL_STATES_TOTAL = Counter(
    "dagents_a2a_terminal_states_total",
    "A2A 流式任务终态次数（按组件、操作与终态聚合）",
    labelnames=("component", "operation", "final_state"),
)


def sanitize_prometheus_label_value(value: str | None, *, max_len: int = 160) -> str:
    """将任意字符串规范为安全的 Prometheus label 值。"""
    s = (value or "").strip()
    if not s:
        return "_empty"
    safe = re.sub(r"[^a-zA-Z0-9_.:-]+", "_", s).strip("_") or "_"
    return safe[:max_len]


def record_a2a_operation(
    *,
    component: str,
    operation: str,
    status: str,
    elapsed_seconds: float | None = None,
) -> None:
    """递增 A2A 操作 Counter，并在提供耗时时写入 Histogram。"""
    component_label = sanitize_prometheus_label_value(component or "unknown", max_len=80)
    operation_label = sanitize_prometheus_label_value(operation or "unknown", max_len=80)
    status_label = sanitize_prometheus_label_value(status or "unknown", max_len=80)
    A2A_OPERATIONS_TOTAL.labels(component_label, operation_label, status_label).inc()
    if elapsed_seconds is not None:
        A2A_OPERATION_LATENCY_SECONDS.labels(component_label, operation_label, status_label).observe(
            max(0.0, float(elapsed_seconds))
        )


def record_a2a_terminal_state(*, component: str, operation: str, final_state: str) -> None:
    """记录 A2A 流式任务终态。"""
    component_label = sanitize_prometheus_label_value(component or "unknown", max_len=80)
    operation_label = sanitize_prometheus_label_value(operation or "unknown", max_len=80)
    final_state_label = sanitize_prometheus_label_value(final_state or "unknown", max_len=80)
    A2A_TERMINAL_STATES_TOTAL.labels(component_label, operation_label, final_state_label).inc()


def metrics_text() -> tuple[bytes, str]:
    """生成 Prometheus 文本 exposition 与 Content-Type。"""
    return generate_latest(), CONTENT_TYPE_LATEST
