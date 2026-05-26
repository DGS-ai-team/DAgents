"""OpenAI messages 写入上下文前的协议规范化工具。"""

from __future__ import annotations

import copy
import logging
from typing import Any

logger = logging.getLogger(__name__)


def normalize_reasoning_content(value: Any) -> str:
    """规范化模型/历史中的 `reasoning_content`。

    逻辑：
    1. `None` 视为 provider 未返回，统一转为空串；
    2. 其它值转为字符串，保留空串字段；
    3. 调用方将返回值写入 assistant 历史消息。

    关键边界：
    - 不做 strip，避免破坏 provider 返回的原始思考文本；
    - 空串也代表合法字段，用于 DeepSeek 后续请求回传。
    """
    if value is None:
        return ""
    return str(value)


def latest_assistant_reasoning_content(messages: list[dict[str, Any]]) -> str:
    """从 OpenAI messages 中读取最近一条 assistant 的 `reasoning_content`。

    逻辑：
    1. 从尾部倒序扫描 `messages`；
    2. 找到最近的 assistant 消息后读取 `reasoning_content`；
    3. 若历史中没有该字段则返回空串，让调用方仍写出稳定 key。

    关键边界：
    - 跳过非 dict 消息和非 assistant 消息；
    - 空字符串也会原样返回，因为 DeepSeek 历史回传需要字段存在。

    副作用说明：
    - 只读消息列表，不修改传入对象。
    """
    for message in reversed(messages):
        if not isinstance(message, dict):
            continue
        if str(message.get("role") or "") != "assistant":
            continue
        reasoning_content = message.get("reasoning_content")
        if reasoning_content is not None:
            return normalize_reasoning_content(reasoning_content)
    return ""


def _is_tool_callback_message(message: dict[str, Any]) -> bool:
    """判断 assistant tool_calls 是否为系统合成的异步工具回调。

    逻辑：
    1. 仅检查 `tool_calls` 中的 function name；
    2. 任一调用名为 `tool_callback` 即视为异步回灌合成消息；
    3. 其它结构异常或缺失时返回 False。

    关键边界：
    - 不解析 arguments，避免把规范化入口绑定到具体 payload 结构；
    - 返回 True 只表示可以继承最近 reasoning，不表示工具结果有效。
    """
    raw_calls = message.get("tool_calls")
    if not isinstance(raw_calls, list):
        return False
    for call in raw_calls:
        if not isinstance(call, dict):
            continue
        function = call.get("function")
        if not isinstance(function, dict):
            continue
        if str(function.get("name") or "") == "tool_callback":
            return True
    return False


def normalize_openai_message_for_context(
    *,
    existing_messages: list[dict[str, Any]],
    message: dict[str, Any],
) -> dict[str, Any]:
    """规范化即将写入 `OpenAIConversationContext.messages` 的单条消息。

    逻辑：
    1. 深拷贝待写入消息，避免调用方持有对象被原地改写；
    2. 非 assistant 或无 `tool_calls` 的消息直接返回；
    3. `assistant + tool_calls` 统一写入 `reasoning_content` 字段；
    4. 异步工具回调 `tool_callback` 缺字段时继承最近 assistant 的 reasoning。

    关键边界：
    - 普通模型 tool_call 若缺字段，不盲目继承其它轮次 reasoning，仅补为空串并打 warning；
    - DeepSeek 要求 tool_calls 场景后续请求完整回传 reasoning，本函数只保证写入入口不漏字段。

    副作用说明：
    - 不修改 `existing_messages` 与原始 `message`；返回可安全写入的新 dict。
    """
    normalized = copy.deepcopy(message)
    if str(normalized.get("role") or "") != "assistant":
        return normalized
    if not normalized.get("tool_calls"):
        return normalized
    if "reasoning_content" in normalized:
        normalized["reasoning_content"] = normalize_reasoning_content(normalized.get("reasoning_content"))
        return normalized
    if _is_tool_callback_message(normalized):
        # 异步工具结果的 callback assistant 不是模型新输出，属于上一条工具链的延续。
        normalized["reasoning_content"] = latest_assistant_reasoning_content(existing_messages)
    else:
        logger.warning("assistant tool_calls message missing reasoning_content; writing empty fallback")
        normalized["reasoning_content"] = ""
    return normalized
