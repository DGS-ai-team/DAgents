"""Register Center FastAPI 应用装配。"""

from __future__ import annotations

import asyncio
import hmac
import os
import uuid

import httpx
from fastapi import FastAPI, HTTPException, Query, Request

from rc_models import (
    AgentListResponse,
    AgentRecord,
    AgentUpsertRequest,
    BroadcastRequest,
    BroadcastResponse,
    BroadcastResultItem,
    HealthResponse,
    RelayRequest,
    RelayResponse,
)
from rc_store import AgentRegistryStore

_A2A_TOKEN_HEADER = "x-dagents-a2a-token"


def _shared_token() -> str:
    return os.environ.get("AGENT_PEER_SHARED_TOKEN", "").strip()


def _a2a_auth_headers() -> dict[str, str]:
    token = _shared_token()
    if not token:
        return {}
    return {_A2A_TOKEN_HEADER: token}


def _verify_shared_token(request: Request) -> None:
    expected = _shared_token()
    if not expected:
        return
    actual = (request.headers.get(_A2A_TOKEN_HEADER) or "").strip()
    if not actual or not hmac.compare_digest(actual, expected):
        raise HTTPException(status_code=401, detail="invalid A2A token")


async def _broadcast_to_agent(
    client: httpx.AsyncClient,
    *,
    agent: AgentRecord,
    message: str,
    source: str,
) -> BroadcastResultItem:
    """向单个 Agent 转发广播消息。

    逻辑：
    1. 以目标 `base_url` 拼接 `/v1/messages`；
    2. 生成唯一 `session_id`，避免不同目标间会话冲突；
    3. 发送消息并解析响应，记录 `session_id/client_id` 供调用方追踪流通道。

    关键分支/边界：
    - HTTP 状态码非 2xx 视为失败并记录响应体摘要；
    - 网络异常（超时、连接失败）会被捕获并转为失败结果；
    - 下游响应异常时保留本地生成的 `session_id/client_id`，便于调用方排查。

    与外部交互：
    - 通过 HTTP 调用下游 Agent 的 `/v1/messages` 接口。

    异常说明：
    - 本方法会吞掉请求异常并转换为 `BroadcastResultItem(ok=False)`，
      避免单点失败中断整批广播。

    副作用说明：
    - 可能触发下游 Agent 创建新会话并入队消息。
    """

    session_id = f"broadcast-{agent.agent_id}-{uuid.uuid4().hex[:8]}"
    client_id = f"broadcast-{uuid.uuid4().hex}"
    target_url = f"{agent.base_url}/v1/messages"
    payload = {
        "session_id": session_id,
        "client_id": client_id,
        "request_type": "message",
        "content": message,
        "source": source,
        "priority": "human",
    }
    try:
        resp = await client.post(target_url, json=payload, headers=_a2a_auth_headers())
        detail = None
        try:
            body = resp.json()
        except Exception:
            # 响应不是 JSON 时退化为文本摘要，便于调用方定位下游报错。
            body = None
        if resp.status_code >= 400:
            detail = resp.text[:300]
            return BroadcastResultItem(
                agent_id=agent.agent_id,
                base_url=agent.base_url,
                discovery_group=agent.discovery_group,
                ok=False,
                status_code=resp.status_code,
                session_id=session_id,
                client_id=client_id,
                detail=detail,
            )
        return BroadcastResultItem(
            agent_id=agent.agent_id,
            base_url=agent.base_url,
            discovery_group=agent.discovery_group,
            ok=True,
            status_code=resp.status_code,
            session_id=session_id,
            client_id=client_id,
            detail=None if body is not None else "下游返回非 JSON 响应",
        )
    except Exception as exc:
        return BroadcastResultItem(
            agent_id=agent.agent_id,
            base_url=agent.base_url,
            discovery_group=agent.discovery_group,
            ok=False,
            status_code=None,
            session_id=session_id,
            client_id=client_id,
            detail=str(exc),
        )


