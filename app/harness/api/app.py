"""FastAPI 网关：所有客户端通过 HTTP 调用 AgentService。"""

from __future__ import annotations

import asyncio
import hmac
import json
import logging
from contextlib import asynccontextmanager, suppress
from typing import Any, Literal

import httpx
from fastapi import FastAPI, HTTPException, Request
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import Response, StreamingResponse
from pydantic import BaseModel, Field, model_validator

from app.config.runtime_layout import triggers_store_path
from app.config.settings import get_settings
from app.harness.queue.message_queue import MessageEnvelope, MessagePriority
from app.harness.service.agent_service import AgentService
from app.harness.streaming.events import InMemoryEventBus, StreamEvent
from app.harness.triggers.models import (
    TriggerCreateIn,
    TriggerDefinition,
    TriggerFireIn,
    TriggerFireRecord,
    TriggerHistoryResult,
    TriggerListResult,
    TriggerUpdateIn,
)
from app.harness.triggers.runtime import get_trigger_store, set_trigger_runtime
from app.harness.triggers.scheduler import TriggerScheduler
from app.harness.triggers.store import JsonTriggerStore
from app.observability.metrics import metrics_text
from app.schemas.agent_peer import parse_agent_peer_envelope_from_text

_A2A_TOKEN_HEADER = "x-dagents-a2a-token"
_logger = logging.getLogger(__name__)


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


class SessionReleaseResult(BaseModel):
    session_id: str
    released: bool


class SessionActiveInfo(BaseModel):
    session_id: str
    client_id: str | None = None
    queue_pending: int = 0
    has_active_turn: bool = False
    run_turn_phase: str = "idle"
    last_activity_at: float | None = None


class SessionPersistedInfo(BaseModel):
    session_id: str
    first_request_message: str = ""
    updated_at: str = ""
    in_queue: bool = False


class SessionListResult(BaseModel):
    active: list[SessionActiveInfo]
    persisted: list[SessionPersistedInfo]


class SessionPersistedDeleteResult(BaseModel):
    session_id: str
    deleted: bool


class CancelTurnResult(BaseModel):
    """取消当前推理 turn 的响应（无在途任务时 `cancelled=false`）。"""

    session_id: str
    cancelled: bool


def _a2a_auth_headers() -> dict[str, str]:
    token = (get_settings().agent_peer_shared_token or "").strip()
    if not token:
        return {}
    return {_A2A_TOKEN_HEADER: token}


def _requires_a2a_token(source: str) -> bool:
    normalized = (source or "").strip().lower()
    return normalized.startswith("agent-peer") or normalized.startswith("register_center") or normalized.startswith("a2a:")


def _verify_a2a_token_value(request: Request) -> None:
    expected = (get_settings().agent_peer_shared_token or "").strip()
    if not expected:
        return
    actual = (request.headers.get(_A2A_TOKEN_HEADER) or "").strip()
    if not actual or not hmac.compare_digest(actual, expected):
        raise HTTPException(status_code=401, detail="invalid A2A token")


def _verify_a2a_token(request: Request, source: str) -> None:
    if not _requires_a2a_token(source):
        return
    _verify_a2a_token_value(request)


def _requires_a2a_stream_token(client_id: str) -> bool:
    normalized = (client_id or "").strip().lower()
    return normalized.startswith("peer-") or normalized.startswith("approve-") or normalized.startswith("broadcast-")


def _normalize_inbound_peer_message(content: str, source: str) -> tuple[str, str, dict[str, Any] | None]:
    """识别入站 AgentPeerEnvelope 并转为普通消息正文。

    逻辑：
    1. 尝试将 `content` 解析为 `AgentPeerEnvelope`；
    2. 非信封时原样返回；
    3. 信封命中时提取 payload content，并把 source 标记为 `a2a:<caller_agent_id>`。

    关键边界：
    - 仅处理文本入站，不改变 resume；
    - payload 为 JSON 对象时稳定序列化后交给 Agent。
    """
    envelope = parse_agent_peer_envelope_from_text(content)
    if envelope is None:
        return content, source, None
    payload_content = envelope.payload.content
    if isinstance(payload_content, dict):
        final_content = json.dumps(payload_content, ensure_ascii=False)
    else:
        final_content = str(payload_content)
    caller_id = envelope.caller.agent_id.strip() or "unknown"
    return final_content, f"a2a:{caller_id}", envelope.model_dump()


