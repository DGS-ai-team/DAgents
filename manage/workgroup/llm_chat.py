"""Manage 侧 LLM chat 客户端（OpenAI-compatible + mock），支持流式。"""

from __future__ import annotations

import json
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from typing import Any, Iterator, Protocol

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


@dataclass
class ChatStreamPiece:
    """流式片段：delta 为增量文本；result 在最后一帧给出完整结果。"""

    delta: str = ""
    result: ChatResult | None = None


class LLMChatClient(Protocol):
    def chat(
        self,
        messages: list[dict[str, Any]],
        *,
        tools: list[dict[str, Any]] | None = None,
        tool_choice: str | dict[str, Any] | None = None,
    ) -> ChatResult: ...

    def stream_chat(
        self,
        messages: list[dict[str, Any]],
        *,
        tools: list[dict[str, Any]] | None = None,
        tool_choice: str | dict[str, Any] | None = None,
    ) -> Iterator[ChatStreamPiece]: ...


def _chunk_text(text: str, size: int = 12) -> Iterator[str]:
    if not text:
        return
    for i in range(0, len(text), size):
        yield text[i : i + size]


def _stream_from_result(result: ChatResult, *, chunk_size: int = 12) -> Iterator[ChatStreamPiece]:
    """将一次性结果拆成 delta（仅文本终态）+ 最终 result。"""
    if result.tool_calls:
        yield ChatStreamPiece(result=result)
        return
    content = result.content or ""
    for part in _chunk_text(content, chunk_size):
        yield ChatStreamPiece(delta=part)
    yield ChatStreamPiece(result=result)


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
        text = ""
        for m in reversed(messages):
            if m.get("role") in {"user", "assistant"} and m.get("content"):
                text = str(m["content"])
                break
        return ChatResult(content=text or "(empty)", finish_reason="stop")

    def stream_chat(
        self,
        messages: list[dict[str, Any]],
        *,
        tools: list[dict[str, Any]] | None = None,
        tool_choice: str | dict[str, Any] | None = None,
    ) -> Iterator[ChatStreamPiece]:
        yield from _stream_from_result(self.chat(messages, tools=tools, tool_choice=tool_choice))


