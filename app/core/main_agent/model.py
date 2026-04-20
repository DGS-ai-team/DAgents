"""OpenAI 客户端工厂（与服务编排解耦，便于替换与单测）。"""

from __future__ import annotations

from typing import Any

from openai import AsyncOpenAI

from app.config.settings import get_settings


def get_openai_client() -> AsyncOpenAI:
    """返回异步 OpenAI 客户端。

    逻辑：
    1. 从 `Settings` 读取 `LLM_API_KEY/LLM_API_BASE`；
    2. 构造 `AsyncOpenAI`；
    3. 交由运行时模块统一发起 chat 请求。
    """
    s = get_settings()
    return AsyncOpenAI(
        api_key=s.llm_api_key,
        base_url=s.llm_api_base or None,
        timeout=s.llm_timeout,
    )


def get_model_config() -> dict[str, Any]:
    """返回模型请求所需的轻量配置字典。"""
    s = get_settings()
    extra_body: dict[str, Any] = {}
    if s.llm_enable_thinking:
        extra_body["enable_thinking"] = True
        extra_body["thinking_budget"] = s.llm_thinking_budget
    return {
        "model": s.llm_model,
        "temperature": 0.1,
        "extra_body": extra_body,
    }