async def _register_self_to_registry() -> tuple[bool, str, str]:
    """在 Agent 启动阶段向 Register Center 登记当前实例。

    逻辑：
    1. 读取配置中的 `registry_url/agent_public_base_url/discovery_groups/agent_id`；
    2. 配置缺失时跳过登记并返回原因；
    3. 发送 `POST /v1/agents`，成功后返回登记状态与目标 URL。

    关键分支/边界：
    - 任一关键配置为空时不发请求，避免无效外呼；
    - discovery_groups 为空时跳过，防止写入不可发现记录；
    - 网络异常不会中断 API 启动，返回失败原因供日志排查。

    与外部交互：
    - 对 Register Center 发起 HTTP POST 请求。

    异常说明：
    - 内部吞掉网络异常并转为 `(False, reason, url)`。

    副作用说明：
    - 成功时会在 Register Center 新增或覆盖当前 agent 记录。
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
        "ttl_seconds": max(5, min(3600, int(s.agent_registry_ttl_seconds))),
    }
    try:
        async with httpx.AsyncClient(timeout=10.0) as client:
            resp = await client.post(f"{registry_url}/v1/agents", json=payload, headers=_a2a_auth_headers())
            resp.raise_for_status()
        return True, "ok", registry_url
    except Exception as exc:
        return False, f"register failed: {exc}", registry_url


def _registry_heartbeat_interval_seconds(ttl_seconds: int) -> float:
    ttl = max(5, min(3600, int(ttl_seconds)))
    return max(1.0, float(ttl) / 2.0)


async def _unregister_self_from_registry(registry_url: str) -> tuple[bool, str]:
    """在 Agent 关闭阶段从 Register Center 注销当前实例。

    逻辑：
    1. 校验 `registry_url` 与 `agent_id`；
    2. 请求 `DELETE /v1/agents/{agent_id}`；
    3. 返回注销结果与可读原因，供调用方记录日志。

    关键分支/边界：
    - registry_url 或 agent_id 为空时直接跳过；
    - 404 视为幂等成功（记录可能已被清理）；
    - 网络异常不阻断主进程退出。

    与外部交互：
    - 对 Register Center 发起 HTTP DELETE 请求。

    异常说明：
    - 内部吞掉网络异常并转为 `(False, reason)`。

    副作用说明：
    - 成功时会移除 Register Center 中当前 agent 的目录记录。
    """
    s = get_settings()
    agent_id = (s.agent_id or "").strip()
    final_registry_url = (registry_url or "").strip().rstrip("/")
    if not final_registry_url or not agent_id:
        return False, "missing registry_url/agent_id"
    try:
        async with httpx.AsyncClient(timeout=10.0) as client:
            resp = await client.delete(f"{final_registry_url}/v1/agents/{agent_id}", headers=_a2a_auth_headers())
        if resp.status_code in (200, 204, 404):
            return True, "ok"
        return False, f"unregister status={resp.status_code}"
    except Exception as exc:
        return False, f"unregister failed: {exc}"


async def _registry_heartbeat_loop(stop_event: asyncio.Event) -> None:
    while not stop_event.is_set():
        interval = _registry_heartbeat_interval_seconds(get_settings().agent_registry_ttl_seconds)
        try:
            await asyncio.wait_for(stop_event.wait(), timeout=interval)
            return
        except TimeoutError:
            registered, reason, registry_url = await _register_self_to_registry()
            if not registered and registry_url:
                _logger.warning("agent registry heartbeat failed: %s", reason)


@asynccontextmanager
async def lifespan(app: FastAPI):
    s = get_settings(reload=True)
    bus = InMemoryEventBus()

    async def handle_stream_event(env: MessageEnvelope, event_type: str, data: dict):
        # 无 client_id 时无法关联 SSE 订阅方；与旧版在 AgentService 内短路的行为一致。
        cid = (env.client_id or "").strip()
        if not cid:
            return
        bus.publish(client_id=cid, session_id=env.session_id, event_type=event_type, data=data)

    service = AgentService(max_queue_size=s.max_queue_size, handle_stream_event=handle_stream_event)
    await service.start()
    trigger_store = get_trigger_store(triggers_store_path())
    trigger_scheduler: TriggerScheduler | None = None
    if bool(getattr(s, "triggers_enabled", True)):
        trigger_scheduler = TriggerScheduler(
            store=trigger_store,
            service=service,
            poll_seconds=int(getattr(s, "trigger_scheduler_poll_seconds", 5)),
        )
        trigger_scheduler.start()
    set_trigger_runtime(store=trigger_store, scheduler=trigger_scheduler)
    # message_queue 已随 service.start 初始化完成，此后再进行自登记，避免目录可见但服务未就绪。
    registered, register_reason, registry_url = await _register_self_to_registry()
    heartbeat_stop_event: asyncio.Event | None = None
    heartbeat_task: asyncio.Task[None] | None = None
    if registered:
        heartbeat_stop_event = asyncio.Event()
        heartbeat_task = asyncio.create_task(_registry_heartbeat_loop(heartbeat_stop_event))
    app.state.service = service
    app.state.bus = bus
    app.state.trigger_store = trigger_store
    app.state.trigger_scheduler = trigger_scheduler
    app.state.registry_registered = registered
    app.state.registry_url = registry_url
    app.state.registry_heartbeat_task = heartbeat_task
    if not registered and registry_url:
        _logger.warning("agent registry skipped: %s", register_reason)
    try:
        yield
    finally:
        if trigger_scheduler is not None:
            await trigger_scheduler.stop()
        set_trigger_runtime(store=trigger_store, scheduler=None)
        if heartbeat_stop_event is not None:
            heartbeat_stop_event.set()
        if heartbeat_task is not None:
            heartbeat_task.cancel()
            with suppress(asyncio.CancelledError):
                await heartbeat_task
        if bool(getattr(app.state, "registry_registered", False)):
            unregistered, unregister_reason = await _unregister_self_from_registry(
                str(getattr(app.state, "registry_url", ""))
            )
            if not unregistered:
                _logger.warning("agent unregister failed: %s", unregister_reason)
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

    @app.get("/v1/sessions", response_model=SessionListResult)
    async def list_sessions() -> SessionListResult:
        """列出活跃队列 session 与 sqlite 已持久化 session。"""
        service: AgentService = app.state.service
        data = await service.list_sessions()
        return SessionListResult(
            active=[SessionActiveInfo.model_validate(item) for item in data.get("active", [])],
            persisted=[SessionPersistedInfo.model_validate(item) for item in data.get("persisted", [])],
        )

    @app.post("/v1/sessions/{session_id}/cancel", response_model=CancelTurnResult)
    async def cancel_current_turn(session_id: str) -> CancelTurnResult:
        """取消指定 session 当前正在执行的 `_handle_message`（流式输出会中断，上下文由 runtime `flush_cancelled_turn` 修补）。"""
        service: AgentService = app.state.service
        sid = session_id.strip()
        if not sid:
            raise HTTPException(status_code=422, detail="session_id is empty")
        ok = service.cancel_current_turn(sid)
        return CancelTurnResult(session_id=sid, cancelled=ok)

    @app.delete("/v1/sessions/{session_id}", response_model=SessionReleaseResult)
    async def release_session(session_id: str) -> SessionReleaseResult:
        """释放指定 session 的服务端资源，并删除该会话持久化记录。"""
        service: AgentService = app.state.service
        sid = session_id.strip()
        if not sid:
            raise HTTPException(status_code=422, detail="session_id is empty")
        try:
            released = await service.release_session(sid, clear_persisted=True)
        except Exception as exc:
            raise HTTPException(status_code=400, detail=str(exc)) from exc
        return SessionReleaseResult(session_id=sid, released=released)

    @app.delete("/v1/sessions/{session_id}/persisted", response_model=SessionPersistedDeleteResult)
    async def delete_persisted_session(session_id: str) -> SessionPersistedDeleteResult:
        """仅删除 sqlite 中的 session；会话仍在队列时返回 409。"""
        service: AgentService = app.state.service
        sid = session_id.strip()
        if not sid:
            raise HTTPException(status_code=422, detail="session_id is empty")
        try:
            deleted = await service.delete_persisted_session(sid)
        except RuntimeError as exc:
            raise HTTPException(status_code=409, detail=str(exc)) from exc
        except ValueError as exc:
            raise HTTPException(status_code=422, detail=str(exc)) from exc
        return SessionPersistedDeleteResult(session_id=sid, deleted=deleted)

    @app.post("/v1/messages", response_model=SubmitResult)
    async def submit_message(body: MessageIn, request: Request) -> SubmitResult:
        service: AgentService = app.state.service
        try:
            _verify_a2a_token(request, body.source)
            if body.request_type == "resume":
                await service.submit_resume(
                    session_id=body.session_id,
                    client_id=body.client_id,
                    resume_value=body.resume_value,
                    source=body.source,
                    priority=body.priority,
                )
            else:
                if not body.content or not body.content.strip():
                    raise HTTPException(status_code=422, detail="content is required for request_type=message")
                final_content, final_source, peer_envelope = _normalize_inbound_peer_message(body.content, body.source)
                await service.submit_message(
                    session_id=body.session_id,
                    client_id=body.client_id,
                    content=final_content,
                    source=final_source,
                    priority=body.priority,
                    peer_envelope=peer_envelope,
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

    @app.post("/v1/triggers", response_model=TriggerDefinition)
    async def create_trigger(body: TriggerCreateIn) -> TriggerDefinition:
        store: JsonTriggerStore = app.state.trigger_store
        try:
            return store.create_trigger(body.to_definition())
        except ValueError as exc:
            raise HTTPException(status_code=422, detail=str(exc)) from exc

    @app.get("/v1/triggers", response_model=TriggerListResult)
    async def list_triggers() -> TriggerListResult:
        store: JsonTriggerStore = app.state.trigger_store
        return TriggerListResult(triggers=store.list_triggers())

    @app.get("/v1/triggers/{trigger_id}", response_model=TriggerDefinition)
    async def get_trigger(trigger_id: str) -> TriggerDefinition:
        store: JsonTriggerStore = app.state.trigger_store
        trigger = store.get_trigger(trigger_id)
        if trigger is None:
            raise HTTPException(status_code=404, detail="trigger not found")
        return trigger

    @app.patch("/v1/triggers/{trigger_id}", response_model=TriggerDefinition)
    async def update_trigger(trigger_id: str, body: TriggerUpdateIn) -> TriggerDefinition:
        store: JsonTriggerStore = app.state.trigger_store
        try:
            return store.update_trigger(trigger_id, body)
        except KeyError as exc:
            raise HTTPException(status_code=404, detail="trigger not found") from exc
        except ValueError as exc:
            raise HTTPException(status_code=422, detail=str(exc)) from exc

    @app.delete("/v1/triggers/{trigger_id}")
    async def delete_trigger(trigger_id: str) -> dict[str, object]:
        store: JsonTriggerStore = app.state.trigger_store
        return {"trigger_id": trigger_id, "deleted": store.delete_trigger(trigger_id)}

    @app.post("/v1/triggers/{trigger_id}/fire", response_model=TriggerFireRecord)
    async def fire_trigger(trigger_id: str, body: TriggerFireIn) -> TriggerFireRecord:
        scheduler: TriggerScheduler | None = app.state.trigger_scheduler
        if scheduler is None:
            raise HTTPException(status_code=503, detail="trigger scheduler is disabled")
        try:
            return await scheduler.fire_trigger(
                trigger_id,
                reason=body.reason,
                payload=body.payload,
                force=body.force,
            )
        except KeyError as exc:
            raise HTTPException(status_code=404, detail="trigger not found") from exc

    @app.get("/v1/triggers/{trigger_id}/history", response_model=TriggerHistoryResult)
    async def list_trigger_history(trigger_id: str) -> TriggerHistoryResult:
        store: JsonTriggerStore = app.state.trigger_store
        if store.get_trigger(trigger_id) is None:
            raise HTTPException(status_code=404, detail="trigger not found")
        return TriggerHistoryResult(records=store.list_history(trigger_id))

    @app.get("/v1/streams")
    async def stream_all(client_id: str, request: Request):
        """订阅全局 SSE 流（跨 session/request 的实时事件）。

        逻辑：
        1. 连接建立后持续读取事件总线 `subscribe_all()`；
        2. 每条事件均透传为标准 SSE 包；
        3. 连接断开由客户端主动关闭或网络中断触发。

        关键边界：
        - 该接口仅推送连接建立后的实时事件，不回放历史；
        - 同一前端可复用一个连接并按 `session_id` 在本地分流展示。
        """
        if _requires_a2a_stream_token(client_id):
            _verify_a2a_token_value(request)
        bus: InMemoryEventBus = app.state.bus
        last_seq = _parse_last_event_id(request.headers.get("last-event-id"))

        async def event_iter():
            async for event in bus.subscribe_all(client_id=client_id, last_seq=last_seq):
                yield _to_sse(event)

        return StreamingResponse(event_iter(), media_type="text/event-stream")

    return app


app = create_app()


def _parse_last_event_id(value: str | None) -> int | None:
    if value is None:
        return None
    try:
        return int(value.strip())
    except ValueError:
        return None


def _to_sse(event: StreamEvent) -> str:
    payload = json.dumps(event.to_dict(), ensure_ascii=False)
    return f"id: {event.seq}\nevent: {event.type}\ndata: {payload}\n\n"

