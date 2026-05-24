"""触发器资源模型：定义、创建/更新入参、触发记录与 API 列表包装。"""

from __future__ import annotations

import time
from typing import Any, Literal
from uuid import uuid4

from pydantic import BaseModel, Field, model_validator

ScheduleKind = Literal["manual", "interval", "once"]
# 一次 fire 的落库状态：queued=已投递到 Agent 队列；skipped=未投递；error=投递异常。
TriggerFireStatus = Literal["queued", "skipped", "error"]


def infer_schedule_kind(condition: dict[str, Any]) -> ScheduleKind:
    """根据 `condition` 键推断调度类型（不再单独存 source_type）。

    逻辑：
    1. `interval_seconds > 0` → interval；
    2. 否则 `fire_at > 0` → once；
    3. 否则 → manual（仅手动 fire）。

    关键分支：同时设置 interval_seconds 与 fire_at 时抛 ValueError。
    """
    interval = int(condition.get("interval_seconds") or 0)
    fire_at = float(condition.get("fire_at") or 0)
    if interval > 0 and fire_at > 0:
        raise ValueError("condition cannot set both interval_seconds and fire_at")
    if interval > 0:
        return "interval"
    if fire_at > 0:
        return "once"
    return "manual"


def ensure_schedule_condition(condition: dict[str, Any]) -> ScheduleKind:
    """校验 condition 非空且含有效调度键，返回推断类型。

    逻辑：
    1. 拒绝 None / 空 dict；
    2. 须为 interval 或 once（不允许「无调度键」的 manual）。

    异常：不满足时抛 ValueError。
    """
    if not isinstance(condition, dict) or not condition:
        raise ValueError("condition is required and cannot be empty")
    kind = infer_schedule_kind(condition)
    if kind == "manual":
        raise ValueError("condition must include interval_seconds or fire_at")
    return kind


class TriggerDefinition(BaseModel):
    """触发器资源完整定义（持久化与 API 响应的主体）。

    职责：描述「何时、向哪个会话」将 `task_template` 渲染后投递给 Agent。

    关键字段：
    - `condition`：调度条件（`interval_seconds` / `fire_at`），类型由 `infer_schedule_kind` 推断；
    - `next_fire_at`：由 `with_next_fire` 根据 condition 与 `last_fired_at` 推算；
    - `enabled`：为 false 时仅 `force` 手动 fire 可绕过（见调度器）。
    """

    trigger_id: str = Field(default_factory=lambda: str(uuid4()), min_length=1)
    name: str = Field(min_length=1)
    condition: dict[str, Any] = Field(default_factory=dict)
    target_agent_id: str = "local"
    target_session_id: str | None = None
    client_id: str | None = None
    task_template: str = Field(min_length=1)
    enabled: bool = False
    fire_count: int = 0
    last_fired_at: float | None = None
    next_fire_at: float | None = None
    created_at: float = Field(default_factory=time.time)
    updated_at: float = Field(default_factory=time.time)

    @model_validator(mode="after")
    def _validate_condition(self) -> TriggerDefinition:
        """校验 condition，不满足时构造期抛 ValueError。"""
        ensure_schedule_condition(self.condition)
        return self

    def schedule_kind(self) -> ScheduleKind:
        """当前资源的调度类型（由 condition 推断）。"""
        return infer_schedule_kind(self.condition)

    def with_next_fire(self, now: float | None = None) -> TriggerDefinition:
        """根据 condition 与 enabled 重算 `next_fire_at`（不修改其它字段）。

        逻辑：
        1. interval：下次 = max(now, last_fired_at|now) + interval_seconds；
        2. once：若从未 fire 且 fire_at >= now，则 next = fire_at；
        3. manual 或 disabled：next_fire_at 置 None。

        副作用：返回新副本，并刷新 `updated_at`。
        """
        current = time.time() if now is None else now
        next_fire_at: float | None = None
        kind = self.schedule_kind()
        if self.enabled and kind == "interval":
            interval = int(self.condition.get("interval_seconds") or 0)
            base = self.last_fired_at or current
            next_fire_at = max(current, base + interval)
        elif self.enabled and kind == "once":
            fire_at = float(self.condition.get("fire_at") or 0)
            if self.last_fired_at is None and fire_at >= current:
                next_fire_at = fire_at
        return self.model_copy(update={"next_fire_at": next_fire_at, "updated_at": current})


class TriggerCreateIn(BaseModel):
    """创建触发器 API / 工具的请求体（不含运行时统计字段）。"""

    name: str = Field(min_length=1)
    condition: dict[str, Any]
    target_agent_id: str = "local"
    target_session_id: str | None = None
    client_id: str | None = None
    task_template: str = Field(min_length=1)

    @model_validator(mode="after")
    def _validate_create_condition(self) -> TriggerCreateIn:
        """创建入参的 condition 须非空且含有效调度键。"""
        ensure_schedule_condition(self.condition)
        return self

    def to_definition(self, *, now: float | None = None) -> TriggerDefinition:
        """转为可持久化的 `TriggerDefinition` 并计算首次 `next_fire_at`。

        逻辑：
        1. 拷贝请求字段并生成新 trigger_id；
        2. 新建资源默认 `enabled=true`；
        3. 调用 `with_next_fire` 初始化调度时间。
        """
        current = time.time() if now is None else now
        return TriggerDefinition(
            name=self.name,
            condition=self.condition,
            target_agent_id=self.target_agent_id,
            target_session_id=self.target_session_id,
            client_id=self.client_id,
            task_template=self.task_template,
            enabled=True,
            created_at=current,
            updated_at=current,
        ).with_next_fire(current)


class TriggerUpdateIn(BaseModel):
    """部分更新触发器；未设置的字段保持原值（PATCH / trigger_update 工具）。"""

    name: str | None = None
    condition: dict[str, Any] | None = None
    target_agent_id: str | None = None
    target_session_id: str | None = None
    client_id: str | None = None
    task_template: str | None = None
    enabled: bool | None = None

    @model_validator(mode="after")
    def _validate_patch_condition(self) -> TriggerUpdateIn:
        """若 PATCH 携带 condition，须非空且含有效调度键。"""
        if self.condition is not None:
            ensure_schedule_condition(self.condition)
        return self


class TriggerFireRecord(BaseModel):
    """单次触发（手动或调度）的执行记录，追加写入 store 的 history。"""

    fire_id: str = Field(default_factory=lambda: str(uuid4()), min_length=1)
    trigger_id: str = Field(min_length=1)
    status: TriggerFireStatus
    reason: str
    session_id: str | None = None
    client_id: str | None = None
    content: str = ""
    message: str = ""
    payload: dict[str, Any] = Field(default_factory=dict)
    fired_at: float = Field(default_factory=time.time)


class TriggerFireIn(BaseModel):
    """手动 fire API 的请求体。"""

    reason: str = "manual"
    payload: dict[str, Any] = Field(default_factory=dict)
    force: bool = False


class TriggerListResult(BaseModel):
    """GET /v1/triggers 响应包装。"""

    triggers: list[TriggerDefinition]


class TriggerHistoryResult(BaseModel):
    """GET /v1/triggers/{id}/history 响应包装。"""

    records: list[TriggerFireRecord]
