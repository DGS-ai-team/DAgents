from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any, AsyncIterator
from urllib.parse import urlencode

import aiohttp


@dataclass(frozen=True, slots=True)
class StreamEvent:
    event_type: str
    event_id: str | None
    payload: dict[str, Any]

    @property
    def session_id(self) -> str:
        return str(self.payload.get("session_id") or "")

    @property
    def data(self) -> dict[str, Any]:
        data = self.payload.get("data")
        return data if isinstance(data, dict) else {}


class DAgentsApiClient:
    def __init__(self, api_base: str, *, timeout_seconds: float = 30.0) -> None:
        self.api_base = api_base.rstrip("/")
        timeout = aiohttp.ClientTimeout(total=None, sock_connect=timeout_seconds)
        self._session = aiohttp.ClientSession(timeout=timeout)

    async def close(self) -> None:
        await self._session.close()

    async def health(self) -> bool:
        async with self._session.get(f"{self.api_base}/health") as resp:
            return resp.status == 200

    async def create_session(self, session_id: str | None = None) -> str:
        payload: dict[str, Any] = {}
        if session_id:
            payload["session_id"] = session_id
        result = await self._post_json("/v1/sessions", payload)
        sid = str(result.get("session_id") or "").strip()
        if not sid:
            raise RuntimeError("backend did not return a session_id")
        return sid

    async def submit_message(self, *, session_id: str, client_id: str, content: str) -> None:
        await self._post_json(
            "/v1/messages",
            {
                "session_id": session_id,
                "client_id": client_id,
                "request_type": "message",
                "content": content,
                "source": "cli",
            },
        )

    async def submit_resume(self, *, session_id: str, client_id: str, resume_value: dict[str, Any]) -> None:
        await self._post_json(
            "/v1/messages",
            {
                "session_id": session_id,
                "client_id": client_id,
                "request_type": "resume",
                "resume_value": resume_value,
                "source": "cli",
            },
        )

    async def list_triggers(self) -> dict[str, Any]:
        """GET /v1/triggers，返回含 triggers 列表的字典。"""
        return await self._get_json("/v1/triggers")

    async def list_sessions(self) -> dict[str, Any]:
        """GET /v1/sessions，返回活跃与持久化 session 列表。"""
        return await self._get_json("/v1/sessions")

    async def delete_persisted_session(self, session_id: str) -> dict[str, Any]:
        """DELETE /v1/sessions/{session_id}/persisted，仅删除 sqlite 行。"""
        sid = str(session_id or "").strip()
        if not sid:
            raise ValueError("session_id is required")
        return await self._delete_json(f"/v1/sessions/{sid}/persisted")

    async def patch_trigger(self, trigger_id: str, payload: dict[str, Any]) -> dict[str, Any]:
        """PATCH /v1/triggers/{trigger_id} 部分更新。"""
        return await self._patch_json(f"/v1/triggers/{trigger_id}", payload)

    async def stream_events(self, *, client_id: str) -> AsyncIterator[StreamEvent]:
        query = urlencode({"client_id": client_id})
        async with self._session.get(f"{self.api_base}/v1/streams?{query}") as resp:
            if resp.status != 200:
                body = await resp.text()
                raise RuntimeError(f"stream failed: HTTP {resp.status}: {body}")
            buffer = ""
            async for chunk in resp.content.iter_chunked(1024):
                buffer += chunk.decode("utf-8", errors="replace")
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
                raise RuntimeError(f"HTTP {resp.status}: {text}")
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
                raise RuntimeError(f"HTTP {resp.status}: {text}")
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
                raise RuntimeError(f"HTTP {resp.status}: {text}")
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
                raise RuntimeError(f"HTTP {resp.status}: {text}")
            if not text.strip():
                return {}
            try:
                data = json.loads(text)
            except json.JSONDecodeError as exc:
                raise RuntimeError(f"invalid JSON response from {path}: {text}") from exc
            return data if isinstance(data, dict) else {}


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
