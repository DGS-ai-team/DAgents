"""会话上下文模型：消息历史与 OpenAI runtime 对齐的上下文结构。"""

from __future__ import annotations

import copy
import json
from enum import StrEnum
from typing import Any

from pydantic import BaseModel, ConfigDict, Field

class RunTurnPhase(StrEnum):
    """`run_turn` 内显式阶段（供上层观测；与 `pending_tool_calls` / 流式 buffer 配合理解）。"""

    IDLE = "idle"
    BRANCH_RESOLVING = "branch_resolving"
    MODEL_STREAMING = "model_streaming"
    AWAITING_TOOL_EXECUTION = "awaiting_tool_execution"


class SummaryCompressionPhase(StrEnum):
    """summary 压缩流程阶段（独立于主 `run_turn_phase`，避免并发竞态）。"""

    NOT_STARTED = "not_started"
    IDLE = "idle"
    PREPARING = "preparing"
    SUMMARIZING = "summarizing"


def _json_safe_deep(obj: Any) -> Any:
    """将对象规范为可 `json.dumps` 的结构（用于落盘前深拷贝校验）。"""
    return json.loads(json.dumps(obj, ensure_ascii=False))


class MessageRecord(BaseModel):
    """单条对话消息记录。"""

    model_config = ConfigDict(frozen=True)

    role: str = Field(description="消息角色（如 user/assistant/tool/system）。")
    content: str = Field(default="", description="消息正文文本。")
    meta: dict[str, Any] = Field(
        default_factory=dict,
        description="结构化附加元数据（如 tool_call_id、tool_calls 等）。",
    )


def _openai_messages_to_message_records(messages: list[dict[str, Any]]) -> list[MessageRecord]:
    """将 OpenAI 消息 dict 列表派生为 `MessageRecord` 列表供 `history` 列持久化。"""
    records: list[MessageRecord] = []
    for m in messages:
        if not isinstance(m, dict):
            continue
        role = str(m.get("role", "") or "")
        content = m.get("content", "")
        ctext = "" if content is None else str(content)
        if role == "tool":
            records.append(
                MessageRecord(
                    role="tool",
                    content=ctext,
                    meta={"tool_call_id": str(m.get("tool_call_id", "") or "")},
                )
            )
        elif role == "assistant" and m.get("tool_calls"):
            records.append(
                MessageRecord(
                    role="assistant",
                    content=ctext,
                    meta={"tool_calls": _json_safe_deep(m.get("tool_calls"))},
                )
            )
        else:
            records.append(MessageRecord(role=role or "unknown", content=ctext, meta={}))
    return records


class ConversationContext(BaseModel):
    """会话可持久化上下文（历史消息 + runtime 常驻字段）。

    逻辑：
    1. `history` 保存人类可读历史；
    2. `openai_messages` / `pending_tool_calls` / `run_turn_phase` / `tool_loop_count` 为主流程字段；
    3. 仅保留主流程常驻字段；summary 压缩态由编排层内存维护，不持久化。
    """

    model_config = ConfigDict(validate_assignment=True)

    history: list[MessageRecord] = Field(default_factory=list, description="会话历史消息列表。")
    openai_messages: list[dict[str, Any]] = Field(
        default_factory=list,
        description="OpenAI 对话消息权威副本（不含 system）。",
    )
    pending_tool_calls: list[dict[str, Any]] = Field(
        default_factory=list,
        description="待执行/待审批工具规格（dict 形态，含 call_id/name/arguments）。",
    )
    run_turn_phase: RunTurnPhase = Field(default=RunTurnPhase.IDLE, description="持久化态 run_turn 阶段。")
    messages_total_tokens: int = Field(
        default=0,
        description="当前 `openai_messages` 的粗略 token 总量（由运行时维护）。",
    )
    tool_loop_count: int = Field(default=0, description="跨回合累计工具循环计数。")
    loaded_skills: list[dict[str, str]] = Field(
        default_factory=list,
        description="已加载技能列表（`skill_name/description`），用于持久化当前会话技能态。",
    )

    def add_turn(self, *, role: str, content: str, meta: dict[str, Any] | None = None) -> None:
        """向 `history` 末尾追加一条消息（仅内存，不写库）。"""
        role_text = (role or "").strip()
        if not role_text:
            raise ValueError("role 不能为空。")
        self.history.append(MessageRecord(role=role_text, content=content, meta=dict(meta or {})))

    def unpack_for_openai_runtime(
        self,
    ) -> tuple[
        list[dict[str, Any]],
        list[dict[str, Any]],
        int,
        list[dict[str, str]],
    ]:
        """解析为 OpenAI runtime 使用的常驻字段。"""
        messages: list[dict[str, Any]] = []
        for item in self.openai_messages:
            if isinstance(item, dict):
                messages.append(copy.deepcopy(item))

        pending_specs: list[dict[str, Any]] = []
        for item in self.pending_tool_calls:
            if not isinstance(item, dict):
                continue
            cid = item.get("call_id")
            if not cid:
                continue
            args = item.get("arguments")
            if not isinstance(args, dict):
                args = {}
            pending_specs.append(
                {
                    "call_id": str(cid),
                    "name": str(item.get("name", "") or ""),
                    "arguments": dict(args),
                }
            )
        loaded_skills: list[dict[str, str]] = []
        for item in self.loaded_skills:
            if not isinstance(item, dict):
                continue
            skill_name = str(item.get("skill_name") or "").strip()
            description = str(item.get("description") or "").strip()
            if not skill_name:
                continue
            loaded_skills.append({"skill_name": skill_name, "description": description})
        return (
            messages,
            pending_specs,
            max(0, int(self.messages_total_tokens)),
            loaded_skills,
        )

    @classmethod
    def from_openai_runtime(
        cls,
        *,
        openai_messages: list[dict[str, Any]],
        pending_tool_calls: list[dict[str, Any]],
        run_turn_phase: RunTurnPhase = RunTurnPhase.IDLE,
        messages_total_tokens: int = 0,
        tool_loop_count: int = 0,
        loaded_skills: list[dict[str, str]] | None = None,
    ) -> 'ConversationContext':
        """由 runtime 内存态组装可写入 sqlite 的 `ConversationContext`。"""
        history = _openai_messages_to_message_records(openai_messages)
        return cls(
            history=history,
            openai_messages=_json_safe_deep(openai_messages),
            pending_tool_calls=_json_safe_deep(pending_tool_calls),
            run_turn_phase=run_turn_phase,
            messages_total_tokens=max(0, int(messages_total_tokens)),
            tool_loop_count=max(0, int(tool_loop_count)),
            loaded_skills=_json_safe_deep(list(loaded_skills or [])),
        )


