"""Prometheus 指标定义与 LLM token 观测（供 `/metrics` 抓取）。

与 OpenAI Chat Completions 流式对齐：需在请求中开启 `include_usage`，
在流末尾 chunk 的 `usage` 上读取 `prompt_tokens` / `completion_tokens`。

说明：`usage` 中的 prompt/completion 计数在部分网关侧已为**进程或账号维度累计值**，
此处用 **Gauge + `set`** 直接反映上报快照，而不用 Counter 再次累加。
"""

from __future__ import annotations

import re
from typing import Any

from prometheus_client import CONTENT_TYPE_LATEST, Gauge, generate_latest

# --- LLM token（按模型名分桶；值为提供商 usage 上报快照，多为网关侧已累计计数）---
LLM_PROMPT_TOKENS = Gauge(
    "dagents_llm_prompt_tokens",
    "LLM 输入 token 数快照（提供商 usage.prompt_tokens；通常为已累计值，非本进程二次累加）",
    ("model",),
)
LLM_COMPLETION_TOKENS = Gauge(
    "dagents_llm_completion_tokens",
    "LLM 输出 token 数快照（提供商 usage.completion_tokens；通常为已累计值，非本进程二次累加）",
    ("model",),
)

# --- Session 对话上下文（`OpenAIConversationContext.messages`，由 AgentService 在上下文变更时刷新）---
SESSION_CONTEXT_MESSAGES_COUNT = Gauge(
    "dagents_session_context_messages_count",
    "各 session 内 `OpenAIConversationContext.messages` 条数（OpenAI 对话列表）",
    labelnames=("session_id",),
)

_session_context_sessions_raw: set[str] = set()


def sanitize_model_label(model: str) -> str:
    """将模型名规范为安全的 Prometheus label 值。

    逻辑：
    1. 去首尾空白，空则返回 `unknown`；
    2. 将非 [A-Za-z0-9_.:-] 替换为 `_`；
    3. 截断至 128 字符。

    关键边界：
    - 不保证全局唯一，仅作观测维度；同一进程内与 `get_model_config()['model']` 一致即可。
    """
    s = (model or "").strip()
    if not s:
        return "unknown"
    safe = re.sub(r"[^a-zA-Z0-9_.:-]+", "_", s).strip("_") or "unknown"
    return safe[:128]


def sanitize_prometheus_label_value(value: str | None, *, max_len: int = 160) -> str:
    """将任意字符串规范为安全的 Prometheus label 值（session_id / source / client_id 等）。

    逻辑：与 **`sanitize_model_label`** 同类替换；空串返回 **`_empty`**，避免裸空 label。
    """
    s = (value or "").strip()
    if not s:
        return "_empty"
    safe = re.sub(r"[^a-zA-Z0-9_.:-]+", "_", s).strip("_") or "_"
    return safe[:max_len]


def refresh_session_context_metrics(session_contexts: dict[str, Any]) -> None:
    """按当前 AgentService 的 **`_session_contexts`** 快照刷新 Prometheus Gauge。

    逻辑：
    1. 对比上一轮出现的 **`session_id`**，对已消失的 session **`SESSION_CONTEXT_MESSAGES_COUNT.remove`**；
    2. 对每个 **`OpenAIConversationContext`** 读取 **`messages`**，写入 **`SESSION_CONTEXT_MESSAGES_COUNT`**。

    关键边界：
    - **`session_contexts={}`**（如服务 **`stop`**）清空本模块跟踪的全部上下文指标；
    - **不在 metrics 中展开单条消息的 content 长度**。

    副作用：
    - 修改 Prometheus Registry 中对应 Gauge。
    """
    global _session_context_sessions_raw

    curr_sessions_raw = set(session_contexts.keys())
    for sid_raw in _session_context_sessions_raw - curr_sessions_raw:
        sid_label = sanitize_prometheus_label_value(sid_raw)
        try:
            SESSION_CONTEXT_MESSAGES_COUNT.remove(sid_label)
        except KeyError:
            pass

    for sid_raw, ctx in session_contexts.items():
        sid_label = sanitize_prometheus_label_value(sid_raw)
        messages = getattr(ctx, "messages", None)
        if not isinstance(messages, list):
            messages = []

        SESSION_CONTEXT_MESSAGES_COUNT.labels(sid_label).set(len(messages))

    _session_context_sessions_raw = set(curr_sessions_raw)


