"""A2A Task 模型（M2）。"""

from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, Field

TaskKind = Literal["invoke", "notify"]
TaskStatus = Literal["queued", "delivered", "processing", "awaiting_caller", "completed", "failed", "expired"]
ReplyStatus = Literal["completed", "failed", "requires_input"]


class TaskCreateRequest(BaseModel):
    from_agent_id: str = Field(min_length=1)
    to_agent_id: str = Field(min_length=1)
    kind: TaskKind = "invoke"
    content: str = ""
    blob_ids: list[str] = Field(default_factory=list)
    caller_session_id: str = ""
    idempotency_key: str = ""
    ttl_seconds: int = Field(default=3600, ge=1, le=86400)
    trace_id: str = ""


class TaskCreateResponse(BaseModel):
    task_id: str
    status: TaskStatus
    to_agent_id: str


class TaskStoredRecord(BaseModel):
    task_id: str
    from_agent_id: str
    to_agent_id: str
    kind: TaskKind
    content: str = ""
    blob_ids: list[str] = Field(default_factory=list)
    caller_session_id: str = ""
    idempotency_key: str = ""
    trace_id: str = ""
    status: TaskStatus = "queued"
    created_at_unix: int
    updated_at_unix: int
    expires_at_unix: int
    delivered_at_unix: int | None = None
    result_text: str = ""
    result_status: ReplyStatus | None = None
    callee_session_id: str = ""
    error_detail: str = ""
    pending_caller_resume: dict[str, object] = Field(default_factory=dict)


class TaskRecord(TaskStoredRecord):
    pass


class InboxTaskItem(BaseModel):
    task_id: str
    from_agent_id: str
    kind: TaskKind
    content: str
    content_truncated: bool = False
    blob_ids: list[str] = Field(default_factory=list)
    caller_session_id: str = ""
    trace_id: str = ""
    created_at_unix: int
    expires_at_unix: int


class InboxResponse(BaseModel):
    tasks: list[InboxTaskItem]
    pending_count: int = 0


class TaskAckRequest(BaseModel):
    agent_id: str = Field(min_length=1)


class TaskReplyRequest(BaseModel):
    agent_id: str = Field(min_length=1)
    status: ReplyStatus
    result_text: str = ""
    callee_session_id: str = ""
    error_detail: str = ""


class TaskReplyResponse(BaseModel):
    task_id: str
    status: TaskStatus


class TaskGetResponse(BaseModel):
    task: TaskRecord


class AdminTaskListResponse(BaseModel):
    tasks: list[TaskRecord]
    total: int
    limit: int
    offset: int


class TaskCallerResumeRequest(BaseModel):
    caller_agent_id: str = Field(min_length=1)
    resume_value: dict[str, object] = Field(default_factory=dict)


class TaskCallerResumeResponse(BaseModel):
    task_id: str
    status: TaskStatus


class TaskCallerInputResponse(BaseModel):
    task_id: str
    ready: bool = False
    resume_value: dict[str, object] = Field(default_factory=dict)