def create_app() -> FastAPI:
    """创建 Register Center 的 FastAPI 应用。

    逻辑：
    1. 初始化应用与内存仓库；
    2. 注册健康检查与 Agent CRUD 路由；
    3. 将所有路由绑定到同一仓库实例，保证进程内视图一致。

    关键分支/边界：
    - `GET /v1/agents/{agent_id}` 在不存在或分组不匹配时返回 404；
    - `GET /v1/agents` 必须显式传 `discovery_group`，不提供全量视图；
    - `POST /v1/agents` 对重复 `agent_id` 执行覆盖更新。

    与外部交互：
    - 通过 HTTP 暴露接口；不依赖数据库或远程服务。

    异常说明：
    - 路由内部将不存在资源场景转换为 `HTTPException(404)`；
    - 输入校验异常由 FastAPI/Pydantic 自动转换为 422。

    副作用说明：
    - 启动后会在内存中维护可变登记表。
    """

    app = FastAPI(
        title="DAgents Register Center",
        version="0.1.0",
        description="用于维护 agent_id -> base_url 映射的轻量目录服务（MVP 内存版）。",
    )
    store = AgentRegistryStore()

    @app.get("/health", response_model=HealthResponse, tags=["system"])
    def get_health() -> HealthResponse:
        """返回服务健康状态。

        逻辑：
        1. 读取当前登记总数；
        2. 返回固定状态 `ok` 与计数。

        关键分支/边界：
        - 无复杂分支，始终返回 200。

        与外部交互：
        - 仅读取进程内存仓库。

        异常说明：
        - 不主动吞异常，异常将由 FastAPI 统一处理。

        副作用说明：
        - 无。
        """

        return HealthResponse(status="ok", agents=store.count())

    @app.post("/v1/agents", response_model=AgentRecord, tags=["agents"])
    def upsert_agent(payload: AgentUpsertRequest, request: Request) -> AgentRecord:
        """登记或覆盖更新一条 Agent 记录。

        逻辑：
        1. 接收并校验请求体；
        2. 调用仓库执行 upsert；
        3. 返回写入后的完整记录（含服务端时间戳）。

        关键分支/边界：
        - 同 `agent_id` 重复写入时覆盖旧值；
        - `base_url`、`agent_id`、`discovery_group` 列表约束由模型层保证。

        与外部交互：
        - 无外部服务调用，仅更新内存仓库。

        异常说明：
        - 不捕获异常，依赖 FastAPI 统一返回错误响应。

        副作用说明：
        - 修改仓库内对应 `agent_id` 的记录。
        """

        _verify_shared_token(request)
        return store.upsert(payload)

    @app.get("/v1/agents", response_model=AgentListResponse, tags=["agents"])
    def list_agents(
        request: Request,
        discovery_group: str = Query(..., description="按发现分组精确筛选（必填）。"),
    ) -> AgentListResponse:
        """查询指定分组下的 Agent 列表。

        逻辑：
        1. 读取查询参数；
        2. 向仓库请求该分组内快照；
        3. 用标准响应壳包装并返回。

        关键分支/边界：
        - 分组参数必填，不支持全量查询；
        - 分组仅做精确匹配。

        与外部交互：
        - 无。

        异常说明：
        - 不主动吞异常。

        副作用说明：
        - 无。
        """

        _verify_shared_token(request)
        records = store.list(discovery_group=discovery_group)
        return AgentListResponse(agents=records)

    @app.get("/v1/agents/{agent_id}", response_model=AgentRecord, tags=["agents"])
    def get_agent(
        agent_id: str,
        request: Request,
        discovery_group: str = Query(..., description="调用方所属分组（必填）。"),
    ) -> AgentRecord:
        """按 agent_id 查询单条 Agent 记录（分组隔离）。

        逻辑：
        1. 调用仓库读取记录；
        2. 校验记录分组列表包含调用方分组；
        3. 命中则返回，否则抛出 404。

        关键分支/边界：
        - 不存在或分组不匹配时统一返回 404，避免跨组探测。

        与外部交互：
        - 无。

        异常说明：
        - 未命中场景抛 `HTTPException(404)`。

        副作用说明：
        - 无。
        """

        _verify_shared_token(request)
        record = store.get(agent_id)
        if record is None or discovery_group not in record.discovery_group:
            raise HTTPException(status_code=404, detail=f"agent_id={agent_id!r} 不存在")
        return record

    @app.delete("/v1/agents/{agent_id}", tags=["agents"])
    def delete_agent(agent_id: str, request: Request) -> dict[str, bool]:
        """按 `agent_id` 删除登记记录。

        逻辑：
        1. 调用仓库删除指定记录；
        2. 删除成功返回 `{"deleted": true}`；
        3. 未命中返回 404，避免调用方误判注销已生效。

        关键分支/边界：
        - 空 `agent_id` 由路由参数层约束，不额外处理；
        - 不存在记录时统一返回 404。

        与外部交互：
        - 无，仅操作内存仓库。

        异常说明：
        - 未命中时抛 `HTTPException(404)`。

        副作用说明：
        - 可能删除 `_records` 中一条登记。
        """

        _verify_shared_token(request)
        ok = store.delete(agent_id)
        if not ok:
            raise HTTPException(status_code=404, detail=f"agent_id={agent_id!r} 不存在")
        return {"deleted": True}

    @app.post("/v1/broadcast", response_model=BroadcastResponse, tags=["broadcast"])
    async def broadcast_message(payload: BroadcastRequest, request: Request) -> BroadcastResponse:
        """按分组列表向已注册 Agent 广播消息。

        逻辑：
        1. 根据请求中的 `discovery_group_ids` 汇总目标 Agent（去重）；
        2. 并发调用每个目标的 `/v1/messages` 发送广播消息；
        3. 汇总成功/失败数量及逐目标结果并返回。

        关键分支/边界：
        - 所有目标分组均无匹配时返回空结果（非错误）；
        - 单个目标请求失败不会中断整批广播；
        - 分组命中同一 agent 仅发送一次，避免重复入队。

        与外部交互：
        - 读取本地仓库中的注册记录；
        - 通过 HTTP 并发请求下游 Agent 服务。

        异常说明：
        - 单个下游异常会被转换为失败项并继续执行；
        - 本路由本身不主动抛 HTTP 异常，除非发生不可恢复的框架级错误。

        副作用说明：
        - 会触发多个下游 Agent 接收并处理同一条消息。
        """

        _verify_shared_token(request)
        targets_by_id: dict[str, AgentRecord] = {}
        for group_id in payload.discovery_group_ids:
            group_targets = store.list(discovery_group=group_id)
            for item in group_targets:
                targets_by_id[item.agent_id] = item
        targets = list(targets_by_id.values())
        if not targets:
            return BroadcastResponse(
                message=payload.message,
                discovery_group_ids=payload.discovery_group_ids,
                total_targets=0,
                success_count=0,
                failed_count=0,
                results=[],
            )

        async with httpx.AsyncClient(timeout=20.0) as client:
            tasks = [
                _broadcast_to_agent(
                    client,
                    agent=target,
                    message=payload.message,
                    source=payload.source,
                )
                for target in targets
            ]
            results = await asyncio.gather(*tasks)

        success_count = sum(1 for item in results if item.ok)
        failed_count = len(results) - success_count
        return BroadcastResponse(
            message=payload.message,
            discovery_group_ids=payload.discovery_group_ids,
            total_targets=len(results),
            success_count=success_count,
            failed_count=failed_count,
            results=results,
        )

    @app.post("/v1/relay", response_model=RelayResponse, tags=["relay"])
    async def relay_message(payload: RelayRequest, request: Request) -> RelayResponse:
        """按目标 Agent ID 中继单条消息到下游 Agent。

        逻辑：
        1. 读取并校验目标 Agent 是否存在；
        2. 若 `caller_groups` 非空，则校验与目标分组有交集；
        3. 透传消息字段调用目标 `/v1/messages`；
        4. 回传中继结果与 `session_id/client_id`（用于 SSE 跟踪）。

        关键分支/边界：
        - 目标不存在或分组不可见时返回 404；
        - 下游返回非 2xx 时转为 502，附带错误摘要；
        - 下游响应仅依赖 `session_id/client_id`，调用方据此建立并过滤 SSE。

        与外部交互：
        - 读取本地仓库；
        - 通过 HTTP 调用目标 Agent API。

        异常说明：
        - 目标不可见/不存在抛 `HTTPException(404)`；
        - 下游调用失败抛 `HTTPException(502)`。

        副作用说明：
        - 会触发目标 Agent 接收并处理一条新消息。
        """

        _verify_shared_token(request)
        target = store.get(payload.target_agent_id)
        if target is None:
            raise HTTPException(status_code=404, detail=f"agent_id={payload.target_agent_id!r} 不存在")
        if payload.caller_groups and not any(group in target.discovery_group for group in payload.caller_groups):
            raise HTTPException(status_code=404, detail=f"agent_id={payload.target_agent_id!r} 不存在")

        target_url = f"{target.base_url}/v1/messages"
        forward_payload = {
            "session_id": payload.session_id,
            "client_id": payload.client_id,
            "request_type": payload.request_type,
            "source": payload.source,
            "priority": payload.priority,
        }
        if payload.request_type == "resume":
            forward_payload["resume_value"] = payload.resume_value
        else:
            forward_payload["content"] = payload.content
        async with httpx.AsyncClient(timeout=20.0) as client:
            try:
                resp = await client.post(target_url, json=forward_payload, headers=_a2a_auth_headers())
            except Exception as exc:
                raise HTTPException(status_code=502, detail=f"relay 下游调用失败: {exc}") from exc
        if resp.status_code >= 400:
            raise HTTPException(status_code=502, detail=f"relay 下游返回错误: {resp.text[:300]}")
        try:
            body = resp.json()
        except Exception:
            body = {}
        return RelayResponse(
            accepted=True,
            target_agent_id=target.agent_id,
            target_base_url=target.base_url,
            session_id=payload.session_id,
            client_id=payload.client_id,
        )

    return app


app = create_app()
