"""Manage 侧 LLM chat 客户端（OpenAI-compatible + mock）。"""

from __future__ import annotations

import json
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from typing import Any, Protocol

from manage.llm.models import LLMResolved
from manage.llm.store import LLMConfigStore


@dataclass
class ChatToolCall:
    id: str
    name: str
    arguments: str


@dataclass
class ChatResult:
    content: str = ""
    tool_calls: list[ChatToolCall] = field(default_factory=list)
    finish_reason: str = "stop"
    raw: dict[str, Any] = field(default_factory=dict)


class LLMChatClient(Protocol):
    def chat(
        self,
        messages: list[dict[str, Any]],
        *,
        tools: list[dict[str, Any]] | None = None,
        tool_choice: str | dict[str, Any] | None = None,
    ) -> ChatResult: ...


class MockLLMClient:
    """可脚本化的 mock：按调用序号返回预设回复，或回显最后一条 user。"""

    def __init__(self, script: list[ChatResult] | None = None) -> None:
        self._script = list(script or [])
        self._i = 0
        self.calls: list[dict[str, Any]] = []

    def chat(
        self,
        messages: list[dict[str, Any]],
        *,
        tools: list[dict[str, Any]] | None = None,
        tool_choice: str | dict[str, Any] | None = None,
    ) -> ChatResult:
        self.calls.append({"messages": messages, "tools": tools, "tool_choice": tool_choice})
        if self._i < len(self._script):
            result = self._script[self._i]
            self._i += 1
            return result
        # 默认：回显最后一条非 tool 用户/投影文本
        text = ""
        for m in reversed(messages):
            if m.get("role") in {"user", "assistant"} and m.get("content"):
                text = str(m["content"])
                break
        return ChatResult(content=text or "(empty)", finish_reason="stop")


class OpenAICompatibleChatClient:
    def __init__(self, resolved: LLMResolved, *, timeout_seconds: float = 60.0) -> None:
        self._resolved = resolved
        self._timeout = timeout_seconds

    def chat(
        self,
        messages: list[dict[str, Any]],
        *,
        tools: list[dict[str, Any]] | None = None,
        tool_choice: str | dict[str, Any] | None = None,
    ) -> ChatResult:
        base = self._resolved.baseURL.rstrip("/")
        url = f"{base}/chat/completions"
        body: dict[str, Any] = {
            "model": self._resolved.model,
            "messages": messages,
        }
        if tools:
            body["tools"] = tools
            if tool_choice is not None:
                body["tool_choice"] = tool_choice
        data = json.dumps(body).encode("utf-8")
        req = urllib.request.Request(
            url,
            data=data,
            method="POST",
            headers={
                "Content-Type": "application/json",
                "Authorization": f"Bearer {self._resolved.apiKey}",
            },
        )
        try:
            with urllib.request.urlopen(req, timeout=self._timeout) as resp:
                raw = json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as exc:
            detail = exc.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"llm http {exc.code}: {detail}") from exc
        choice = (raw.get("choices") or [{}])[0]
        message = choice.get("message") or {}
        tool_calls: list[ChatToolCall] = []
        for tc in message.get("tool_calls") or []:
            fn = tc.get("function") or {}
            tool_calls.append(
                ChatToolCall(
                    id=str(tc.get("id") or ""),
                    name=str(fn.get("name") or ""),
                    arguments=str(fn.get("arguments") or "{}"),
                )
            )
        return ChatResult(
            content=str(message.get("content") or ""),
            tool_calls=tool_calls,
            finish_reason=str(choice.get("finish_reason") or "stop"),
            raw=raw if isinstance(raw, dict) else {},
        )


def resolve_chat_client(
    llm_store: LLMConfigStore | None,
    *,
    profile_id: str,
    mock: bool = False,
    mock_script: list[ChatResult] | None = None,
) -> LLMChatClient:
    if mock:
        return MockLLMClient(mock_script)
    if llm_store is None:
        return MockLLMClient(mock_script)
    cfg = llm_store.get(profile_id) or llm_store.get_default()
    if cfg is None:
        return MockLLMClient(mock_script)
    if cfg.provider == "mock" or (cfg.model or "").lower() == "mock":
        return MockLLMClient(mock_script)
    return OpenAICompatibleChatClient(llm_store.resolve(cfg))
