from __future__ import annotations

import os
from dataclasses import dataclass
from typing import Any

from browser_use.llm.base import BaseChatModel


@dataclass
class LLMSettings:
    provider: str = "openai"
    base_url: str = ""
    model: str = ""
    api_key_env: str = "OPENAI_API_KEY"


def llm_settings_from_config(raw: dict[str, Any]) -> LLMSettings | None:
    llm = raw.get("llm") or {}
    model = str(llm.get("model") or "").strip()
    if not model:
        return None
    if bool(llm.get("mock")):
        return None
    return LLMSettings(
        provider=str(llm.get("provider") or "openai").strip().lower(),
        base_url=str(llm.get("base_url") or "").strip(),
        model=model,
        api_key_env=str(llm.get("api_key_env") or "OPENAI_API_KEY").strip() or "OPENAI_API_KEY",
    )


def create_extraction_llm(settings: LLMSettings) -> BaseChatModel:
    api_key = os.environ.get(settings.api_key_env, "").strip() or None
    provider = settings.provider
    if provider in ("openai", "deepseek", "qwen", "vllm", "glm", "minimax", "mimo"):
        from browser_use.llm.openai.chat import ChatOpenAI

        kwargs: dict[str, Any] = {"model": settings.model, "api_key": api_key}
        if settings.base_url:
            kwargs["base_url"] = settings.base_url
        return ChatOpenAI(**kwargs)
    raise ValueError(f"unsupported llm provider for browser_extract: {provider}")
