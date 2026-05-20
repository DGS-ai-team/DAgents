"""触发器资源模型。"""

from __future__ import annotations

import time
from typing import Any, Literal
from uuid import uuid4

from pydantic import BaseModel, Field, model_validator

TriggerSourceType = Literal["manual", "interval", "once", "webhook", "queue", "file", "metric", "registry_event"]
TriggerRiskLevel = Literal["low", "medium", "high", "critical"]
TriggerFireStatus = Literal["queued", "skipped", "error"]


class TriggerDefinition(BaseModel):
    trigger_id: str = Field(default_factory=lambda: str(uuid4()), min_length=1)
    name: str = Field(min_length=1)
    description: str = ""
    owner: str = ""
    team: str = ""
    source_type: TriggerSourceType = "manual"
    condition: dict[str, Any] = Field(default_factory=dict)
    target_agent_id: str = "local"
    target_session_id: str | None = None
    client_id: str | None = None
    task_template: str = Field(min_length=1)
    risk_level: TriggerRiskLevel = "low"
    enabled: bool = False
    cooldown_seconds: int = 0
    max_concurrency: int = 1
    approval_policy: dict[str, Any] = Field(default_factory=dict)
    metadata: dict[str, Any] = Field(default_factory=dict)
    fire_count: int = 0
    last_fired_at: float | None = None
    next_fire_at: float | None = None
    created_at: float = Field(default_factory=time.time)
    updated_at: float = Field(default_factory=time.time)

    @model_validator(mode="after")
    def _validate_condition(self) -> TriggerDefinition:
        if self.source_type == "interval":
            interval = int(self.condition.get("interval_seconds") or 0)
            if interval <= 0:
                raise ValueError("interval trigger requires condition.interval_seconds > 0")
        if self.source_type == "once":
            fire_at = float(self.condition.get("fire_at") or 0)
            if fire_at <= 0:
                raise ValueError("once trigger requires condition.fire_at as unix seconds")
        if self.cooldown_seconds < 0:
            raise ValueError("cooldown_seconds must be >= 0")
        if self.max_concurrency < 1:
            raise ValueError("max_concurrency must be >= 1")
        return self

    def with_next_fire(self, now: float | None = None) -> TriggerDefinition:
        current = time.time() if now is None else now
        next_fire_at: float | None = None
        if self.enabled and self.source_type == "interval":
            interval = int(self.condition.get("interval_seconds") or 0)
            base = self.last_fired_at or current
            next_fire_at = max(current, base + interval)
        elif self.enabled and self.source_type == "once":
            fire_at = float(self.condition.get("fire_at") or 0)
            if self.last_fired_at is None and fire_at >= current:
                next_fire_at = fire_at
        return self.model_copy(update={"next_fire_at": next_fire_at, "updated_at": current})


class TriggerCreateIn(BaseModel):
    name: str = Field(min_length=1)
    description: str = ""
    owner: str = ""
    team: str = ""
    source_type: TriggerSourceType = "manual"
    condition: dict[str, Any] = Field(default_factory=dict)
    target_agent_id: str = "local"
    target_session_id: str | None = None
    client_id: str | None = None
    task_template: str = Field(min_length=1)
    risk_level: TriggerRiskLevel = "low"
    enabled: bool = False
    cooldown_seconds: int = 0
    max_concurrency: int = 1
    approval_policy: dict[str, Any] = Field(default_factory=dict)
    metadata: dict[str, Any] = Field(default_factory=dict)

    def to_definition(self, *, now: float | None = None) -> TriggerDefinition:
        current = time.time() if now is None else now
        return TriggerDefinition(
            name=self.name,
            description=self.description,
            owner=self.owner,
            team=self.team,
            source_type=self.source_type,
            condition=self.condition,
            target_agent_id=self.target_agent_id,
            target_session_id=self.target_session_id,
            client_id=self.client_id,
            task_template=self.task_template,
            risk_level=self.risk_level,
            enabled=self.enabled,
            cooldown_seconds=self.cooldown_seconds,
            max_concurrency=self.max_concurrency,
            approval_policy=self.approval_policy,
            metadata=self.metadata,
            created_at=current,
            updated_at=current,
        ).with_next_fire(current)


class TriggerUpdateIn(BaseModel):
    name: str | None = None
    description: str | None = None
    owner: str | None = None
    team: str | None = None
    source_type: TriggerSourceType | None = None
    condition: dict[str, Any] | None = None
    target_agent_id: str | None = None
    target_session_id: str | None = None
    client_id: str | None = None
    task_template: str | None = None
    risk_level: TriggerRiskLevel | None = None
    enabled: bool | None = None
    cooldown_seconds: int | None = None
    max_concurrency: int | None = None
    approval_policy: dict[str, Any] | None = None
    metadata: dict[str, Any] | None = None


class TriggerFireRecord(BaseModel):
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
    reason: str = "manual"
    payload: dict[str, Any] = Field(default_factory=dict)
    force: bool = False


class TriggerListResult(BaseModel):
    triggers: list[TriggerDefinition]


class TriggerHistoryResult(BaseModel):
    records: list[TriggerFireRecord]
