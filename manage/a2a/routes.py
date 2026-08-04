"""A2A Task HTTP 路由。

A2A inbox callee（GET /inbox、ack/reply/caller_input）与新建 invoke task 已退役：
跨 Node 协作请使用工作组。Admin Task 列表与 GET 单任务只读保留以便排障。
"""

from __future__ import annotations

from fastapi import APIRouter, HTTPException, Query, Request
from fastapi.responses import JSONResponse

from manage.a2a.models import (
    TaskGetResponse,
)
from manage.a2a.store import A2ATaskStore
from manage.platform.audit import AuditLog
from manage.platform.auth import AuthContext, authenticate, ensure_node_identity
from manage.platform.metrics import record_a2a_operation
from manage.registry.store import AgentRegistryStore

_A2A_INBOX_RETIRED = {
    "error": {
        "code": "a2a_inbox_retired",
        "message": "A2A inbox / invoke 已退役：跨机器协作请使用工作组（Workgroup）",
    }
}


def _ensure_node_agent_request(request: Request, agent_id: str, auth: AuthContext) -> None:
    ensure_node_identity(request, agent_id, auth)


def _ensure_task_reader(task_from: str, task_to: str, reader_agent_id: str) -> None:
    if reader_agent_id not in (task_from, task_to):
        raise HTTPException(status_code=403, detail="无权读取该 task")


def _retired() -> JSONResponse:
    record_a2a_operation(operation="retired", status="gone")
    return JSONResponse(status_code=410, content=_A2A_INBOX_RETIRED)


def build_a2a_router(registry: AgentRegistryStore, store: A2ATaskStore, audit: AuditLog) -> APIRouter:
    _ = registry
    _ = audit
    router = APIRouter(tags=["a2a"])

    @router.post("/v1/a2a/tasks")
    def create_task() -> JSONResponse:
        return _retired()

    @router.get("/v1/a2a/inbox")
    def poll_inbox() -> JSONResponse:
        return _retired()

    @router.post("/v1/a2a/tasks/{task_id}/ack")
    def ack_task(task_id: str) -> JSONResponse:
        _ = task_id
        return _retired()

    @router.post("/v1/a2a/tasks/{task_id}/reply")
    def reply_task(task_id: str) -> JSONResponse:
        _ = task_id
        return _retired()

    @router.post("/v1/a2a/tasks/{task_id}/caller_notify")
    def caller_notify(task_id: str) -> JSONResponse:
        _ = task_id
        return _retired()

    @router.post("/v1/a2a/tasks/{task_id}/caller_resume")
    def caller_resume(task_id: str) -> JSONResponse:
        _ = task_id
        return _retired()

    @router.get("/v1/a2a/tasks/{task_id}/caller_input")
    def poll_caller_input(task_id: str) -> JSONResponse:
        _ = task_id
        return _retired()

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
