from __future__ import annotations

import os
from dataclasses import dataclass
from typing import Any

from browser_use.llm.base import BaseChatModel
from browser_use.llm.openai.chat import ChatOpenAI


class _MimoCompletionsProxy:
    """Inject MiMo's provider-specific thinking control into browser-use calls.

    browser-use's generic OpenAI adapter does not expose ``extra_body``. MiMo
    defaults to deep thinking, which consumes the small structured-action
    completion budget and frequently leaves browser-use with truncated JSON.
    Browser actions need deterministic JSON more than a visible reasoning
    trace, so keep thinking disabled for this sidecar-only client.
    """

    def __init__(self, inner: Any) -> None:
        self._inner = inner

    async def create(self, *args: Any, **kwargs: Any) -> Any:
        extra_body = dict(kwargs.pop("extra_body", {}) or {})
        extra_body.setdefault("thinking", {"type": "disabled"})
        kwargs["extra_body"] = extra_body
        return await self._inner.create(*args, **kwargs)


class _MimoChatProxy:
    def __init__(self, inner: Any) -> None:
        self.completions = _MimoCompletionsProxy(inner.completions)


class MimoChatOpenAI(ChatOpenAI):
    """browser-use ChatOpenAI with MiMo-compatible request extras."""

    def __init__(self, **kwargs: Any) -> None:
        # MiMo's OpenAI-compatible endpoint accepts the request but is not
        # consistent with strict JSON-schema response_format. Put the schema
        # in the prompt instead and leave enough completion budget for the
        # action envelope after its short planning text.
        kwargs.setdefault("add_schema_to_system_prompt", True)
        kwargs.setdefault("dont_force_structured_output", True)
        kwargs.setdefault("max_completion_tokens", 16384)
        kwargs.setdefault("temperature", 0.1)
        kwargs.setdefault("frequency_penalty", 0.0)
        super().__init__(**kwargs)

    def get_client(self) -> Any:
        client = super().get_client()
        proxy = type("_MimoClientProxy", (), {})()
        proxy.chat = _MimoChatProxy(client.chat)
        return proxy


@dataclass
class LLMSettings:
    provider: str = "openai"
    base_url: str = ""
    model: str = ""
    api_key_env: str = "OPENAI_API_KEY"
    multimodal_enabled: bool = False


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
        multimodal_enabled=bool(llm.get("multimodal_enabled")),
    )


def create_extraction_llm(settings: LLMSettings) -> BaseChatModel:
    api_key = os.environ.get(settings.api_key_env, "").strip() or None
    provider = settings.provider
    if provider in ("openai", "deepseek", "qwen", "vllm", "glm", "minimax", "mimo"):
        kwargs: dict[str, Any] = {"model": settings.model, "api_key": api_key}
        if settings.base_url:
            kwargs["base_url"] = settings.base_url
        if provider == "mimo":
            return MimoChatOpenAI(**kwargs)
        return ChatOpenAI(**kwargs)
    raise ValueError(f"unsupported llm provider for browser_extract: {provider}")