class PendingToolCall(BaseModel):
    """OpenAI tool calling 下单次待执行/待审批的工具调用规格（推理上下文的一部分）。"""

    model_config = ConfigDict(frozen=True)

    call_id: str = Field(description="工具调用唯一标识（与 assistant.tool_calls[].id 对应）。")
    name: str = Field(default="", description="工具名称。")
    arguments: dict[str, Any] = Field(
        default_factory=dict,
        description="工具调用参数（已解析为字典形态）。",
    )


class OpenAIConversationContext(BaseModel):
    """OpenAI 隐式 ReAct 的可变推理上下文（由上层绑定会话并负责持久化）。"""

    model_config = ConfigDict(validate_assignment=True, extra="ignore")

    session_id: str = Field(default="", description="会话 ID。")
    sse_client_id: str = Field(
        default="",
        description=(
            "本进程内最近一条入站 MessageEnvelope 携带的 client_id（非空时刷新）；"
            "供异步工具提交时写入 AsyncToolJob，使回灌入队仍带 SSE 通道。"
            "不入库，重启后为空直至再次收到带 client_id 的请求。"
        ),
    )
    messages: list[dict[str, Any]] = Field(default_factory=list, description="OpenAI 对话消息列表。")
    pending_tool_calls: list[PendingToolCall] = Field(
        default_factory=list,
        description="待执行/待审批的工具调用队列。",
    )
    run_turn_phase: RunTurnPhase = Field(default=RunTurnPhase.IDLE, description="当前 run_turn 所处阶段。")
    messages_total_tokens: int = Field(
        default=0,
        description="当前 `messages` 的粗略 token 总量（由运行时维护）。",
    )
    tool_loop_count: int = Field(default=0, description="跨 run_turn 累积的工具循环计数。")
    loaded_skills: list[dict[str, str]] = Field(
        default_factory=list,
        description="当前会话已加载技能列表（`skill_name/description`）。",
    )
    assistant_stream_buffer: str = Field(default="", repr=False, description="流式输出增量缓冲。")
    active_client_id: str = Field(default="", repr=False, description="当前入站消息的 SSE client_id，仅用于本轮工具路由。")

    def normalized_run_turn_phase_for_persist(self) -> RunTurnPhase:
        """将 `run_turn_phase` 规范为可写入 sqlite 的值。"""
        p = self.run_turn_phase
        if p in (RunTurnPhase.BRANCH_RESOLVING, RunTurnPhase.MODEL_STREAMING):
            return RunTurnPhase.IDLE
        if p == RunTurnPhase.AWAITING_TOOL_EXECUTION and not self.pending_tool_calls:
            return RunTurnPhase.IDLE
        return p

    @classmethod
    def from_conversation_context(cls, cc: ConversationContext) -> 'OpenAIConversationContext':
        """从持久化态 `ConversationContext` 还原为推理上下文。"""
        messages, pending_specs, total_tokens, loaded_skills = cc.unpack_for_openai_runtime()
        pending = [
            PendingToolCall(
                call_id=str(s["call_id"]),
                name=str(s.get("name", "")),
                arguments=dict(s.get("arguments") or {}),
            )
            for s in pending_specs
        ]
        return cls(
            session_id="",
            messages=messages,
            pending_tool_calls=pending,
            run_turn_phase=cc.run_turn_phase,
            messages_total_tokens=max(0, int(total_tokens)),
            tool_loop_count=max(0, int(cc.tool_loop_count)),
            loaded_skills=list(loaded_skills),
        )

    def to_conversation_context(self) -> ConversationContext:
        """将当前推理上下文编码为可写入 sqlite 的 `ConversationContext`。"""
        specs = [
            {"call_id": p.call_id, "name": p.name, "arguments": dict(p.arguments)} for p in self.pending_tool_calls
        ]
        return ConversationContext.from_openai_runtime(
            openai_messages=self.messages,
            pending_tool_calls=specs,
            run_turn_phase=self.normalized_run_turn_phase_for_persist(),
            messages_total_tokens=max(0, int(self.messages_total_tokens)),
            tool_loop_count=max(0, int(self.tool_loop_count)),
            loaded_skills=list(self.loaded_skills),
        )
