"""HTTP 版 Agent Service 客户端（方案二：CLI 通过 API 交互）。"""

from __future__ import annotations

import json
import uuid
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
    - `result = await client.submit(req); async for ev in client.stream(result.session_id): ...`
    """

    def __init__(self, base_url: str, timeout_seconds: int = 30, client_id: str | None = None) -> None:
        """初始化 HTTP 客户端并固定 `client_id`。

        逻辑：
        1. 规范化 `base_url` 与超时参数；
        2. 若调用方未传 `client_id`，自动生成稳定 UUID；
        3. 后续 `submit/stream` 复用同一 `client_id`，保证 SSE 事件归属一致。

        关键分支/边界：
        - `client_id` 传空白时自动回退随机值，避免请求 422。
        """
        self._base_url = base_url.rstrip("/")
        self._timeout_seconds = timeout_seconds
        provided_client_id = (client_id or "").strip()
        if provided_client_id:
            self._client_id = provided_client_id
        else:
            self._client_id = f"cli-{uuid.uuid4().hex}"

    @property
    def client_id(self) -> str:
        """返回当前客户端固定使用的 `client_id`。"""
        return self._client_id

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
            "client_id": request.client_id or self._client_id,
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

    async def stream(self, session_id: str) -> AsyncIterator[AgentStreamEventData]:
        """按会话读取全局 SSE，`done` 仅作为事件透传。

        逻辑：
        1. 连接 `/v1/streams?client_id=...`；
        2. 解析 `event/data` 帧并按 `session_id` 过滤；
        3. 命中当前会话事件后产出 `AgentStreamEventData`；
        4. `done` 作为普通事件继续向上透传，不主动断开 SSE 连接。

        关键分支/边界：
        - 其它会话事件会被忽略，不污染当前 CLI 输出；
        - SSE 非 2xx 时抛出异常，由调用方统一兜底。
        """
        sid = session_id.strip()
        url = f"{self._base_url}/v1/streams?client_id={self._client_id}"
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
                        event_session_id = str(payload.get("session_id", ""))
                        if event_session_id == sid:
                            event_data = AgentStreamEventData(
                                client_id=str(payload.get("client_id", self._client_id)),
                                session_id=event_session_id,
                                type=event_name or str(payload.get("type", "")),
                                seq=int(payload.get("seq", 0)),
                                ts=str(payload.get("ts", "")),
                                data=payload.get("data", {}) if isinstance(payload.get("data", {}), dict) else {},
                            )
                            yield event_data
                        else:
                            # 全局流中其它会话事件不属于当前调用方，直接忽略。
                            pass
                        event_name = ""
                        data_lines = []
