"""OpenAI 原生 tool calling 的隐式 ReAct 运行时（仅推理，无会话/持久化）。"""

from __future__ import annotations

import asyncio
import json
import logging
from typing import Any, AsyncIterator

from app.config.settings import get_settings
from app.context.models import OpenAIConversationContext, PendingToolCall, RunTurnPhase
from app.core.main_agent.display_inference import (
    infer_assistant_delta_display_type,
    infer_reasoning_delta_display_type,
    infer_tool_call_display_type,
)
from app.core.main_agent.model import get_model_config, get_openai_client
from app.core.main_agent.prompt import get_system_prompt
from app.harness.history.raw_message_journal import append_openai_message_with_journal
from app.harness.service.interface import AgentEventEnvelope
from app.harness.tools.tool import build_openai_toolkit, parse_tool_arguments
from app.observability.metrics import (
    record_llm_token_usage,
    record_system_prompt_observation,
    usage_fields_from_openai_usage,
)

_logger = logging.getLogger(__name__)


def estimate_messages_total_tokens(messages: list[dict[str, Any]], *, system_prompt: str = "") -> int:
    """在 provider 未返回 usage 时粗估当前上下文 token 数。"""
    try:
        messages_text = json.dumps(messages, ensure_ascii=False, sort_keys=True, default=str)
    except Exception:  # noqa: BLE001
        messages_text = repr(messages)
    total_chars = len(str(system_prompt or "")) + len(messages_text)
    message_overhead = max(0, len(messages)) * 4
    return max(1, (total_chars + 3) // 4 + message_overhead)


def resolve_messages_total_tokens(
    messages: list[dict[str, Any]],
    *,
    system_prompt: str = "",
    usage_total_tokens: int | None = None,
) -> int:
    """优先使用 provider usage；缺失时退化为本地估算。"""
    if usage_total_tokens is not None and usage_total_tokens >= 0:
        return int(usage_total_tokens)
    return estimate_messages_total_tokens(messages, system_prompt=system_prompt)


class OpenAIImplicitReActRuntime:
    """基于 OpenAI Chat Completions + tools 的隐式 ReAct 运行时。

    职责：
    - 在给定 **`OpenAIConversationContext`** 上执行单轮推理（仅 `human_message` / `tool_message`）；
    - 发起模型调用、解析 `tool_calls` 并产出统一事件信封；
    - 在取消时修补模型流式中的 assistant 半截正文。

    非职责（由上层如 `AgentService` 负责）：
    - `session_id`、队列、sqlite 加载/落盘、会话是否可用等。
    """

    def __init__(self) -> None:
        """初始化 OpenAI 客户端、模型参数、工具表与循环上限。"""
        settings = get_settings()
        self._client = get_openai_client()
        self._model_cfg = get_model_config()
        self._tools_payload, _ = build_openai_toolkit()
        self._max_tool_loops = max(1, int(settings.llm_max_tool_loops))
        self._stream_include_usage = bool(settings.llm_stream_include_usage)

    def flush_cancelled_turn(self, ctx: OpenAIConversationContext) -> None:
        """在消费 `run_turn` 的 Task 收到 `CancelledError` 后修补 `ctx.messages`（下次 OpenAI 请求合法）。

        逻辑：
        1. 若 `assistant_stream_buffer` 非空白，追加一条 `role=assistant`（保留已流式输出的正文）；
        2. 清空 `assistant_stream_buffer`；
        3. 置 `ctx.run_turn_phase = IDLE`（本轮推理已中断结束）。

        关键边界：
        - 不修改 `pending_tool_calls`；
        - 本方法不向外 yield，由上层在 `except CancelledError` 中调用后再 `raise`。
        """
        # 模型流取消：补一条无 tool_calls 的 assistant，避免半截正文丢失；不触发「必须有 tool」规则。
        if (ctx.assistant_stream_buffer or "").strip():
            append_openai_message_with_journal(ctx, {"role": "assistant", "content": ctx.assistant_stream_buffer})
        ctx.assistant_stream_buffer = ""
        ctx.run_turn_phase = RunTurnPhase.IDLE

    async def run_turn(
        self,
        ctx: OpenAIConversationContext,
        *,
        request_type: str,
        content: str | None = None,
    ) -> AsyncIterator[AgentEventEnvelope]:
        """在 `ctx` 上执行单轮并产出统一事件信封。

        职责：根据 `request_type` 更新上下文，并且**仅驱动一次**「模型流式 → 解析 final」推理。

        执行顺序：
        1. 分支入口：`human_message` 仅追加 user，并**将 `ctx.tool_loop_count` 置 0**（新用户话轮重新计算工具链预算）；`tool_message` 不追加新消息（用于外层已改写 `ctx.messages` 后续推理），**不**清零该计数；
        2. 单次模型请求：`_request_model_stream` → 流式转发 **`assistant` / `reasoning` / `tool_call_delta`**、**`usage`**；**`finish_reason`** 经 **`done` + `payload.finish_reason`** 透出 → 读取 `final` 整包；
        3. 若 `final` 含 `tool_calls`：写入 assistant、填充 `ctx.pending_tool_calls`、发 `tool_call` 并返回；
        4. 若无 `tool_calls`：写入最终 assistant 并置 `IDLE`（**`done`** 已在流式 **`finish_reason`** 分片发出，不在此重复）。

        Tool 执行时机（与 OpenAI 隐式 ReAct + 人工审批对齐）：
        - runtime **不执行工具、不处理审批/拒绝、不处理 tool_result 回灌**；
        - 当模型输出 `tool_calls` 时仅登记 pending，并把执行控制权交回外层编排层。

        关键边界：
        - 任意 `request_type` 下 `content` 为空白：报错并结束；
        - 单次 `run_turn` 最多只做一次模型请求；多轮 tool 必须由外层多次调用 `run_turn`（**`tool_message`** 链上累计 **`tool_loop_count`**，**`human_message`** 会清零后再计入本轮模型前 **`+= 1`**，受 **`_max_tool_loops`** 限制）。

        与外部交互：
        - OpenAI：`chat.completions.create(stream=True)`（见 `_request_model_stream`）；
        - 工具：本方法不直接执行工具；仅产出 `tool_call` 事件以便外层统一编排。

        副作用说明：
        - 就地更新 **`ctx.run_turn_phase`**（`RunTurnPhase`），便于上层与持久化层观测当前阶段；
        - **`request_type == human_message`** 时清零 **`ctx.tool_loop_count`**；**`tool_message`** 沿用既有累计值直至无 **`tool_calls`** 的 assistant 收口时再清零；
        - 每条 **`done`** 信封的 **`payload`** 均含 **`finish_reason`**（OpenAI 末包值或本层语义字符串）。

        Args:
            ctx: 当前会话的 OpenAI 消息与 pending 状态（由上层注入）。
            request_type: `human_message` 或 `tool_message`。
            content: 本轮输入正文；任何 `request_type` 下都必须为非空白文本。
        """
        ctx.assistant_stream_buffer = ""
        ctx.run_turn_phase = RunTurnPhase.BRANCH_RESOLVING
        # 统一输入约束：外层调用 runtime 时必须提供可追踪的非空内容（human/tool 两类一致）。
        if not content or not content.strip():
            ctx.run_turn_phase = RunTurnPhase.IDLE
            yield self._ev("error", {"message": "run_turn 请求缺少 content。"})
            yield self._ev("done", {"finish_reason": "error"})
            return
        # ------------------------------------------------------------------ #
        # 分支 A：message —— 仅追加用户消息。                                         #
        # ------------------------------------------------------------------ #
        if request_type == "human_message":
            append_openai_message_with_journal(ctx, {"role": "user", "content": content})
            # 新用户话轮：工具链轮次预算从 0 起算，避免沿用上一轮未收口的累计值误触上限。
            ctx.tool_loop_count = 0
        elif request_type == "tool_message":
            # 外层已更新完 messages（如写入 tool/tool_result）时，tool_message 只负责继续做一轮模型推理。
            pass
        else:
            ctx.run_turn_phase = RunTurnPhase.IDLE
            yield self._ev("error", {"message": f"不支持的 request_type: {request_type}"})
            yield self._ev("done", {"finish_reason": "error"})
            return

        # ------------------------------------------------------------------ #
        # 单轮模型请求：一次完整流式请求 + 一个 final。                      #
        # 若 final 带 tool_calls：只登记 pending 并中断（工具留给外层执行）。 #
        # 若 final 无 tool_calls：得到本轮最终自然语言回复并收口。           #
        # ------------------------------------------------------------------ #
        # 跨回合累计保护：超过上限时直接拒绝新一轮模型请求，避免 tool 相关死循环。
        if ctx.tool_loop_count >= self._max_tool_loops:
            ctx.run_turn_phase = RunTurnPhase.IDLE
            yield self._ev("error", {"message": f"工具调用轮次超过上限：{self._max_tool_loops}"})
            yield self._ev("done", {"finish_reason": "error"})
            return

        # 每次 run_turn 最多消耗一次“模型轮次预算”；若中途因审批中断，该计数留给后续回合继续累计。
        ctx.tool_loop_count += 1
        ctx.run_turn_phase = RunTurnPhase.MODEL_STREAMING
        if request_type == "human_message":
            # human_message 阶段不再按查询自动选技能；仅消费上下文中已加载 skills。
            dynamic_system_prompt = get_system_prompt(context=ctx)
        else:
            dynamic_system_prompt = get_system_prompt(context=ctx)
        model_name = str(getattr(self, "_model_cfg", {}).get("model") or "")
        system_prompt_fp = record_system_prompt_observation(
            model=model_name,
            system_prompt=dynamic_system_prompt,
            message_count=len(ctx.messages),
        )
        model_msg = None
        latest_total_tokens: int | None = None
        ctx.assistant_stream_buffer = ""
        # 流式阶段仅缓存 assistant 正文，供上层 Cancelled 后 flush 为合法 assistant 行。
        try:
            async for model_event in self._request_model_stream(ctx.messages, dynamic_system_prompt):
                event_kind = str(model_event.get("kind") or "")
                # 如果event_kind为assistant_delta，流式yield返回assistant事件，供前端进行流式输出。
                if event_kind == "assistant_delta":
                    delta_text = str(model_event.get("text", ""))
                    if delta_text:
                        ctx.assistant_stream_buffer += delta_text
                        # 构建流式事件envelope，供前端进行流式输出。
                        yield self._ev(
                            "assistant",
                            {
                                "content": delta_text,
                                "display_type": infer_assistant_delta_display_type(delta_text),
                            },
                        )
                    continue
                # 如果event_kind为reasoning_delta，流式yield返回reasoning事件，供前端进行流式输出。
                elif event_kind == "reasoning_delta":
                    reasoning_text = str(model_event.get("text", ""))
                    if reasoning_text:
                        yield self._ev(
                            "reasoning",
                            {
                                "content": reasoning_text,
                                "display_type": infer_reasoning_delta_display_type(),
                            },
                        )
                    continue
                # 流式 tool 分片：与 OpenAI delta 对齐供前端渐进展示；落库与审批仍以 final 合成后的 tool_call 为准。
                elif event_kind == "tool_call_delta":
                    raw_calls = model_event.get("tool_calls")
                    if isinstance(raw_calls, list) and raw_calls:
                        yield self._ev("tool_call_delta", {"tool_calls": raw_calls})
                    continue
                elif event_kind == "usage":
                    # 透传 `usage_fields_from_openai_usage` 全量字段（含 cache/audio 明细）。
                    usage_payload = {k: v for k, v in model_event.items() if k != "kind"}
                    usage_payload.setdefault("system_prompt_chars", len(dynamic_system_prompt))
                    usage_payload.setdefault("system_prompt_fingerprint", system_prompt_fp)
                    prompt_tokens = int(usage_payload.get("prompt_tokens", 0))
                    completion_tokens = int(usage_payload.get("completion_tokens", 0))
                    total_tokens = int(usage_payload.get("total_tokens") or 0)
                    # 优先使用 total_tokens；缺失时退化为 input+output（prompt+completion）。
                    if total_tokens > 0:
                        latest_total_tokens = total_tokens
                    else:
                        merged_tokens = prompt_tokens + completion_tokens
                        if merged_tokens > 0:
                            latest_total_tokens = merged_tokens
                    yield self._ev("usage", usage_payload)
                    continue
                elif event_kind == "finish_reason":
                    # 流式末包结束原因：本回合对外的唯一 `done`（编排层会暂存，与 `tool_call` 等合并后一次下发 SSE）。
                    fr = str(model_event.get("finish_reason") or "").strip()
                    if fr:
                        yield self._ev("done", {"finish_reason": fr})
                    continue
                elif event_kind == "final":
                    model_msg = model_event.get("message")
                    ctx.assistant_stream_buffer = ""
                    break
                else:
                    # 未识别事件类型直接忽略，避免污染当前回合状态机。
                    continue
        except asyncio.CancelledError:
            # 不在此 flush：由 AgentService._handle_message 统一调用 flush_cancelled_turn 后再次抛出。
            raise
        if not isinstance(model_msg, dict):
            ctx.run_turn_phase = RunTurnPhase.IDLE
            yield self._ev("error", {"message": "模型流式响应解析失败。"})
            yield self._ev("done", {"finish_reason": "error"})
            return
        assistant_content = model_msg.get("content", "") or ""
        tool_calls = model_msg.get("tool_calls", [])
        # ----- 子分支：模型要求调用工具（此处仍不 invoke，只登记 + 等人批）----- #
        if tool_calls:
            # 必须把 assistant + tool_calls 写入历史，否则下次请求模型时上下文不完整。
            append_openai_message_with_journal(
                ctx,
                {
                    "role": "assistant",
                    "content": assistant_content or "",
                    "tool_calls": tool_calls,
                },
            )
            # 构建pending_tool_calls列表，记录待执行的tool_calls
            pending: list[PendingToolCall] = []
            payload_calls: list[dict[str, Any]] = []
            # 因为tool_calls是列表，可能不止一个工具调用
            for idx, c in enumerate(tool_calls):
                call_id = str(c.get("id"))
                fn = c.get("function", {}) if isinstance(c.get("function"), dict) else {}
                name = str(fn.get("name") or "")
                args = parse_tool_arguments(fn.get("arguments"))
                pending.append(PendingToolCall(call_id=call_id, name=name, arguments=args))
                payload_calls.append(
                    {"id": call_id, "name": name, "arguments": args, "raw_arguments": fn.get("arguments")}
                )
            ctx.pending_tool_calls = pending
            ctx.messages_total_tokens = resolve_messages_total_tokens(
                ctx.messages,
                system_prompt=dynamic_system_prompt,
                usage_total_tokens=latest_total_tokens,
            )
            yield self._ev(
                "tool_call",
                {
                    "tool_calls": payload_calls,
                    "assistant_content": assistant_content,
                    "display_type": infer_tool_call_display_type(assistant_content, payload_calls),
                },
            )
            ctx.run_turn_phase = RunTurnPhase.AWAITING_TOOL_EXECUTION
            return

        # ----- 子分支：本轮无工具调用，视为最终回复 ----- #
        append_openai_message_with_journal(ctx, {"role": "assistant", "content": assistant_content})
        ctx.messages_total_tokens = resolve_messages_total_tokens(
            ctx.messages,
            system_prompt=dynamic_system_prompt,
            usage_total_tokens=latest_total_tokens,
        )
        # 仅在“无 tool_calls 的 assistant 正常收口”时重置累计循环计数。
        ctx.tool_loop_count = 0
        ctx.run_turn_phase = RunTurnPhase.IDLE
        # 不在此再 yield `done`：正常流式路径已在 `finish_reason` 分片发出；缺省时由编排层按 `final` 兜底补 `stop`。

    async def _request_model_stream(
        self,
        messages: list[dict[str, Any]],
        system_prompt: str,
    ) -> AsyncIterator[dict[str, Any]]:
        """发起一次流式模型请求，逐步产出 delta，并在流末尾合成 **`final`**。

        除 **`final`** 外，本异步生成器可能 **`yield`** 的下级事件如下（`kind` 字段）；**`run_turn`** 消费
        **`assistant_delta` / `reasoning_delta` / `tool_call_delta` / `usage` / `finish_reason` / `final`**；其中 **`finish_reason`** 在 **`run_turn`** 内转为 **`done` + `payload.finish_reason`** 再外发。

        | `kind` | 触发条件 | 载荷要点 |
        |--------|----------|----------|
        | **`usage`** | **`chunk.usage` 非空**（常与空 **`choices`** 同现；亦可能与其它字段同 chunk） | `usage_fields_from_openai_usage` 全量可数字段 + 写 Prometheus |
        | **`tool_call_delta`** | **`delta.tool_calls`** 非空且 **`role`** 为 **`assistant`/空** | 当前分片聚合后的 **`tool_calls`** 片段列表（含 **`index`**） |
        | **`assistant_delta`** | 同上且 **`delta.content`** 非空 | **`text`** 为正文分片 |
        | **`reasoning_delta`** | 同上、无 **`tool_calls`**、无 **`content`** 且 **`delta.reasoning_content`** 非空 | **`text`** 为推理分片 |
        | **`finish_reason`** | **`choice.finish_reason` 非空**（经 **`str(...).strip()`**） | **`finish_reason`**：如 **`stop`**、**`tool_calls`** 等，仅通知结束原因，**不**合并 **`choice.message`** |
        | **`final`** | **`async for` 流结束**后唯一一条 | **`message`**：`content` + **`tool_calls`**（delta 累加）；**`finish_reason`**：流式末包最后一次非空 **`choice.finish_reason`**（可能为空串） |

        逻辑（执行顺序）：

        1. **`stream=True`** 调用 **`chat.completions`**；可选 **`stream_options.include_usage`**；
        2. 对每个 **`chunk`**：先处理 **`usage`**，再处理 **`choices[0]`** 的 **`delta`**（与现有一致）；
        3. 若 **`choice.finish_reason`** 经规范化后非空：**`yield finish_reason`** 一条（**不**提前 `break`，避免漏掉末尾 **`usage`** 分片）；
        4. 流结束后构造 **`tool_calls` 列表** 并 **`yield final`**。

        与外部交互：
        - OpenAI 兼容 **`ChatCompletionChunk`**；**DEBUG** 下对每 **`chunk`** 打 **`%r`**。

        异常说明：
        - 网络/SDK 异常由调用方 **`run_turn`** 的 **`try/except`** 处理。
        """
        kwargs: dict[str, Any] = {
            "model": self._model_cfg["model"],
            "messages": [{"role": "system", "content": system_prompt}, *messages],
            "tools": self._tools_payload,
            "temperature": self._model_cfg["temperature"],
            "stream": True,
        }
        # 如果包含usage，则设置stream_options.include_usage为True
        if self._stream_include_usage:
            kwargs["stream_options"] = {"include_usage": True}
        # 如果包含extra_body，则设置extra_body
        # todo：某些网关不支持extra_body，需要额外处理
        extra_body = self._model_cfg.get("extra_body") or {}
        if extra_body:
            kwargs["extra_body"] = extra_body
        stream = await self._client.chat.completions.create(**kwargs)
        # 流式阶段只累积；最终一条 final 与 OpenAI 非流式 message 结构对齐，供 run_turn 分支判断 tool_calls。
        full_content: str = ""
        tool_calls_acc: dict[int, dict[str, Any]] = {}
        model_name = str(self._model_cfg.get("model") or "")
        # 与流式 `finish_reason` 分片同源：挂到 `final` 上供 `run_turn` 收口 `done` 使用（网关偶发缺末包时可为空）。
        stream_tail_finish_reason = ""
        async for chunk in stream:
            # DEBUG：直接输出 SDK 流式分片对象（由 `%r` 展示原始 repr）。
            _logger.debug("openai chat.completions stream chunk: %r", chunk)
            usage = getattr(chunk, "usage", None)
            # 如果包含usage，则记录usage并yield
            if usage is not None:
                fields = usage_fields_from_openai_usage(usage)
                pt, ct = int(fields["prompt_tokens"]), int(fields["completion_tokens"])
                record_llm_token_usage(
                    prompt_tokens=pt, completion_tokens=ct, model=model_name, usage=usage
                )
                yield {"kind": "usage", **fields}
            choices = getattr(chunk, "choices", None) or []
            # 如果choices为空，则继续下一个chunk
            if not choices:
                continue
            choice = choices[0]
            fr = getattr(choice, "finish_reason", None)
            delta = getattr(choice, "delta", None)
            if delta is not None:
                role = getattr(delta, "role", None)
                if role in (None, "assistant"):
                    tc_list = getattr(delta, "tool_calls", None) or []
                    # 如果tool_calls不为空，则处理tool_calls
                    if tc_list:
                        # 同一轮里多个 tool 或参数分片：用 index 聚合到 tool_calls_acc，流结束后再排序输出。
                        delta_calls: list[dict[str, Any]] = []
                        for tc in tc_list:
                            # 取idx作为key，如果idx不存在，则创建一个新dict
                            idx = int(getattr(tc, "index", 0) or 0)
                            item = tool_calls_acc.setdefault(
                                idx,
                                {
                                    "id": None,
                                    "type": "function",
                                    "function": {"name": "", "arguments": ""},
                                },
                            )
                            tc_id = getattr(tc, "id", None)
                            if tc_id:
                                item["id"] = tc_id
                            tc_type = getattr(tc, "type", None)
                            if tc_type:
                                item["type"] = tc_type
                            fn = getattr(tc, "function", None)
                            if fn is not None:
                                fn_name = getattr(fn, "name", None)
                                fn_args = getattr(fn, "arguments", None)
                                if fn_name:
                                    item["function"]["name"] += str(fn_name)
                                if fn_args:
                                    item["function"]["arguments"] += str(fn_args)
                            delta_calls.append(
                                {
                                    "index": idx,
                                    "id": item.get("id"),
                                    "type": item.get("type", "function"),
                                    "function": {
                                        "name": item.get("function", {}).get("name", ""),
                                        "arguments": item.get("function", {}).get("arguments", ""),
                                    },
                                }
                            )
                        yield {"kind": "tool_call_delta", "tool_calls": delta_calls}
                    else:
                        # 与 tool 分片互斥：同一 chunk 通常不会同时带正文与 tool_calls。
                        content_delta = getattr(delta, "content", None)
                        reasoning_delta = getattr(delta, "reasoning_content", None)
                        if content_delta:
                            full_content += str(content_delta)
                            yield {"kind": "assistant_delta", "text": str(content_delta)}
                        elif reasoning_delta:
                            yield {"kind": "reasoning_delta", "text": str(reasoning_delta)}
            # 末包常带 finish_reason：只把结束原因交给上层，不累加 choice.message（与 delta 权威一致）。
            if fr is not None:
                fr_s = str(fr).strip()
                if fr_s:
                    stream_tail_finish_reason = fr_s
                    yield {"kind": "finish_reason", "finish_reason": fr_s}

        tool_calls: list[dict[str, Any]] = []
        # 排序tool_calls_acc，并转换为tool_calls列表
        for idx in sorted(tool_calls_acc):
            item = tool_calls_acc[idx]
            tool_calls.append(
                {
                    "id": item.get("id"),
                    "type": item.get("type", "function"),
                    "function": {
                        "name": item.get("function", {}).get("name", ""),
                        "arguments": item.get("function", {}).get("arguments", ""),
                    },
                }
            )
        yield {
            "kind": "final",
            "message": {"content": full_content, "tool_calls": tool_calls},
            "finish_reason": stream_tail_finish_reason,
        }

    @staticmethod
    def _ev(event_type: str, payload: dict[str, Any]) -> AgentEventEnvelope:
        """构造统一事件信封。"""
        return AgentEventEnvelope(event_type=event_type, payload=payload, meta={})
