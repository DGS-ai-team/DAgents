from __future__ import annotations

import codecs
import json
from collections.abc import AsyncIterator, Iterable
from dataclasses import dataclass
from typing import Any
from urllib.parse import urlencode

import aiohttp

from app.cli.log import get_api_client_logger


@dataclass(frozen=True, slots=True)
class StreamEvent:
    event_type: str
    event_id: str | None
    payload: dict[str, Any]

    @property
    def is_stream_ready(self) -> bool:
        return self.event_type == "__stream_ready__"

    @property
    def session_id(self) -> str:
        return str(self.payload.get("session_id") or "")

    @property
    def data(self) -> dict[str, Any]:
        data = self.payload.get("data")
        return data if isinstance(data, dict) else {}


class DAgentsApiClient:
    """Go Agent Node HTTP/SSE 客户端；请求/响应字段与 Node API 一一对应。"""

    def __init__(self, api_base: str, *, timeout_seconds: float = 30.0) -> None:
        self.api_base = api_base.rstrip("/")
        timeout = aiohttp.ClientTimeout(total=None, sock_connect=timeout_seconds)
        self._session = aiohttp.ClientSession(timeout=timeout)

    async def close(self) -> None:
        await self._session.close()

    async def get_health(self) -> dict[str, Any]:
        """GET /health → `{ status, agent_id, version }`。"""
        async with self._session.get(f"{self.api_base}/health") as resp:
            if resp.status != 200:
                body = (await resp.text())[:512]
                raise RuntimeError(f"GET /health: status {resp.status}: {body.strip()}")
            data = await resp.json()
            if not isinstance(data, dict):
                raise RuntimeError("GET /health: invalid JSON")
            return data

    async def health(self) -> bool:
        try:
            payload = await self.get_health()
        except Exception:
            return False
        return str(payload.get("status") or "").strip().lower() == "ok"

    async def get_agent_info(self) -> dict[str, Any]:
        return await self._get_json("/v1/agent/info")

    async def get_agent_update(self) -> dict[str, Any]:
        """GET /v1/agent/update → Release Hub 更新摘要。"""
        return await self._get_json("/v1/agent/update")

    async def get_llm_settings(self) -> dict[str, Any]:
        return await self._get_json("/v1/llm/settings")

    async def patch_llm_settings(self, patch: dict[str, Any]) -> dict[str, Any]:
        return await self._patch_json("/v1/llm/settings", patch)

    async def create_session(self, session_id: str | None = None) -> str:
        payload: dict[str, Any] = {}
        if session_id:
            payload["session_id"] = session_id
        result = await self._post_json("/v1/sessions", payload)
        sid = str(result.get("session_id") or "").strip()
        if not sid:
            raise RuntimeError("node did not return a session_id")
        return sid

    async def submit_message(self, *, session_id: str, content: str) -> None:
        await self._post_json(
            "/v1/messages",
            {
                "session_id": session_id,
                "request_type": "message",
                "content": content,
            },
        )

    async def submit_resume(
        self,
        *,
        session_id: str,
        resume_value: dict[str, Any],
        submit_seq: int | None = None,
    ) -> None:
        """POST resume；`submit_seq` 与 SessionController 日志序号对齐便于对账。"""
        logger = get_api_client_logger()
        logger.info(
            "http submit resume begin session_id=%s seq=%s type=%s approval_id=%s tool_call_id=%s",
            session_id,
            submit_seq,
            resume_value.get("type", ""),
            resume_value.get("approval_id", ""),
            resume_value.get("tool_call_id", ""),
        )
        result = await self._post_json(
            "/v1/messages",
            {
                "session_id": session_id,
                "request_type": "resume",
                "resume_value": resume_value,
            },
        )
        logger.info(
            "http submit resume done session_id=%s seq=%s accepted=%s priority=%s",
            session_id,
            submit_seq,
            result.get("accepted"),
            result.get("priority"),
        )

    async def list_sessions(self) -> dict[str, Any]:
        """GET /v1/sessions → `{ "sessions": [...] }`。"""
        return await self._get_json("/v1/sessions")

    async def delete_session(self, session_id: str) -> dict[str, Any]:
        """DELETE /v1/sessions/{id} → `{ session_id, released }`。"""
        sid = str(session_id or "").strip()
        if not sid:
            raise ValueError("session_id is required")
        return await self._delete_json(f"/v1/sessions/{sid}")

    async def clear_session_context(self, session_id: str) -> dict[str, Any]:
        sid = str(session_id or "").strip()
        if not sid:
            raise ValueError("session_id is required")
        return await self._post_json(f"/v1/sessions/{sid}/clear-context", {})

    async def get_session_context(self, session_id: str) -> dict[str, Any]:
        """查询指定 session 的 context 摘要。

        逻辑：
        1. 规范化并校验 `session_id`；
        2. 调用 `GET /v1/sessions/{session_id}/context`；
        3. 返回 Node JSON dict。
        """
        sid = str(session_id or "").strip()
        if not sid:
            raise ValueError("session_id is required")
        return await self._get_json(f"/v1/sessions/{sid}/context")

    async def compress_session_context(self, session_id: str) -> dict[str, Any]:
        """POST /v1/sessions/{session_id}/compress，手动触发阻塞压缩。"""
        sid = str(session_id or "").strip()
        if not sid:
            raise ValueError("session_id is required")
        return await self._post_json(f"/v1/sessions/{sid}/compress", {})

    async def cancel_current_turn(self, session_id: str) -> dict[str, Any]:
        sid = str(session_id or "").strip()
        if not sid:
            raise ValueError("session_id is required")
        return await self._post_json(f"/v1/sessions/{sid}/cancel", {})

    async def list_session_skills(self, session_id: str) -> dict[str, Any]:
        sid = str(session_id or "").strip()
        if not sid:
            raise ValueError("session_id is required")
        return await self._get_json(f"/v1/sessions/{sid}/skills")

    async def load_session_skill(self, session_id: str, skill_name: str) -> dict[str, Any]:
        sid = str(session_id or "").strip()
        name = str(skill_name or "").strip()
        if not sid or not name:
            raise ValueError("session_id and skill_name are required")
        return await self._post_json(f"/v1/sessions/{sid}/skills/load", {"skill_name": name})

    async def unload_session_skill(self, session_id: str, skill_name: str) -> dict[str, Any]:
        sid = str(session_id or "").strip()
        name = str(skill_name or "").strip()
        if not sid or not name:
            raise ValueError("session_id and skill_name are required")
        return await self._post_json(f"/v1/sessions/{sid}/skills/unload", {"skill_name": name})

    async def list_child_agents(self, parent_session_id: str) -> list[dict[str, Any]]:
        """GET /v1/sessions/{parent}/child-agents → items 列表。"""
        sid = str(parent_session_id or "").strip()
        if not sid:
            raise ValueError("session_id is required")
        result = await self._get_json(f"/v1/sessions/{sid}/child-agents")
        items = result.get("items")
        return items if isinstance(items, list) else []

    async def list_triggers(self) -> dict[str, Any]:
        """GET /v1/triggers → `{ "triggers": [...] }`。"""
        return await self._get_json("/v1/triggers")

    async def get_policy(self, *, shell: str = "") -> dict[str, Any]:
        path = "/v1/policy"
        if str(shell or "").strip():
            path += f"?shell={str(shell).strip()}"
        return await self._get_json(path)

    async def update_tool_policy(self, updates: list[dict[str, str]]) -> None:
        await self._put_json("/v1/policy/tools", {"updates": updates})

    async def update_shell_policy(
        self,
        shell_type: str,
        updates: list[dict[str, str]] | None = None,
        *,
        deletes: list[str] | None = None,
    ) -> None:
        shell = str(shell_type or "").strip().lower()
        body: dict[str, Any] = {"updates": updates or []}
        if deletes:
            body["deletes"] = deletes
        await self._put_json(f"/v1/policy/shell/{shell}", body)

    async def stream_events(self, *, session_id: str, live_only: bool = True) -> AsyncIterator[StreamEvent]:
        """订阅 SSE；默认 live_only 跳过重放（对齐 Node `live=1`）。"""
        sid = str(session_id or "").strip()
        if not sid:
            raise ValueError("session_id is required for SSE")
        params: dict[str, str] = {"session_id": sid}
        if live_only:
            params["live"] = "1"
        query = urlencode(params)
        async with self._session.get(f"{self.api_base}/v1/streams?{query}") as resp:
            if resp.status != 200:
                body = await resp.text()
                raise RuntimeError(_format_http_error(resp.status, body))
            yield StreamEvent(
                event_type="__stream_ready__",
                event_id=None,
                payload={"session_id": sid},
            )
            buffer = ""
            decoder = codecs.getincrementaldecoder("utf-8")()
            async for chunk in resp.content.iter_chunked(1024):
                # 按块 decode 时必须用增量解码器；否则多字节字符（如中文）恰好在块边界会被
                # errors="replace" 变成 U+FFFD（界面上的 �）。
                buffer += decoder.decode(chunk)
                buffer = buffer.replace("\r\n", "\n").replace("\r", "\n")
                while "\n\n" in buffer:
                    block, buffer = buffer.split("\n\n", 1)
                    event = _parse_sse_block(block)
                    if event is not None:
                        yield event
            buffer += decoder.decode(b"", final=True)
            buffer = buffer.replace("\r\n", "\n").replace("\r", "\n")
            while "\n\n" in buffer:
                block, buffer = buffer.split("\n\n", 1)
                event = _parse_sse_block(block)
                if event is not None:
                    yield event

    async def _get_json(self, path: str) -> dict[str, Any]:
        async with self._session.get(f"{self.api_base}{path}") as resp:
            text = await resp.text()
            if resp.status >= 400:
                raise RuntimeError(_format_http_error(resp.status, text))
            if not text.strip():
                return {}
            try:
                data = json.loads(text)
            except json.JSONDecodeError as exc:
                raise RuntimeError(f"invalid JSON response from {path}: {text}") from exc
            return data if isinstance(data, dict) else {}

    async def _post_json(self, path: str, payload: dict[str, Any]) -> dict[str, Any]:
        async with self._session.post(f"{self.api_base}{path}", json=payload) as resp:
            text = await resp.text()
            if resp.status >= 400:
                raise RuntimeError(_format_http_error(resp.status, text))
            if not text.strip():
                return {}
            try:
                data = json.loads(text)
            except json.JSONDecodeError as exc:
                raise RuntimeError(f"invalid JSON response from {path}: {text}") from exc
            return data if isinstance(data, dict) else {}

    async def _patch_json(self, path: str, payload: dict[str, Any]) -> dict[str, Any]:
        async with self._session.patch(f"{self.api_base}{path}", json=payload) as resp:
            text = await resp.text()
            if resp.status >= 400:
                raise RuntimeError(_format_http_error(resp.status, text))
            if not text.strip():
                return {}
            try:
                data = json.loads(text)
            except json.JSONDecodeError as exc:
                raise RuntimeError(f"invalid JSON response from {path}: {text}") from exc
            return data if isinstance(data, dict) else {}

    async def _put_json(self, path: str, payload: dict[str, Any]) -> dict[str, Any]:
        async with self._session.put(f"{self.api_base}{path}", json=payload) as resp:
            text = await resp.text()
            if resp.status >= 400:
                raise RuntimeError(_format_http_error(resp.status, text))
            if not text.strip():
                return {}
            try:
                data = json.loads(text)
            except json.JSONDecodeError as exc:
                raise RuntimeError(f"invalid JSON response from {path}: {text}") from exc
            return data if isinstance(data, dict) else {}

    async def _delete_json(self, path: str) -> dict[str, Any]:
        async with self._session.delete(f"{self.api_base}{path}") as resp:
            text = await resp.text()
            if resp.status >= 400:
                raise RuntimeError(_format_http_error(resp.status, text))
            if not text.strip():
                return {}
            try:
                data = json.loads(text)
            except json.JSONDecodeError as exc:
                raise RuntimeError(f"invalid JSON response from {path}: {text}") from exc
            return data if isinstance(data, dict) else {}


