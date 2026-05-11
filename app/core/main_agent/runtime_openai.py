"""OpenAI 原生 tool calling 的隐式 ReAct 运行时（仅推理，无会话/持久化）。"""

from __future__ import annotations

import asyncio
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
from app.observability.metrics import record_llm_token_usage, usage_fields_from_openai_usage

_logger = logging.getLogger(__name__)


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
        1. 分支入口：`human_message` 仅追加 user；`tool_message` 不追加新消息（用于外层已改写 `ctx.messages` 后续推理）；
        2. 单次模型请求：`_request_model_stream` → 流式转发 delta → 读取 `final` 整包；
        3. 若 `final` 含 `tool_calls`：写入 assistant、填充 `ctx.pending_tool_calls`、发 `tool_call` 并返回；
        4. 若无 `tool_calls`：写入最终 assistant、`done` 后返回。

        Tool 执行时机（与 OpenAI 隐式 ReAct + 人工审批对齐）：
        - runtime **不执行工具、不处理审批/拒绝、不处理 tool_result 回灌**；
        - 当模型输出 `tool_calls` 时仅登记 pending，并把执行控制权交回外层编排层。

        关键边界：
        - 任意 `request_type` 下 `content` 为空白：报错并结束；
        - 单次 `run_turn` 最多只做一次模型请求；多轮 tool 必须由外层多次调用 `run_turn`（受 `_max_tool_loops` 限制）。

        与外部交互：
        - OpenAI：`chat.completions.create(stream=True)`（见 `_request_model_stream`）；
        - 工具：本方法不直接执行工具；仅产出 `tool_call` 事件以便外层统一编排。

        副作用说明：
        - 就地更新 **`ctx.run_turn_phase`**（`RunTurnPhase`），便于上层与持久化层观测当前阶段。

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
            yield self._ev("done", {})
            return
        # ------------------------------------------------------------------ #
        # 分支 A：message —— 仅追加用户消息。                                         #
        # ------------------------------------------------------------------ #
        if request_type == "human_message":
            append_openai_message_with_journal(ctx, {"role": "user", "content": content})
        elif request_type == "tool_message":
            # 外层已更新完 messages（如写入 tool/tool_result）时，tool_message 只负责继续做一轮模型推理。
            pass
        else:
            ctx.run_turn_phase = RunTurnPhase.IDLE
            yield self._ev("error", {"message": f"不支持的 request_type: {request_type}"})
            yield self._ev("done", {})
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
            yield self._ev("done", {})
            return

        # 每次 run_turn 最多消耗一次“模型轮次预算”；若中途因审批中断，该计数留给后续回合继续累计。
        ctx.tool_loop_count += 1
        ctx.run_turn_phase = RunTurnPhase.MODEL_STREAMING
        if request_type == "human_message":
            # human_message 阶段不再按查询自动选技能；仅消费上下文中已加载 skills。
            dynamic_system_prompt = get_system_prompt(context=ctx)
        else:
            dynamic_system_prompt = get_system_prompt(context=ctx)
        model_msg = None
        latest_total_tokens: int | None = None
        ctx.assistant_stream_buffer = ""
        # 流式阶段：同步写入 ctx.assistant_stream_buffer，供上层 Cancelled 后 flush 为合法 assistant 行。
        try:
            # tool_call_delta 由 _request_model_stream 产出但此处不消费；权威 tool_calls 仅来自 final 整包。
            async for model_event in self._request_model_stream(ctx.messages, dynamic_system_prompt):
                event_kind = str(model_event.get("kind") or "")
                if event_kind == "assistant_delta":
                    delta_text = str(model_event.get("text", ""))
                    if delta_text:
                        ctx.assistant_stream_buffer += delta_text
                        yield self._ev(
                            "assistant",
                            {
                                "content": delta_text,
                                "display_type": infer_assistant_delta_display_type(delta_text),
                            },
                        )
                    continue
                elif event_kind == "reasoning_delta":
                    reasoning_text = str(model_event.get("text", ""))
                    if reasoning_text:
                        ctx.assistant_stream_buffer += reasoning_text
                        yield self._ev(
                            "reasoning",
                            {
                                "content": reasoning_text,
                                "display_type": infer_reasoning_delta_display_type(),
                            },
                        )
                    continue
                elif event_kind == "usage":
                    # 透传 `usage_fields_from_openai_usage` 全量字段（含 cache/audio 明细）。
                    usage_payload = {k: v for k, v in model_event.items() if k != "kind"}
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
            yield self._ev("done", {})
            return
        # 本轮 AI 响应返回后，使用 usage 总量刷新消息总 token（仅在元数据可用时更新）。
        if latest_total_tokens is not None and latest_total_tokens >= 0:
            ctx.messages_total_tokens = latest_total_tokens
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
            pending: list[PendingToolCall] = []
            payload_calls: list[dict[str, Any]] = []
            for idx, c in enumerate(tool_calls):
                call_id = str(c.get("id") or f"tool-call-{idx}")
                fn = c.get("function", {}) if isinstance(c.get("function"), dict) else {}
                name = str(fn.get("name") or "")
                args = parse_tool_arguments(fn.get("arguments"))
                pending.append(PendingToolCall(call_id=call_id, name=name, arguments=args))
                payload_calls.append(
                    {"id": call_id, "name": name, "arguments": args, "raw_arguments": fn.get("arguments")}
                )
            ctx.pending_tool_calls = pending
            yield self._ev(
                "tool_call",
                {
                    "tool_calls": payload_calls,
                    "assistant_content": assistant_content,
                    "display_type": infer_tool_call_display_type(assistant_content, payload_calls),
                },
            )
            ctx.run_turn_phase = RunTurnPhase.AWAITING_TOOL_EXECUTION
            yield self._ev("done", {})
            return

        # ----- 子分支：本轮无工具调用，视为最终回复 ----- #
        append_openai_message_with_journal(ctx, {"role": "assistant", "content": assistant_content})
        # 仅在“无 tool_calls 的 assistant 正常收口”时重置累计循环计数。
        ctx.tool_loop_count = 0
        ctx.run_turn_phase = RunTurnPhase.IDLE
        yield self._ev("done", {})

    async def _request_model_stream(
        self,
        messages: list[dict[str, Any]],
        system_prompt: str,
    ) -> AsyncIterator[dict[str, Any]]:
        """发起一次流式模型请求，逐步产出 delta 并在末尾给出最终消息对象。

        逻辑：
        1. 以 `stream=True` 调用 chat.completions；
        2. 若 `role=assistant` 且 `tool_calls` 有值，产出 `tool_call_delta` 并累积参数分片；
        3. 若 `content` 非空，产出 `assistant_delta`；若 `content` 为空但 `reasoning_content` 非空，产出 `reasoning_delta`；
        4. 含 **`usage`** 的 chunk（通常 choices 为空）产出 **`kind=usage`** 字典，供 `run_turn` 发 SSE；
        5. 结束后产出 `final`，包含完整 `content + tool_calls`。

        关键边界：
        - tool 调用参数可能分片返回，需按 `index` 逐段拼接；
        - 空 choices 的 chunk 仍可能携带 **`usage`**：先 **`record_llm_token_usage`**（Gauge **`set`**，映射提供商计数快照）、**`yield usage`**，再 `continue`；
        - 未开启 `include_usage` 或网关不支持时无 **`usage`** 分片。

        与外部交互：
        - 若 **`self._stream_include_usage`** 为真，向 OpenAI 传入 **`stream_options={"include_usage": True}`**；
        - DEBUG 日志下对每个 SDK **`chunk`** 输出 **`%r`**（不做 JSON 再封装）。
        """
        kwargs: dict[str, Any] = {
            "model": self._model_cfg["model"],
            "messages": [{"role": "system", "content": system_prompt}, *messages],
            "tools": self._tools_payload,
            "temperature": self._model_cfg["temperature"],
            "stream": True,
        }
        if self._stream_include_usage:
            kwargs["stream_options"] = {"include_usage": True}
        extra_body = self._model_cfg.get("extra_body") or {}
        if extra_body:
            kwargs["extra_body"] = extra_body
        stream = await self._client.chat.completions.create(**kwargs)
        # 流式阶段只累积；最终一条 final 与 OpenAI 非流式 message 结构对齐，供 run_turn 分支判断 tool_calls。
        full_content: str = ""
        tool_calls_acc: dict[int, dict[str, Any]] = {}
        model_name = str(self._model_cfg.get("model") or "")
        async for chunk in stream:
            # DEBUG：直接输出 SDK 流式分片对象（由 `%r` 展示原始 repr）。
            _logger.debug("openai chat.completions stream chunk: %r", chunk)
            usage = getattr(chunk, "usage", None)
            if usage is not None:
                fields = usage_fields_from_openai_usage(usage)
                pt, ct = int(fields["prompt_tokens"]), int(fields["completion_tokens"])
                record_llm_token_usage(
                    prompt_tokens=pt, completion_tokens=ct, model=model_name, usage=usage
                )
                yield {"kind": "usage", **fields}
            choices = getattr(chunk, "choices", None) or []
            if not choices:
                continue
            choice = choices[0]
            delta = getattr(choice, "delta", None)
            if delta is None:
                continue
            role = getattr(delta, "role", None)
            if role not in (None, "assistant"):
                continue

            tc_list = getattr(delta, "tool_calls", None) or []
            if tc_list:
                # 同一轮里多个 tool 或参数分片：用 index 聚合到 tool_calls_acc，流结束后再排序输出。
                delta_calls: list[dict[str, Any]] = []
                for tc in tc_list:
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
                    # 推理模型：reasoning 与 content 在协议上可能分列，run_turn 里与正文一并记入 assistant_stream_buffer。
                    yield {"kind": "reasoning_delta", "text": str(reasoning_delta)}

        tool_calls: list[dict[str, Any]] = []
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
        yield {"kind": "final", "message": {"content": full_content, "tool_calls": tool_calls}}

    @staticmethod
    def _ev(event_type: str, payload: dict[str, Any]) -> AgentEventEnvelope:
        """构造统一事件信封。"""
        return AgentEventEnvelope(event_type=event_type, payload=payload, meta={})