class OpenAICompatibleChatClient:
    def __init__(self, resolved: LLMResolved, *, timeout_seconds: float = 120.0) -> None:
        self._resolved = resolved
        self._timeout = timeout_seconds

    def chat(
        self,
        messages: list[dict[str, Any]],
        *,
        tools: list[dict[str, Any]] | None = None,
        tool_choice: str | dict[str, Any] | None = None,
    ) -> ChatResult:
        raw = self._post_json(
            {
                "model": self._resolved.model,
                "messages": messages,
                **self._tools_body(tools, tool_choice),
            }
        )
        return self._parse_completion(raw)

    def stream_chat(
        self,
        messages: list[dict[str, Any]],
        *,
        tools: list[dict[str, Any]] | None = None,
        tool_choice: str | dict[str, Any] | None = None,
    ) -> Iterator[ChatStreamPiece]:
        body: dict[str, Any] = {
            "model": self._resolved.model,
            "messages": messages,
            "stream": True,
            **self._tools_body(tools, tool_choice),
        }
        content_parts: list[str] = []
        # index -> partial tool call
        tool_acc: dict[int, dict[str, str]] = {}
        finish_reason = "stop"
        raw_last: dict[str, Any] = {}

        for payload in self._iter_sse_json(body):
            raw_last = payload if isinstance(payload, dict) else raw_last
            choice = (payload.get("choices") or [{}])[0]
            finish_reason = str(choice.get("finish_reason") or finish_reason or "stop")
            delta = choice.get("delta") or {}
            piece = delta.get("content")
            if piece:
                text = str(piece)
                content_parts.append(text)
                yield ChatStreamPiece(delta=text)
            for tc in delta.get("tool_calls") or []:
                try:
                    idx = int(tc.get("index", 0))
                except (TypeError, ValueError):
                    idx = 0
                slot = tool_acc.setdefault(idx, {"id": "", "name": "", "arguments": ""})
                if tc.get("id"):
                    slot["id"] = str(tc["id"])
                fn = tc.get("function") or {}
                if fn.get("name"):
                    slot["name"] = str(fn["name"])
                if fn.get("arguments"):
                    slot["arguments"] += str(fn["arguments"])

        tool_calls = [
            ChatToolCall(
                id=slot["id"] or f"call_{idx}",
                name=slot["name"],
                arguments=slot["arguments"] or "{}",
            )
            for idx, slot in sorted(tool_acc.items())
            if slot["name"]
        ]
        yield ChatStreamPiece(
            result=ChatResult(
                content="".join(content_parts),
                tool_calls=tool_calls,
                finish_reason=finish_reason if finish_reason != "None" else ("tool_calls" if tool_calls else "stop"),
                raw=raw_last if isinstance(raw_last, dict) else {},
            )
        )

    def _tools_body(
        self,
        tools: list[dict[str, Any]] | None,
        tool_choice: str | dict[str, Any] | None,
    ) -> dict[str, Any]:
        out: dict[str, Any] = {}
        if tools:
            out["tools"] = tools
            if tool_choice is not None:
                out["tool_choice"] = tool_choice
        return out

    def _post_json(self, body: dict[str, Any]) -> dict[str, Any]:
        data = json.dumps(body).encode("utf-8")
        req = urllib.request.Request(
            self._completions_url(),
            data=data,
            method="POST",
            headers=self._headers(),
        )
        try:
            with urllib.request.urlopen(req, timeout=self._timeout) as resp:
                raw = json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as exc:
            detail = exc.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"llm http {exc.code}: {detail}") from exc
        return raw if isinstance(raw, dict) else {}

    def _iter_sse_json(self, body: dict[str, Any]) -> Iterator[dict[str, Any]]:
        data = json.dumps(body).encode("utf-8")
        req = urllib.request.Request(
            self._completions_url(),
            data=data,
            method="POST",
            headers=self._headers(),
        )
        try:
            resp = urllib.request.urlopen(req, timeout=self._timeout)
        except urllib.error.HTTPError as exc:
            detail = exc.read().decode("utf-8", errors="replace")
            raise RuntimeError(f"llm http {exc.code}: {detail}") from exc
        with resp:
            while True:
                line = resp.readline()
                if not line:
                    break
                text = line.decode("utf-8", errors="replace").strip()
                if not text or text.startswith(":"):
                    continue
                if not text.startswith("data:"):
                    continue
                payload = text[5:].strip()
                if payload == "[DONE]":
                    break
                try:
                    obj = json.loads(payload)
                except json.JSONDecodeError:
                    continue
                if isinstance(obj, dict):
                    yield obj

    def _completions_url(self) -> str:
        return f"{self._resolved.baseURL.rstrip('/')}/chat/completions"

    def _headers(self) -> dict[str, str]:
        return {
            "Content-Type": "application/json",
            "Authorization": f"Bearer {self._resolved.apiKey}",
            "Accept": "text/event-stream, application/json",
        }

    @staticmethod
    def _parse_completion(raw: dict[str, Any]) -> ChatResult:
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


def describe_llm_resolution(
    llm_store: LLMConfigStore | None,
    *,
    profile_id: str,
    mock: bool = False,
) -> dict[str, Any]:
    """供调试 API：说明当前会解析成 mock 还是 live（不发真实请求）。"""
    pid = str(profile_id or "").strip() or "default"
    if mock:
        return {"mode": "mock", "profile_id": pid, "reason": "forced_mock"}
    if llm_store is None:
        return {"mode": "mock", "profile_id": pid, "reason": "no_llm_store"}
    cfg = llm_store.get(pid) or llm_store.get_default()
    if cfg is None:
        return {"mode": "mock", "profile_id": pid, "reason": "profile_not_found"}
    provider = str(cfg.provider or "").strip().lower()
    model = str(cfg.model or "").strip()
    if provider == "mock" or model.lower() == "mock":
        return {
            "mode": "mock",
            "profile_id": cfg.id,
            "provider": provider,
            "model": model,
            "reason": "provider_mock",
        }
    return {
        "mode": "live",
        "profile_id": cfg.id,
        "provider": provider,
        "model": model,
        "reason": "ok",
    }
