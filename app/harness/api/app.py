"""FastAPI 网关：所有客户端通过 HTTP 调用 AgentService。"""

from __future__ import annotations

import json
from contextlib import asynccontextmanager
from typing import Any, Literal

import httpx
from fastapi import FastAPI, HTTPException
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import Response, StreamingResponse
from pydantic import BaseModel, Field, model_validator

from app.config.settings import get_settings
from app.harness.queue.message_queue import MessagePriority
from app.harness.service.agent_service import AgentService
from app.harness.streaming.events import InMemoryEventBus, StreamEvent
from app.observability.metrics import metrics_text


class MessageIn(BaseModel):
    session_id: str = Field(min_length=1)
    client_id: str = Field(min_length=1, default="default")
    request_type: Literal["message", "resume"] = "message"
    content: str | None = None
    resume_value: Any | None = None
    source: str = "api"
    # 缺省时：message → human（可打断当前推理）；resume → resume
    priority: MessagePriority | None = None

    @model_validator(mode="after")
    def _fill_default_priority(self) -> MessageIn:
        if self.priority is not None:
            return self
        if self.request_type == "message":
            return self.model_copy(update={"priority": "human"})
        return self.model_copy(update={"priority": "resume"})


class SubmitResult(BaseModel):
    accepted: bool
    session_id: str
    priority: MessagePriority


class SessionCreateIn(BaseModel):
    session_id: str | None = None


class SessionCreateResult(BaseModel):
    session_id: str
    created: bool


class CancelTurnResult(BaseModel):
    """取消当前推理 turn 的响应（无在途任务时 `cancelled=false`）。"""

    session_id: str
    cancelled: bool


async def _register_self_to_registry() -> tuple[bool, str, str]:
    """在 Agent 启动阶段向 register-center 登记当前实例。

    逻辑：
    1. 读取配置中的 `registry_url/agent_public_base_url/discovery_groups/agent_id`；
    2. 配置缺失时跳过登记并返回原因；
    3. 发送 `POST /v1/agents`，成功后返回登记状态与目标 URL。

    关键分支/边界：
    - 任一关键配置为空时不发请求，避免无效外呼；
    - discovery_groups 为空时跳过，防止写入不可发现记录；
    - 网络异常不会中断 API 启动，返回失败原因供日志排查。

    与外部交互：
    - 对 register-center 发起 HTTP POST 请求。

    异常说明：
    - 内部吞掉网络异常并转为 `(False, reason, url)`。

    副作用说明：
    - 成功时会在 register-center 新增或覆盖当前 agent 记录。
    """
    s = get_settings()
    registry_url = (s.registry_url or "").strip().rstrip("/")
    agent_base_url = (s.agent_public_base_url or "").strip().rstrip("/")
    groups = [g.strip() for g in s.discovery_groups if str(g).strip()]
    agent_id = (s.agent_id or "").strip()
    if not registry_url:
        return False, "REGISTRY_URL 未配置，跳过自登记", ""
    if not agent_base_url:
        return False, "AGENT_PUBLIC_BASE_URL 未配置，跳过自登记", registry_url
    if not groups:
        return False, "DISCOVERY_GROUPS 为空，跳过自登记", registry_url
    if not agent_id:
        return False, "AGENT_ID 为空，跳过自登记", registry_url
    payload = {
        "agent_id": agent_id,
        "base_url": agent_base_url,
        "discovery_group": groups,
    }
    try:
        async with httpx.AsyncClient(timeout=10.0) as client:
            resp = await client.post(f"{registry_url}/v1/agents", json=payload)
            resp.raise_for_status()
        return True, "ok", registry_url
    except Exception as exc:
        return False, f"register failed: {exc}", registry_url


async def _unregister_self_from_registry(registry_url: str) -> tuple[bool, str]:
    """在 Agent 关闭阶段从 register-center 注销当前实例。

    逻辑：
    1. 校验 `registry_url` 与 `agent_id`；
    2. 请求 `DELETE /v1/agents/{agent_id}`；
    3. 返回注销结果与可读原因，供调用方记录日志。

    关键分支/边界：
    - registry_url 或 agent_id 为空时直接跳过；
    - 404 视为幂等成功（记录可能已被清理）；
    - 网络异常不阻断主进程退出。

    与外部交互：
    - 对 register-center 发起 HTTP DELETE 请求。

    异常说明：
    - 内部吞掉网络异常并转为 `(False, reason)`。

    副作用说明：
    - 成功时会移除 register-center 中当前 agent 的目录记录。
    """
    s = get_settings()
    agent_id = (s.agent_id or "").strip()
    final_registry_url = (registry_url or "").strip().rstrip("/")
    if not final_registry_url or not agent_id:
        return False, "missing registry_url/agent_id"
    try:
        async with httpx.AsyncClient(timeout=10.0) as client:
            resp = await client.delete(f"{final_registry_url}/v1/agents/{agent_id}")
        if resp.status_code in (200, 204, 404):
            return True, "ok"
        return False, f"unregister status={resp.status_code}"
    except Exception as exc:
        return False, f"unregister failed: {exc}"


