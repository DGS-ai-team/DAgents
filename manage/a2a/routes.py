"""A2A Task HTTP 路由（M2）。"""

from __future__ import annotations

from fastapi import APIRouter, HTTPException, Query, Request

from manage.a2a.models import (
    InboxResponse,
    TaskAckRequest,
    TaskCallerInputResponse,
    TaskCallerResumeRequest,
    TaskCallerResumeResponse,
    TaskCreateRequest,
    TaskCreateResponse,
    TaskGetResponse,
    TaskReplyRequest,
    TaskReplyResponse,
)
from manage.a2a.store import A2ATaskStore
from manage.platform.audit import AuditLog
from manage.platform.auth import AuthContext, audit_actor, authenticate, ensure_node_identity
from manage.platform.metrics import record_a2a_operation
from manage.registry.store import AgentRegistryStore


def _ensure_node_agent_request(request: Request, agent_id: str, auth: AuthContext) -> None:
    ensure_node_identity(request, agent_id, auth)


def _validate_task_target(registry: AgentRegistryStore, to_agent_id: str) -> tuple[bool, str | None]:
    record = registry.get(to_agent_id)
    if record is None:
        return False, "target_not_found"
    if record.status != "online":
        return False, "target_offline"
    if not record.expose_to_peers:
        return False, "target_not_exposed"
    return True, None


def _ensure_task_reader(task_from: str, task_to: str, reader_agent_id: str) -> None:
    if reader_agent_id not in (task_from, task_to):
        raise HTTPException(status_code=403, detail="无权读取该 task")


