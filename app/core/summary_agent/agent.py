"""上下文压缩 Agent 运行时：run_turn 只消费预先格式化的文本块。"""

from __future__ import annotations

import asyncio
import json
from typing import Any

from app.context.models import OpenAIConversationContext
from app.core.main_agent.model import get_model_config, get_openai_client


class SummaryContextCompressionRuntime:
    """上下文压缩运行时（接口形态对齐 `runtime_openai`）。

    职责：
    - 对外暴露与主 runtime 一致的 `run_turn`/`flush_cancelled_turn`；
    - 提供“区间选择与格式化”预处理方法，供外层在 `run_turn` 前执行；
    - `run_turn` 仅接收格式化后的 human_message 文本块并生成摘要，不直接扫描 `ctx.messages`。
    """

    def __init__(self) -> None:
        """初始化压缩运行时依赖。

        逻辑：
        1. 复用主模型客户端与模型配置；
        2. 定义固定摘要输出模板（任务目标/重要结论/修改文件和资源/下一步动作）；
        3. 当前运行时只负责生成摘要文本，不执行工具、不改写上下文。
        """
        self._client = get_openai_client()
        self._model_cfg = get_model_config()
        self._summary_system_prompt = (
            "你是会话压缩助手。你会基于给定消息块生成结构化摘要，"
            "必须严格包含以下四段并使用中文：\n"
            "1) 任务目标\n"
            "2) 重要结论\n"
            "3) 修改过的文件和资源\n"
            "4) 下一步动作\n"
            "要求：\n"
            "- 不要编造不存在的信息；\n"
            "- 文件/资源尽量用路径或明确名称；\n"
            "- 每段内容简洁但可执行。\n"
            "示例：\n"
            "输入消息块（节选）：\n"
            "[1] role=user content=请修复登录超时问题并补测试\n"
            "[2] role=assistant content=我会先定位超时发生位置\n"
            "[3] role=tool content=定位到 auth/session.py 的 token 校验时间窗口\n"
            "输出：\n"
            "任务目标：修复登录超时问题并补充测试。\n"
            "重要结论：超时由 auth/session.py 中 token 校验时间窗口过短导致。\n"
            "修改过的文件和资源：auth/session.py；tests/test_auth_session.py。\n"
            "下一步动作：调整时间窗口配置，补充回归测试并验证登录链路。"
        )
    def flush_cancelled_turn(self, ctx: OpenAIConversationContext) -> None:
        """取消时清理本轮临时状态。

        逻辑：
        1. 当前 summary runtime 为无状态实现；
        2. 取消时无需改写 `ctx`，直接返回。
        """
        del ctx

    async def run_turn(
        self,
        ctx: OpenAIConversationContext,
        *,
        request_type: str,
        content: str | None = None,
    ) -> str | None:
        """执行单轮摘要：仅消费外部准备好的文本块并返回摘要文本。

        逻辑：
        1. 校验输入；
        2. 将 `content` 视为“已格式化完成的待压缩文本块”；
        3. 调模型生成结构化摘要；
        4. 返回摘要文本，失败返回 `None`。

        关键边界：
        - 若输入为空或请求类型非法，抛 `ValueError`；
        - 若被取消，向上抛 `CancelledError`，由上层统一处理。
        """
        del ctx
        if not content or not content.strip():
            raise ValueError("summary runtime 请求缺少 content。")

        if request_type not in {"human_message", "tool_message"}:
            raise ValueError(f"summary runtime 不支持的 request_type: {request_type}")

        try:
            return await self._summarize_block(str(content))
        except asyncio.CancelledError:
            raise

    def build_compression_plan(self, messages: list[dict[str, Any]]) -> dict[str, Any]:
        """在 `run_turn` 之前执行区间选择与文本块格式化。

        逻辑：
        1. 按规则选择可压缩区间；
        2. 过滤 `system` 后序列化为单条 human_message 文本块；
        3. 返回 `start/end/block` 供上层执行替换。

        Returns:
            `{"ok","reason","start","end","block","source_message_count","compressed_message_count"}`。
        """
        selected = self._select_compress_range(messages)
        if selected is None:
            return {
                "ok": False,
                "reason": "no_valid_compression_range",
                "start": -1,
                "end": -1,
                "block": "",
                "source_message_count": len(messages),
                "compressed_message_count": 0,
            }
        start, end, picked = selected
        block = self._build_human_message_block(picked)
        return {
            "ok": True,
            "reason": "ok",
            "start": start,
            "end": end,
            "block": block,
            "source_message_count": len(messages),
            "compressed_message_count": len(picked),
        }

    def should_compress(
        self,
        messages: list[dict[str, Any]],
        *,
        silent_trigger_tokens: int,
        blocking_trigger_tokens: int,
    ) -> dict[str, Any]:
        """判断是否需要压缩，并识别触发的是静默阈值还是阻塞阈值。

        逻辑：
        1. 统一估算当前 token，并按“阻塞优先于静默”判定触发层级；
        2. 未触发任何阈值时返回 `none`；
        3. 触发阈值后尝试选区间，失败则返回 `should_compress=False`。

        Returns:
            `{"should_compress","trigger_level","total_tokens"}`，其中 `trigger_level` 取值
            `none/silent/blocking`。
        """
        silent_threshold = max(0, int(silent_trigger_tokens))
        blocking_threshold = max(0, int(blocking_trigger_tokens))
        total_tokens = self._estimate_message_tokens(messages)

        # 触发层级判定遵循“阻塞优先”：同时命中时按 blocking 处理。
        if blocking_threshold > 0 and total_tokens >= blocking_threshold:
            trigger_level = "blocking"
        elif silent_threshold > 0 and total_tokens >= silent_threshold:
            trigger_level = "silent"
        else:
            trigger_level = "none"

        if trigger_level == "none":
            return {
                "should_compress": False,
                "trigger_level": "none",
                "total_tokens": total_tokens,
            }

        selected = self._select_compress_range(messages)
        if selected is None:
            return {
                "should_compress": False,
                "trigger_level": trigger_level,
                "total_tokens": total_tokens,
            }
        return {
            "should_compress": True,
            "trigger_level": trigger_level,
            "total_tokens": total_tokens,
        }

    def estimate_message_tokens(self, messages: list[dict[str, Any]]) -> int:
        """对消息列表做粗略 token 估算（供外层阈值判定）。"""
        return self._estimate_message_tokens(messages)

    @staticmethod
    def _select_compress_range(messages: list[dict[str, Any]]) -> tuple[int, int, list[dict[str, Any]]] | None:
        """按规则选择压缩区间（返回 start/end 与区间消息）。"""
        if not messages:
            return None
        last_assistant_idx = -1
        for idx, msg in enumerate(messages):
            if not isinstance(msg, dict):
                continue
            if str(msg.get("role") or "") == "assistant":
                last_assistant_idx = idx
        if last_assistant_idx <= 0:
            return None

        candidates: list[tuple[int, dict[str, Any]]] = []
        for idx, msg in enumerate(messages[:last_assistant_idx]):
            if not isinstance(msg, dict):
                continue
            role = str(msg.get("role") or "")
            if role == "system":
                continue
            candidates.append((idx, msg))

        # 从尾部回退，直到不存在“未闭合”的 assistant(tool_calls)。
        while candidates and not SummaryContextCompressionRuntime._assistant_tool_pairs_complete(
            [m for _, m in candidates]
        ):
            candidates.pop()
        if not candidates:
            return None
        start = int(candidates[0][0])
        end = int(candidates[-1][0])
        selected = [m for _, m in candidates]
        return start, end, selected

    @staticmethod
    def _estimate_message_tokens(messages: list[dict[str, Any]]) -> int:
        """粗略估算消息 token 数（基于 JSON 文本长度）。"""
        total = 0
        for msg in messages:
            if not isinstance(msg, dict):
                continue
            total += max(1, len(json.dumps(msg, ensure_ascii=False)) // 4)
        return total

    @staticmethod
    def _assistant_tool_pairs_complete(messages: list[dict[str, Any]]) -> bool:
        """判断 assistant-tool 配对是否完整（按 call_id 计数）。"""
        pending: dict[str, int] = {}
        for msg in messages:
            if not isinstance(msg, dict):
                continue
            role = str(msg.get("role") or "")
            if role == "assistant":
                raw_calls = msg.get("tool_calls") or []
                if isinstance(raw_calls, list):
                    for idx, call in enumerate(raw_calls):
                        if not isinstance(call, dict):
                            continue
                        call_id = str(call.get("id") or f"tool-call-{idx}")
                        pending[call_id] = pending.get(call_id, 0) + 1
            elif role == "tool":
                call_id = str(msg.get("tool_call_id") or "").strip()
                if call_id and pending.get(call_id, 0) > 0:
                    left = pending[call_id] - 1
                    if left <= 0:
                        pending.pop(call_id, None)
                    else:
                        pending[call_id] = left
            else:
                continue
        return not pending

    @staticmethod
    def _build_human_message_block(messages: list[dict[str, Any]]) -> str:
        """将压缩区间序列化为单条 human_message 文本块。"""
        lines = [
            "以下是需要压缩的历史消息块（human_message）：",
            "",
        ]
        for idx, msg in enumerate(messages, start=1):
            role = str(msg.get("role") or "unknown")
            content = str(msg.get("content") or "")
            lines.append(f"[{idx}] role={role}")
            if role == "assistant":
                tool_calls = msg.get("tool_calls") or []
                if isinstance(tool_calls, list) and tool_calls:
                    lines.append("tool_calls=" + json.dumps(tool_calls, ensure_ascii=False))
            if role == "tool":
                lines.append(f"tool_call_id={str(msg.get('tool_call_id') or '')}")
            lines.append("content=" + content)
            lines.append("")
        return "\n".join(lines).strip()

    async def _summarize_block(self, human_block: str) -> str | None:
        """调用模型生成结构化摘要文本；失败时返回 `None`。"""
        kwargs: dict[str, Any] = {
            "model": self._model_cfg["model"],
            "messages": [
                {"role": "system", "content": self._summary_system_prompt},
                {"role": "user", "content": human_block},
            ],
            "temperature": self._model_cfg["temperature"],
        }
        extra_body = self._model_cfg.get("extra_body") or {}
        if extra_body:
            kwargs["extra_body"] = extra_body
        resp = await self._client.chat.completions.create(**kwargs)
        choices = getattr(resp, "choices", None) or []
        if not choices:
            return None
        message = getattr(choices[0], "message", None)
        content = getattr(message, "content", "") if message is not None else ""
        text = str(content or "").strip()
        if text:
            return text
        return None

def init_agent() -> SummaryContextCompressionRuntime:
    """创建上下文压缩 runtime 实例。"""
    return SummaryContextCompressionRuntime()