@asynccontextmanager
async def lifespan(app: FastAPI):
    s = get_settings(reload=True)
    bus = InMemoryEventBus()

    async def on_stream_event(stream_id: str, event_type: str, data: dict):
        bus.publish(stream_id=stream_id, event_type=event_type, data=data)

    service = AgentService(max_queue_size=s.max_queue_size, on_stream_event=on_stream_event)
    await service.start()
    # message_queue 已随 service.start 初始化完成，此后再进行自登记，避免目录可见但服务未就绪。
    registered, register_reason, registry_url = await _register_self_to_registry()
    app.state.service = service
    app.state.bus = bus
    app.state.registry_registered = registered
    app.state.registry_url = registry_url
    if not registered and registry_url:
        print(f"[WARN] agent registry skipped: {register_reason}")
    try:
        yield
    finally:
        if bool(getattr(app.state, "registry_registered", False)):
            unregistered, unregister_reason = await _unregister_self_from_registry(
                str(getattr(app.state, "registry_url", ""))
            )
            if not unregistered:
                print(f"[WARN] agent unregister failed: {unregister_reason}")
        await service.stop()


def create_app() -> FastAPI:
    """创建并配置 FastAPI 应用实例。

    逻辑：
    1. 读取全局配置并实例化应用；
    2. 按配置安装 CORS 中间件，确保本地前端可完成预检请求；
    3. 注册 health/metrics/session/message/stream 路由。

    关键分支/边界：
    - `api_cors_allow_origins` 为空时回退到本地默认来源；
    - 配置为 `*` 时启用全开放来源（仅建议本地调试）；
    - CORS 仅影响浏览器跨域访问，不影响服务间直连调用。

    与外部交互：
    - 暴露 HTTP API 路由；
    - CORS 中间件会处理浏览器预检请求（OPTIONS）。

    异常说明：
    - 本方法不主动吞异常，框架层异常由 FastAPI/Uvicorn 处理。

    副作用说明：
    - 启动时会向应用实例挂载路由与中间件。
    """
    s = get_settings()
    app = FastAPI(title="DAgents API", version="0.1.0", lifespan=lifespan)
    cors_origins = s.api_cors_allow_origins or ["http://localhost:5173", "http://127.0.0.1:5173"]
    allow_all_origins = "*" in cors_origins
    # 允许前端 dev server 进行跨域预检与正式请求；当前不依赖 cookie 凭证。
    app.add_middleware(
        CORSMiddleware,
        allow_origins=["*"] if allow_all_origins else cors_origins,
        allow_credentials=False,
        allow_methods=["*"],
        allow_headers=["*"],
    )

    @app.get("/health")
    async def health() -> dict[str, str]:
        return {"status": "ok"}

    if s.metrics_enabled:

        @app.get("/metrics")
        async def prometheus_metrics() -> Response:
            body, ctype = metrics_text()
            return Response(content=body, media_type=ctype)

    @app.post("/v1/sessions", response_model=SessionCreateResult)
    async def create_session(body: SessionCreateIn) -> SessionCreateResult:
        service: AgentService = app.state.service
        try:
            final_session_id = await service.create_session(body.session_id)
        except Exception as exc:
            raise HTTPException(status_code=400, detail=str(exc)) from exc
        return SessionCreateResult(session_id=final_session_id, created=True)

    @app.post("/v1/sessions/{session_id}/cancel", response_model=CancelTurnResult)
    async def cancel_current_turn(session_id: str) -> CancelTurnResult:
        """取消指定 session 当前正在执行的 `_handle_message`（流式输出会中断，上下文由 runtime `flush_cancelled_turn` 修补）。"""
        service: AgentService = app.state.service
        sid = session_id.strip()
        if not sid:
            raise HTTPException(status_code=422, detail="session_id is empty")
        ok = service.cancel_current_turn(sid)
        return CancelTurnResult(session_id=sid, cancelled=ok)

    @app.post("/v1/messages", response_model=SubmitResult)
    async def submit_message(body: MessageIn) -> SubmitResult:
        service: AgentService = app.state.service
        bus: InMemoryEventBus = app.state.bus
        stream_id = bus.create_stream(session_id=body.session_id, client_id=body.client_id)
        try:
            if body.request_type == "resume":
                await service.submit_resume(
                    session_id=body.session_id,
                    resume_value=body.resume_value,
                    source=body.source,
                    priority=body.priority,
                    stream_id=stream_id,
                )
            else:
                if not body.content or not body.content.strip():
                    raise HTTPException(status_code=422, detail="content is required for request_type=message")
                await service.submit_message(
                    session_id=body.session_id,
                    content=body.content,
                    source=body.source,
                    priority=body.priority,
                    stream_id=stream_id,
                )
        except HTTPException:
            raise
        except Exception as exc:
            raise HTTPException(status_code=400, detail=str(exc)) from exc
        return SubmitResult(
            accepted=True,
            session_id=body.session_id,
            priority=body.priority,
        )

    @app.get("/v1/streams")
    async def stream_all(client_id: str):
        """订阅全局 SSE 流（跨 session/request 的实时事件）。

        逻辑：
        1. 连接建立后持续读取事件总线 `subscribe_all()`；
        2. 每条事件均透传为标准 SSE 包；
        3. 连接断开由客户端主动关闭或网络中断触发。

        关键边界：
        - 该接口仅推送连接建立后的实时事件，不回放历史；
        - 同一前端可复用一个连接并按 `session_id` 在本地分流展示。
        """
        bus: InMemoryEventBus = app.state.bus

        async def event_iter():
            async for event in bus.subscribe_all(client_id=client_id):
                yield _to_sse(event)

        return StreamingResponse(event_iter(), media_type="text/event-stream")

    return app


app = create_app()


def _to_sse(event: StreamEvent) -> str:
    payload = json.dumps(event.to_dict(), ensure_ascii=False)
    return f"event: {event.type}\ndata: {payload}\n\n"

