"""Agent 间交互协议：统一信封与任务状态模型。"""

from __future__ import annotations

import json
import time
import uuid
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field, model_validator

AgentPeerIntent = Literal["ask", "delegate", "notify", "broadcast", "task_update"]
AgentPeerTaskState = Literal[
    "queued",
    "running",
    "requires_input",
    "succeeded",
    "failed",
    "cancelled",
]
AgentPeerContentType = Literal["text/plain", "application/json"]


class AgentPeerCaller(BaseModel):
    """交互发起方元数据。"""

    model_config = ConfigDict(extra="forbid")

    agent_id: str
    session_id: str
    discovery_groups: list[str] = Field(default_factory=list)


class AgentPeerTarget(BaseModel):
    """交互目标（单 Agent 与分组广播二选一）。"""

    model_config = ConfigDict(extra="forbid")

    agent_id: str | None = None
    discovery_groups: list[str] = Field(default_factory=list)

    @model_validator(mode="after")
    def validate_target(self) -> "AgentPeerTarget":
        """校验目标选择互斥规则。

        逻辑：
        1. 读取 `agent_id` 与 `discovery_groups`；
        2. 校验两者不能同时为空；
        3. 校验两者不能同时有值，保证路由语义唯一。

        关键分支/边界：
        - 仅支持“点对点”或“按组”，不支持混合目标；
        - 发现分组列表中空字符串会被拒绝。

        与外部交互：
        - 无。

        异常说明：
        - 违反约束时抛出 `ValueError`。

        副作用说明：
        - 无。
        """

        has_agent = bool((self.agent_id or "").strip())
        cleaned_groups = [item.strip() for item in self.discovery_groups if item.strip()]
        if not has_agent and not cleaned_groups:
            raise ValueError("target.agent_id 与 target.discovery_groups 不能同时为空")
        if has_agent and cleaned_groups:
            raise ValueError("target.agent_id 与 target.discovery_groups 只能二选一")
        if len(cleaned_groups) != len(self.discovery_groups):
            raise ValueError("target.discovery_groups 中存在空值")
        self.discovery_groups = cleaned_groups
        if has_agent:
            self.agent_id = (self.agent_id or "").strip()
        return self


class AgentPeerPayload(BaseModel):
    """业务内容载荷。"""

    model_config = ConfigDict(extra="allow")

    content_type: AgentPeerContentType = "text/plain"
    content: str | dict[str, Any]


class AgentPeerTask(BaseModel):
    """任务语义字段。"""

    model_config = ConfigDict(extra="allow")

    task_id: str
    state: AgentPeerTaskState
    artifact_refs: list[str] = Field(default_factory=list)


class AgentPeerError(BaseModel):
    """错误语义字段。"""

    model_config = ConfigDict(extra="allow")

    code: str
    message: str
    retryable: bool = False


class AgentPeerEnvelope(BaseModel):
    """Agent 间交互统一信封（v1）。

    逻辑：
    1. 用统一字段承载请求/响应/事件，降低跨端协作成本；
    2. 要求 `trace_id` 在整条链路透传，便于排障；
    3. 支持 `task`/`error` 可选字段描述异步结果与失败语义。

    关键分支/边界：
    - `target` 的“单 Agent/多分组”互斥由 `AgentPeerTarget` 保证；
    - `intent=task_update` 时建议携带 `task` 字段；
    - `payload.content_type=application/json` 时建议 `content` 为对象。

    与外部交互：
    - 作为 HTTP body/工具输出/日志追踪的统一格式。

    异常说明：
    - 校验异常由 Pydantic 抛出，调用方可转为 HTTP 422 或工具错误文本。

    副作用说明：
    - 无。
    """

    model_config = ConfigDict(extra="allow")

    protocol_version: Literal["a2a-dagents/1.0"] = "a2a-dagents/1.0"
    trace_id: str
    message_id: str
    timestamp_unix_ms: int
    caller: AgentPeerCaller
    target: AgentPeerTarget
    intent: AgentPeerIntent
    payload: AgentPeerPayload
    task: AgentPeerTask | None = None
    error: AgentPeerError | None = None


def build_agent_peer_envelope(
    *,
    caller_agent_id: str,
    caller_session_id: str,
    caller_groups: list[str],
    target_agent_id: str | None,
    target_groups: list[str] | None,
    intent: AgentPeerIntent,
    payload_content: str | dict[str, Any],
    payload_content_type: AgentPeerContentType = "text/plain",
    trace_id: str | None = None,
    task: AgentPeerTask | None = None,
    error: AgentPeerError | None = None,
) -> AgentPeerEnvelope:
    """构造标准化 AgentPeer 信封。

    逻辑：
    1. 生成 `trace_id/message_id/timestamp_unix_ms`（允许上游传入 trace）；
    2. 组装 caller/target/payload；
    3. 执行 Pydantic 校验并返回结构化对象。

    关键分支/边界：
    - `target_agent_id` 与 `target_groups` 互斥规则由模型层统一校验；
    - `trace_id` 缺省时自动生成，便于端到端追踪。

    与外部交互：
    - 无直接网络调用，仅用于生成可序列化协议对象。

    异常说明：
    - 校验失败向上抛出 `ValidationError`。

    副作用说明：
    - 无。
    """

    final_trace_id = (trace_id or "").strip() or f"trace-{uuid.uuid4().hex}"
    final_message_id = f"msg-{uuid.uuid4().hex}"
    final_ts = int(time.time() * 1000)
    return AgentPeerEnvelope(
        trace_id=final_trace_id,
        message_id=final_message_id,
        timestamp_unix_ms=final_ts,
        caller=AgentPeerCaller(
            agent_id=caller_agent_id,
            session_id=caller_session_id,
            discovery_groups=caller_groups,
        ),
        target=AgentPeerTarget(
            agent_id=target_agent_id,
            discovery_groups=target_groups or [],
        ),
        intent=intent,
        payload=AgentPeerPayload(
            content_type=payload_content_type,
            content=payload_content,
        ),
        task=task,
        error=error,
    )


def parse_agent_peer_envelope_from_text(text: str) -> AgentPeerEnvelope | None:
    """从文本中解析 AgentPeer 信封。

    逻辑：
    1. 尝试将文本解析为 JSON 对象；
    2. 验证是否满足 `AgentPeerEnvelope` 结构；
    3. 成功返回对象，失败返回 `None`。

    关键分支/边界：
    - 非 JSON 文本直接返回 `None`；
    - JSON 结构不满足协议字段也返回 `None`。

    与外部交互：
    - 无。

    异常说明：
    - 内部吞解析异常并返回空，避免影响主流程。

    副作用说明：
    - 无。
    """

    raw = (text or "").strip()
    if not raw:
        return None
    try:
        data = json.loads(raw)
    except Exception:
        return None
    if not isinstance(data, dict):
        return None
    try:
        return AgentPeerEnvelope.model_validate(data)
    except Exception:
        return None
