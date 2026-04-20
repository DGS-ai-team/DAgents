"""Prometheus 指标定义与 LLM token 累计（供 `/metrics` 抓取）。

与 OpenAI Chat Completions 流式对齐：需在请求中开启 `include_usage`，
在流末尾 chunk 的 `usage` 上读取 `prompt_tokens` / `completion_tokens`。
"""

from __future__ import annotations

import re
from typing import Any

from prometheus_client import CONTENT_TYPE_LATEST, Counter, generate_latest

# --- LLM token（按模型名分桶，单部署通常 1 个取值，cardinality 可控）---
LLM_PROMPT_TOKENS = Counter(
    "dagents_llm_prompt_tokens_total",
    "LLM 累计输入 token 数（提供商 usage.prompt_tokens）",
    ("model",),
)
LLM_COMPLETION_TOKENS = Counter(
    "dagents_llm_completion_tokens_total",
    "LLM 累计输出 token 数（提供商 usage.completion_tokens）",
    ("model",),
)


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
    """将单次 completions 调用的 usage 累加到 Counter。

    逻辑：
    1. 规范化 `model` label；
    2. prompt / completion 分别 `inc`（仅在大于 0 时），避免无意义时间序列。

    副作用：
    - 修改进程内 Prometheus 注册表中的 Counter 样本。

    关键边界：
    - 全零则不做任何事（兼容未返回 usage 的流式响应）。
    """
    if prompt_tokens <= 0 and completion_tokens <= 0:
        return
    m = sanitize_model_label(model)
    if prompt_tokens > 0:
        LLM_PROMPT_TOKENS.labels(model=m).inc(prompt_tokens)
    if completion_tokens > 0:
        LLM_COMPLETION_TOKENS.labels(model=m).inc(completion_tokens)


def metrics_text() -> tuple[bytes, str]:
    """生成当前进程注册表下的 Prometheus 文本与 Content-Type。

    与外部交互：
    - 调用 `prometheus_client.generate_latest()`，无网络 I/O。
    """
    return generate_latest(), CONTENT_TYPE_LATEST