def build_a2a_router(registry: AgentRegistryStore, store: A2ATaskStore, audit: AuditLog) -> APIRouter:
    router = APIRouter(tags=["a2a"])

    @router.post("/v1/a2a/tasks", response_model=TaskCreateResponse)
    def create_task(payload: TaskCreateRequest, request: Request) -> TaskCreateResponse:
        auth = authenticate(request)
        _ensure_node_agent_request(request, payload.from_agent_id, auth)
        try:
            task, created = store.create(payload, validate_target=lambda agent_id: _validate_task_target(registry, agent_id))
        except ValueError as exc:
            reason = str(exc)
            record_a2a_operation(operation="create", status=reason)
            if reason == "target_not_found":
                raise HTTPException(status_code=404, detail=reason) from exc
            if reason in ("target_offline", "target_not_exposed"):
                raise HTTPException(status_code=403, detail=reason) from exc
            raise HTTPException(status_code=400, detail=reason) from exc
        if created:
            audit.record(
                actor=audit_actor(request, auth, fallback_agent_id=payload.from_agent_id),
                action="a2a.task.create",
                target_agent_id=payload.to_agent_id,
                detail={
                    "task_id": task.task_id,
                    "from_agent_id": payload.from_agent_id,
                    "kind": payload.kind,
                },
            )
        record_a2a_operation(operation="create", status="ok")
        return TaskCreateResponse(task_id=task.task_id, status=task.status, to_agent_id=task.to_agent_id)

    @router.get("/v1/a2a/inbox", response_model=InboxResponse)
    def poll_inbox(
        request: Request,
        agent_id: str = Query(..., min_length=1),
        limit: int = Query(default=10, ge=1, le=50),
        wait: float = Query(default=0.0, ge=0.0, le=60.0),
    ) -> InboxResponse:
        auth = authenticate(request)
        _ensure_node_agent_request(request, agent_id, auth)
        tasks, pending = store.poll_inbox(agent_id, limit=limit, wait_seconds=wait)
        if tasks:
            audit.record(
                actor=audit_actor(request, auth, fallback_agent_id=agent_id),
                action="a2a.inbox.deliver",
                target_agent_id=agent_id,
                detail={"count": len(tasks), "task_ids": [item.task_id for item in tasks]},
            )
        record_a2a_operation(operation="inbox", status="ok")
        return InboxResponse(tasks=tasks, pending_count=pending)

    @router.post("/v1/a2a/tasks/{task_id}/ack", response_model=TaskGetResponse)
    def ack_task(task_id: str, payload: TaskAckRequest, request: Request) -> TaskGetResponse:
        auth = authenticate(request)
        _ensure_node_agent_request(request, payload.agent_id, auth)
        task = store.ack(task_id, payload.agent_id)
        if task is None:
            record_a2a_operation(operation="ack", status="not_found")
            raise HTTPException(status_code=404, detail=f"task_id={task_id!r} 不存在或无权操作")
        audit.record(
            actor=audit_actor(request, auth, fallback_agent_id=payload.agent_id),
            action="a2a.task.ack",
            target_agent_id=payload.agent_id,
            detail={"task_id": task_id, "status": task.status},
        )
        record_a2a_operation(operation="ack", status="ok")
        return TaskGetResponse(task=task)

    @router.post("/v1/a2a/tasks/{task_id}/reply", response_model=TaskReplyResponse)
    def reply_task(task_id: str, payload: TaskReplyRequest, request: Request) -> TaskReplyResponse:
        auth = authenticate(request)
        _ensure_node_agent_request(request, payload.agent_id, auth)
        task = store.reply(task_id, payload.agent_id, payload)
        if task is None:
            record_a2a_operation(operation="reply", status="not_found")
            raise HTTPException(status_code=404, detail=f"task_id={task_id!r} 不存在或无权操作")
        audit.record(
            actor=audit_actor(request, auth, fallback_agent_id=payload.agent_id),
            action="a2a.task.reply",
            target_agent_id=task.from_agent_id,
            detail={"task_id": task_id, "status": task.status, "result_status": payload.status},
        )
        record_a2a_operation(operation="reply", status="ok")
        return TaskReplyResponse(task_id=task.task_id, status=task.status)

    @router.post("/v1/a2a/tasks/{task_id}/caller_resume", response_model=TaskCallerResumeResponse)
    def caller_resume(task_id: str, payload: TaskCallerResumeRequest, request: Request) -> TaskCallerResumeResponse:
        auth = authenticate(request)
        _ensure_node_agent_request(request, payload.caller_agent_id, auth)
        task = store.submit_caller_resume(task_id, payload.caller_agent_id, payload.resume_value)
        if task is None:
            record_a2a_operation(operation="caller_resume", status="not_found")
            raise HTTPException(status_code=404, detail=f"task_id={task_id!r} 不存在、无权操作或状态不允许")
        audit.record(
            actor=audit_actor(request, auth, fallback_agent_id=payload.caller_agent_id),
            action="a2a.task.caller_resume",
            target_agent_id=task.to_agent_id,
            detail={"task_id": task_id},
        )
        record_a2a_operation(operation="caller_resume", status="ok")
        return TaskCallerResumeResponse(task_id=task.task_id, status=task.status)

    @router.get("/v1/a2a/tasks/{task_id}/caller_input", response_model=TaskCallerInputResponse)
    def poll_caller_input(
        task_id: str,
        request: Request,
        agent_id: str = Query(..., min_length=1),
        wait: float = Query(default=0.0, ge=0.0, le=60.0),
    ) -> TaskCallerInputResponse:
        auth = authenticate(request)
        _ensure_node_agent_request(request, agent_id, auth)
        resume_value, task = store.poll_caller_input(task_id, agent_id, wait_seconds=wait)
        if task is None:
            record_a2a_operation(operation="caller_input", status="not_found")
            raise HTTPException(status_code=404, detail=f"task_id={task_id!r} 不存在或无权操作")
        record_a2a_operation(operation="caller_input", status="ok")
        return TaskCallerInputResponse(
            task_id=task.task_id,
            ready=resume_value is not None,
            resume_value=resume_value or {},
        )

    @router.get("/v1/a2a/tasks/{task_id}", response_model=TaskGetResponse)
    def get_task(
        task_id: str,
        request: Request,
        caller_agent_id: str = Query(..., min_length=1),
    ) -> TaskGetResponse:
        auth = authenticate(request)
        _ensure_node_agent_request(request, caller_agent_id, auth)
        task = store.get(task_id)
        if task is None:
            record_a2a_operation(operation="get", status="not_found")
            raise HTTPException(status_code=404, detail=f"task_id={task_id!r} 不存在")
        _ensure_task_reader(task.from_agent_id, task.to_agent_id, caller_agent_id)
        record_a2a_operation(operation="get", status="ok")
        return TaskGetResponse(task=task)

    return router