def _decode_utf8_chunks(chunks: Iterable[bytes], *, final_empty: bool = True) -> str:
    """将可能截断 UTF-8 的字节块序列解码为 str（供 SSE 流与单测使用）。

    逻辑：
    1. 用 incremental decoder 缓冲不完整码点，等待后续块；
    2. 全部块喂完后 `final=True` 冲刷尾部；
    3. 非法 UTF-8 向上抛 `UnicodeDecodeError`（HTTP 体应始终为 UTF-8）。

    Args:
        chunks: 顺序字节块（模拟 `iter_chunked`）。
        final_empty: 是否在末尾调用 `decode(b"", final=True)`。
    """
    decoder = codecs.getincrementaldecoder("utf-8")()
    text = "".join(decoder.decode(chunk) for chunk in chunks)
    if final_empty:
        text += decoder.decode(b"", final=True)
    return text


def _format_http_error(status: int, text: str) -> str:
    """解析 Go Node `{error:{code,message}}` 错误体。"""
    try:
        data = json.loads(text)
    except json.JSONDecodeError:
        return f"HTTP {status}: {text}"
    err = data.get("error")
    if isinstance(err, dict):
        message = str(err.get("message") or "").strip()
        code = str(err.get("code") or "").strip()
        if message and code:
            return f"HTTP {status} ({code}): {message}"
        if message:
            return f"HTTP {status}: {message}"
    return f"HTTP {status}: {text}"


def _parse_sse_block(block: str) -> StreamEvent | None:
    event_type = "message"
    event_id: str | None = None
    data_lines: list[str] = []
    for raw_line in block.replace("\r\n", "\n").split("\n"):
        line = raw_line.rstrip("\r")
        if not line or line.startswith(":"):
            continue
        field, _, value = line.partition(":")
        if value.startswith(" "):
            value = value[1:]
        if field == "event":
            event_type = value
        elif field == "id":
            event_id = value
        elif field == "data":
            data_lines.append(value)
    if not data_lines:
        return None
    raw_data = "\n".join(data_lines)
    try:
        payload = json.loads(raw_data)
    except json.JSONDecodeError:
        payload = {"data": raw_data}
    if not isinstance(payload, dict):
        payload = {"data": payload}
    return StreamEvent(event_type=event_type, event_id=event_id, payload=payload)
