"""Prometheus 指标定义与 LLM token 观测（供 `/metrics` 抓取）。

与 OpenAI Chat Completions 流式对齐：需在请求中开启 `include_usage`，
在流式 **`usage`** chunk 上读取 `prompt_tokens` / `completion_tokens`，
以及（若提供商返回）`prompt_tokens_details` 与 `prompt_cache_*` 相关计数。

说明：以下指标为 **Counter（`inc`）**，在进程内 **按每次 `usage` 分片上报的数值累加**。
约定 **`usage` 表示「当前 completion 请求」的消耗**（OpenAI 及多数兼容网关的语义）。
若某网关在 `usage` 中返回 **账号级终身累计**，则会导致 **重复累加**，需由网关或接入层改为按请求口径再接入本指标。
"""

from __future__ import annotations

import re
from typing import Any

from prometheus_client import CONTENT_TYPE_LATEST, Counter, Gauge, generate_latest

# --- LLM token（按模型名分桶；进程内 Counter 累计，每次 record 对本轮 usage 做 inc）---
LLM_PROMPT_TOKENS = Counter(
    "dagents_llm_prompt_tokens_total",
    "LLM 输入 token 累计（按 model；每次 completions usage 的 prompt_tokens 增量 inc）",
    ("model",),
)
LLM_COMPLETION_TOKENS = Counter(
    "dagents_llm_completion_tokens_total",
    "LLM 输出 token 累计（按 model；每次 usage 的 completion_tokens 增量 inc）",
    ("model",),
)
LLM_PROMPT_AUDIO_TOKENS = Counter(
    "dagents_llm_prompt_audio_tokens_total",
    "usage.prompt_tokens_details.audio_tokens 累计（按 model）",
    ("model",),
)
LLM_PROMPT_CACHED_TOKENS = Counter(
    "dagents_llm_prompt_cached_tokens_total",
    "usage.prompt_tokens_details.cached_tokens 累计（按 model）",
    ("model",),
)
LLM_PROMPT_CACHE_HIT_TOKENS = Counter(
    "dagents_llm_prompt_cache_hit_tokens_total",
    "usage.prompt_cache_hit_tokens 累计（按 model）",
    ("model",),
)
LLM_PROMPT_CACHE_MISS_TOKENS = Counter(
    "dagents_llm_prompt_cache_miss_tokens_total",
    "usage.prompt_cache_miss_tokens 累计（按 model）",
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


def parse_usage_prompt_cache_details(usage: Any) -> dict[str, int]:
    """从 CompletionUsage 读取 prompt 侧 audio/cached 与 cache hit/miss（缺失视为 0）。

    逻辑：
    1. **`prompt_cache_*`** 从 **`usage` 顶层**读取；
    2. **`audio_tokens` / `cached_tokens`** 从 **`prompt_tokens_details`**（兼容 **`prompt_token_details`**）读取；
    3. 解析失败或非数值一律按 0。

    与外部交互：
    - 仅读 SDK 返回对象或等价 dict，不写回提供商。
    """

    def _nz(x: Any) -> int:
        if x is None:
            return 0
        try:
            return max(0, int(x))
        except (TypeError, ValueError):
            return 0

    if usage is None:
        return {
            "prompt_audio_tokens": 0,
            "prompt_cached_tokens": 0,
            "prompt_cache_hit_tokens": 0,
            "prompt_cache_miss_tokens": 0,
        }
    if isinstance(usage, dict):
        hit = _nz(usage.get("prompt_cache_hit_tokens"))
        miss = _nz(usage.get("prompt_cache_miss_tokens"))
        ptd = usage.get("prompt_tokens_details") or usage.get("prompt_token_details")
    else:
        hit = _nz(getattr(usage, "prompt_cache_hit_tokens", None))
        miss = _nz(getattr(usage, "prompt_cache_miss_tokens", None))
        ptd = getattr(usage, "prompt_tokens_details", None)

    audio = 0
    cached = 0
    if ptd is not None:
        if isinstance(ptd, dict):
            audio = _nz(ptd.get("audio_tokens"))
            cached = _nz(ptd.get("cached_tokens"))
        else:
            audio = _nz(getattr(ptd, "audio_tokens", None))
            cached = _nz(getattr(ptd, "cached_tokens", None))

    return {
        "prompt_audio_tokens": audio,
        "prompt_cached_tokens": cached,
        "prompt_cache_hit_tokens": hit,
        "prompt_cache_miss_tokens": miss,
    }


def usage_fields_from_openai_usage(usage: Any) -> dict[str, Any]:
    """从 OpenAI `usage` 对象提取可序列化字段，供 SSE `usage` 事件与 runtime 透传。

    逻辑：
    1. 调用 **`parse_usage_tokens`** 得到非负 prompt/completion；
    2. 读取 **`total_tokens`**（dict 键或属性），可解析且非负则返回 int，否则 `None`；
    3. 合并 **`parse_usage_prompt_cache_details`**（audio/cached/cache hit/miss）。

    关键边界：
    - `usage` 为 None 时返回全 0 与 **`total_tokens=None`**，cache 明细亦为 0。
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
    base: dict[str, Any] = {"prompt_tokens": pt, "completion_tokens": ct, "total_tokens": tt}
    base.update(parse_usage_prompt_cache_details(usage))
    return base


def record_llm_token_usage(
    *,
    prompt_tokens: int,
    completion_tokens: int,
    model: str,
    usage: Any | None = None,
) -> None:
    """将单次 completions **`usage`** 上报的 token 数 **累加** 到 Prometheus Counter（`inc`）。

    逻辑：
    1. 规范化 **`model`** label；
    2. **`prompt_tokens` / `completion_tokens`** 大于 0 时对对应 Counter **`inc`**；
    3. 若传入原始 **`usage`**：对 audio/cached/cache hit/miss 四类解析结果分别 **`inc`**（仅 **>0** 时调用，避免无意义 `inc(0)`）。

    副作用：
    - 修改进程内 Prometheus 注册表中的 Counter 样本（单调增）。

    关键边界：
    - **`prompt_tokens`/`completion_tokens` 全为 0 且 `usage` 为 None** 时直接返回；
    - 若 **`usage` 存在**：即使 prompt/completion 为 0，仍会对四类 cache 明细中 **>0** 的项 **`inc`**；
    - 依赖上游 **`usage` 为「当前请求消耗」**；若为账号级累计值会导致重复计量（见模块 docstring）。
    """
    if prompt_tokens <= 0 and completion_tokens <= 0 and usage is None:
        return
    m = sanitize_model_label(model)
    pt = max(0, int(prompt_tokens))
    ct = max(0, int(completion_tokens))
    if pt > 0:
        LLM_PROMPT_TOKENS.labels(model=m).inc(pt)
    if ct > 0:
        LLM_COMPLETION_TOKENS.labels(model=m).inc(ct)
    if usage is not None:
        ex = parse_usage_prompt_cache_details(usage)
        # 与上游 usage 分片对齐：缺失字段已在 parse 中归零；仅正数参与累计。
        audio = ex["prompt_audio_tokens"]
        cached = ex["prompt_cached_tokens"]
        hit = ex["prompt_cache_hit_tokens"]
        miss = ex["prompt_cache_miss_tokens"]
        if audio > 0:
            LLM_PROMPT_AUDIO_TOKENS.labels(model=m).inc(audio)
        if cached > 0:
            LLM_PROMPT_CACHED_TOKENS.labels(model=m).inc(cached)
        if hit > 0:
            LLM_PROMPT_CACHE_HIT_TOKENS.labels(model=m).inc(hit)
        if miss > 0:
            LLM_PROMPT_CACHE_MISS_TOKENS.labels(model=m).inc(miss)


def metrics_text() -> tuple[bytes, str]:
    """生成当前进程注册表下的 Prometheus 文本与 Content-Type。

    与外部交互：
    - 调用 `prometheus_client.generate_latest()`，无网络 I/O。
    """
    return generate_latest(), CONTENT_TYPE_LATEST
