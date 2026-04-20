"""Agent Service 统一接口抽象（供 CLI/API 客户端复用）。"""

from __future__ import annotations

from typing import Any, AsyncIterator, Literal, Protocol, Self

from pydantic import BaseModel, ConfigDict, Field, model_validator

MessagePriority = Literal["resume", "human", "other"]
RequestType = Literal["message", "resume"]


class AgentSubmitRequest(BaseModel):
    """统一提交请求模型。

    逻辑：
    1. `request_type=message` 时要求 `content` 非空白；
    2. `request_type=resume` 时使用 `resume_value`（结构见 `app.schemas.approval`）；
    3. `source/priority` 用于服务端路由与观测；**`priority="human"`** 仅影响队列优先级，不表示服务端会自动取消在途 turn（需客户端显式调取消 API）。
    4. **`request_type="resume"`** 且未显式指定其它优先级时（仍为默认 **`other`**），自动改为 **`resume`**，与队列 **`PRIORITY_RESUME`** 对齐（审批后继续、不打断同会话逻辑）。
    """

    model_config = ConfigDict(frozen=True)

    session_id: str = Field(min_length=1)
    request_type: RequestType = "message"
    content: str | None = None
    resume_value: Any = None
    source: str = "cli"
    priority: MessagePriority = "other"

    @model_validator(mode="before")
    @classmethod
    def _resume_default_priority_before(cls, data: Any) -> Any:
        """`resume` 且未显式写 `priority`（或仍为默认 `other`）时改为 `resume`，与队列 PRIORITY_RESUME 一致。

        须在 `before` 阶段改 dict：after 阶段 `model_copy` 返回值无法参与 `__init__` 校验链路。
        """
        if not isinstance(data, dict):
            return data
        if data.get("request_type") != "resume":
            return data
        if data.get("priority", "other") == "other":
            return {**data, "priority": "resume"}
        return data

    @model_validator(mode="after")
    def _validate_by_request_type(self) -> Self:
        if self.request_type == "message":
            if self.content is None or not str(self.content).strip():
                raise ValueError("content 在 request_type=message 时不能为空。")
        return self


class AgentSubmitResult(BaseModel):
    """统一提交结果模型（对应 `/v1/messages` 返回）。"""

    model_config = ConfigDict(frozen=True)

    accepted: bool
    request_id: str
    session_id: str
    priority: MessagePriority


class AgentCancelTurnResult(BaseModel):
    """取消当前 turn 的返回（对应 `POST /v1/sessions/{session_id}/cancel`）。"""

    model_config = ConfigDict(frozen=True)

    session_id: str
    cancelled: bool


class AgentSessionCreateResult(BaseModel):
    """统一会话创建结果模型。"""

    model_config = ConfigDict(frozen=True)

    session_id: str
    created: bool = True


class AgentStreamEventData(BaseModel):
    """统一流事件模型（对应 SSE `data` JSON 结构）。"""

    model_config = ConfigDict(frozen=True)

    request_id: str
    session_id: str
    type: str
    seq: int
    ts: str
    data: dict[str, Any] = Field(default_factory=dict)


class AgentEventEnvelope(BaseModel):
    """统一事件信封（服务内部与传输层共用）。

    字段说明：
    - `event_type`：事件类型（如 `assistant`/`reasoning`/`usage`/`tool_call`/`tool_result`/`approval_required`/`error`/`done`）。
    - `payload`：事件业务数据；`approval_required` 时建议符合 **`ApprovalRequiredEnvelopePayload`**（见 `app.schemas.approval`）。
    - `meta`：附加元信息（会话、模型、耗时等）。
    """

    model_config = ConfigDict(frozen=True)

    event_type: str
    payload: dict[str, Any] = Field(default_factory=dict)
    meta: dict[str, Any] = Field(default_factory=dict)


class AgentServiceClient(Protocol):
    """Agent Service 客户端协议（CLI 仅依赖该抽象）。

    使用场景：用于隔离 CLI 与传输实现（HTTP、本地直连、未来 gRPC）。

    字段说明：
    - `submit(...)`：提交一条消息或 resume 请求，返回 `AgentSubmitResult`。
    - `cancel_current_turn(...)`：请求取消指定 session 正在执行的 `_handle_message`（无在途任务则 `cancelled=False`）。
    - `stream(...)`：按 `request_id` 订阅事件流，逐条产出 `AgentStreamEventData`。

    返回说明：
    - 成功：返回模型对象或异步事件迭代器。
    - 失败：实现层可抛异常，由调用方统一兜底展示。

    调用范例：
    - `session = await client.create_session()`
    - `await client.submit(AgentSubmitRequest(session_id=session.session_id, content="你好"))`
    - `async for ev in client.stream("req-id"): ...`
    """

    async def create_session(self, session_id: str | None = None) -> AgentSessionCreateResult:
        ...

    async def submit(self, request: AgentSubmitRequest) -> AgentSubmitResult:
        ...

    async def cancel_current_turn(self, session_id: str) -> AgentCancelTurnResult:
        ...

    async def stream(self, request_id: str) -> AsyncIterator[AgentStreamEventData]:
        ...