def parse_usage_tokens(usage: Any) -> tuple[int, int]:
    """从 OpenAI SDK `CompletionUsage` 或等价 dict 解析 prompt/completion token 数。

    逻辑：
    1. `usage` 为 None 返回 (0, 0)；
    2. dict 时读 `prompt_tokens` / `completion_tokens`（缺省按 0）；
    3. 否则用 `getattr` 读取同名属性。

    关键边界：
    负值按 0 处理；非 int 可转则 `int()`，否则视为 0。
    """
    if usage is None:
        return (0, 0)

    def _pick_pt_ct(obj: Any) -> tuple[int, int]:
        if isinstance(obj, dict):
            raw_p, raw_c = obj.get("prompt_tokens"), obj.get("completion_tokens")
        else:
            raw_p, raw_c = getattr(obj, "prompt_tokens", None), getattr(obj, "completion_tokens", None)

        def _n(x: Any) -> int:
            if x is None:
                return 0
            try:
                v = int(x)
            except (TypeError, ValueError):
                return 0
            return max(0, v)

        return (_n(raw_p), _n(raw_c))

    return _pick_pt_ct(usage)


def usage_fields_from_openai_usage(usage: Any) -> dict[str, int | None]:
    """从 OpenAI `usage` 对象提取可序列化字段，供 SSE `usage` 事件与 runtime 透传。

    逻辑：
    1. 调用 **`parse_usage_tokens`** 得到非负 prompt/completion；
    2. 读取 **`total_tokens`**（dict 键或属性），可解析且非负则返回 int，否则 `None`。

    关键边界：
    - `usage` 为 None 时返回全 0 与 **`total_tokens=None`**。
    """
    pt, ct = parse_usage_tokens(usage)
    tt: int | None = None
    if usage is not None:
        raw = usage.get("total_tokens") if isinstance(usage, dict) else getattr(usage, "total_tokens", None)
        if raw is not None:
            try:
                v = int(raw)
            except (TypeError, ValueError):
                v = -1
            if v >= 0:
                tt = v
    return {"prompt_tokens": pt, "completion_tokens": ct, "total_tokens": tt}


def record_llm_token_usage(*, prompt_tokens: int, completion_tokens: int, model: str) -> None:
    """将单次 completions 调用的 usage 写入 Gauge（`set`，不再二次累加）。

    逻辑：
    1. 规范化 `model` label；
    2. prompt / completion 分别 **`set(max(0, value))`**（同一 `usage` 回调内两项一并更新）。

    副作用：
    - 修改进程内 Prometheus 注册表中的 Gauge 样本。

    关键边界：
    - 全零则不做任何事（兼容未返回 usage 的流式响应）；
    - 与 Counter 不同：多次上报时以后一次 **`set`** 为准，适用于上游已为累计计数的情形。
    """
    if prompt_tokens <= 0 and completion_tokens <= 0:
        return
    m = sanitize_model_label(model)
    LLM_PROMPT_TOKENS.labels(model=m).set(max(0, int(prompt_tokens)))
    LLM_COMPLETION_TOKENS.labels(model=m).set(max(0, int(completion_tokens)))


def metrics_text() -> tuple[bytes, str]:
    """生成当前进程注册表下的 Prometheus 文本与 Content-Type。

    与外部交互：
    - 调用 `prometheus_client.generate_latest()`，无网络 I/O。
    """
    return generate_latest(), CONTENT_TYPE_LATEST
