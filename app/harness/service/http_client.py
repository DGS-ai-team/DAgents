"""HTTP 版 Agent Service 客户端（方案二：CLI 通过 API 交互）。"""

from __future__ import annotations

import json
from typing import AsyncIterator

import httpx

from app.harness.service.interface import (
    AgentCancelTurnResult,
    AgentServiceClient,
    AgentSessionCreateResult,
    AgentStreamEventData,
    AgentSubmitRequest,
    AgentSubmitResult,
)


class HttpAgentServiceClient(AgentServiceClient):
    """基于 FastAPI + SSE 的 Agent Service 客户端。

    使用场景：CLI 通过 HTTP 与独立 `agent_service` 进程交互。

    字段说明：
    - `base_url`：服务根地址（如 `http://127.0.0.1:8000`）。
    - `timeout_seconds`：HTTP 超时秒数（默认 30）。

    返回说明：
    - `submit`：返回 `AgentSubmitResult`。
    - `stream`：返回异步事件流，逐条产出 `AgentStreamEventData`。
    - `cancel_current_turn`：`POST /v1/sessions/{session_id}/cancel`。

    调用范例：
    - `client = HttpAgentServiceClient("http://127.0.0.1:8000")`
    - `result = await client.submit(req); async for ev in client.stream(result.request_id): ...`
    """

    def __init__(self, base_url: str, timeout_seconds: int = 30) -> None:
        self._base_url = base_url.rstrip("/")
        self._timeout_seconds = timeout_seconds

    async def create_session(self, session_id: str | None = None) -> AgentSessionCreateResult:
        """创建会话并返回统一 `session_id`。"""
        payload: dict[str, str] = {}
        if session_id:
            payload["session_id"] = session_id
        async with httpx.AsyncClient(timeout=self._timeout_seconds) as client:
            resp = await client.post(f"{self._base_url}/v1/sessions", json=payload)
        if not resp.is_success:
            raise RuntimeError(f"create_session failed: status={resp.status_code} body={resp.text}")
        data = resp.json()
        return AgentSessionCreateResult(
            session_id=str(data.get("session_id", "")),
            created=bool(data.get("created", True)),
        )

    async def submit(self, request: AgentSubmitRequest) -> AgentSubmitResult:
        """提交消息/恢复请求到 `/v1/messages`。"""
        payload = {
            "session_id": request.session_id,
            "request_type": request.request_type,
            "content": request.content,
            "resume_value": request.resume_value,
            "source": request.source,
            "priority": request.priority,
        }
        async with httpx.AsyncClient(timeout=self._timeout_seconds) as client:
            resp = await client.post(f"{self._base_url}/v1/messages", json=payload)
        if not resp.is_success:
            raise RuntimeError(f"submit failed: status={resp.status_code} body={resp.text}")
        data = resp.json()
        return AgentSubmitResult(
            accepted=bool(data.get("accepted", False)),
            request_id=str(data.get("request_id", "")),
            session_id=str(data.get("session_id", request.session_id)),
            priority=str(data.get("priority", request.priority)),  # type: ignore[arg-type]
        )

    async def cancel_current_turn(self, session_id: str) -> AgentCancelTurnResult:
        """请求取消当前 session 在跑的 turn（见 **`AgentService.cancel_current_turn`**）。"""
        sid = session_id.strip()
        async with httpx.AsyncClient(timeout=self._timeout_seconds) as client:
            resp = await client.post(f"{self._base_url}/v1/sessions/{sid}/cancel")
        if not resp.is_success:
            raise RuntimeError(f"cancel_current_turn failed: status={resp.status_code} body={resp.text}")
        data = resp.json()
        return AgentCancelTurnResult(
            session_id=str(data.get("session_id", sid)),
            cancelled=bool(data.get("cancelled", False)),
        )

    async def stream(self, request_id: str) -> AsyncIterator[AgentStreamEventData]:
        """订阅 `/v1/streams/{request_id}` 的 SSE 并解析为统一事件模型。"""
        url = f"{self._base_url}/v1/streams/{request_id}"
        async with httpx.AsyncClient(timeout=None) as client:
            async with client.stream("GET", url) as resp:
                if not resp.is_success:
                    body = await resp.aread()
                    raise RuntimeError(
                        f"stream failed: status={resp.status_code} body={body.decode('utf-8', errors='replace')}"
                    )

                event_name = ""
                data_lines: list[str] = []
                async for line in resp.aiter_lines():
                    if line.startswith("event:"):
                        event_name = line[len("event:") :].strip()
                        continue
                    if line.startswith("data:"):
                        data_lines.append(line[len("data:") :].lstrip())
                        continue
                    if line == "":
                        if not data_lines:
                            event_name = ""
                            continue
                        raw_data = "\n".join(data_lines)
                        payload = json.loads(raw_data)
                        yield AgentStreamEventData(
                            request_id=str(payload.get("request_id", request_id)),
                            session_id=str(payload.get("session_id", "")),
                            type=event_name or str(payload.get("type", "")),
                            seq=int(payload.get("seq", 0)),
                            ts=str(payload.get("ts", "")),
                            data=payload.get("data", {}) if isinstance(payload.get("data", {}), dict) else {},
                        )
                        event_name = ""
                        data_lines = []
