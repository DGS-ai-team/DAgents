"""触发器工具：供 Agent 管理和唤起受治理的触发器。"""

from __future__ import annotations

import json
from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

from app.context.models import OpenAIConversationContext
from app.harness.tools.tool import tool
from app.harness.triggers.models import TriggerCreateIn, TriggerFireIn, TriggerUpdateIn
from app.harness.triggers.runtime import get_trigger_scheduler, get_trigger_store

TriggerSourceArg = Literal["manual", "interval", "once", "webhook", "queue", "file", "metric", "registry_event"]
TriggerRiskArg = Literal["low", "medium", "high", "critical"]


class TriggerListArgs(BaseModel):
    model_config = ConfigDict(extra="forbid")

    include_disabled: bool = True


class TriggerGetArgs(BaseModel):
    model_config = ConfigDict(extra="forbid")

    trigger_id: str = Field(min_length=1)


class TriggerCreateArgs(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str = Field(min_length=1)
    task_template: str = Field(min_length=1)
    description: str = ""
    owner: str = ""
    team: str = ""
    source_type: TriggerSourceArg = "manual"
    condition: dict[str, Any] = Field(default_factory=dict)
    target_session_id: str | None = None
    client_id: str | None = None
    risk_level: TriggerRiskArg = "low"
    enabled: bool = False
    cooldown_seconds: int = Field(default=0, ge=0)
    max_concurrency: int = Field(default=1, ge=1)
    approval_policy: dict[str, Any] = Field(default_factory=dict)
    metadata: dict[str, Any] = Field(default_factory=dict)


class TriggerUpdateArgs(BaseModel):
    model_config = ConfigDict(extra="forbid")

    trigger_id: str = Field(min_length=1)
    name: str | None = None
    task_template: str | None = None
    description: str | None = None
    owner: str | None = None
    team: str | None = None
    source_type: TriggerSourceArg | None = None
    condition: dict[str, Any] | None = None
    target_session_id: str | None = None
    client_id: str | None = None
    risk_level: TriggerRiskArg | None = None
    enabled: bool | None = None
    cooldown_seconds: int | None = Field(default=None, ge=0)
    max_concurrency: int | None = Field(default=None, ge=1)
    approval_policy: dict[str, Any] | None = None
    metadata: dict[str, Any] | None = None


class TriggerDeleteArgs(BaseModel):
    model_config = ConfigDict(extra="forbid")

    trigger_id: str = Field(min_length=1)


class TriggerFireArgs(BaseModel):
    model_config = ConfigDict(extra="forbid")

    trigger_id: str = Field(min_length=1)
    reason: str = "agent_tool"
    payload: dict[str, Any] = Field(default_factory=dict)
    force: bool = False


@tool("trigger_list")
def trigger_list(include_disabled: bool = True) -> str:
    """列出当前触发器。用于查看已配置的自主行动入口，不会执行触发器。"""

    triggers = get_trigger_store().list_triggers()
    if not include_disabled:
        triggers = [item for item in triggers if item.enabled]
    return _json_text({"ok": True, "triggers": [item.model_dump() for item in triggers]})


@tool("trigger_get")
def trigger_get(trigger_id: str) -> str:
    """查看单个触发器的配置和状态。不会执行触发器。"""

    trigger = get_trigger_store().get_trigger(trigger_id.strip())
    if trigger is None:
        return _json_text({"ok": False, "error": "trigger not found", "trigger_id": trigger_id})
    return _json_text({"ok": True, "trigger": trigger.model_dump()})


@tool("trigger_create")
def trigger_create(
    name: str,
    task_template: str,
    description: str = "",
    owner: str = "",
    team: str = "",
    source_type: TriggerSourceArg = "manual",
    condition: dict[str, Any] | None = None,
    target_session_id: str | None = None,
    client_id: str | None = None,
    risk_level: TriggerRiskArg = "low",
    enabled: bool = False,
    cooldown_seconds: int = 0,
    max_concurrency: int = 1,
    approval_policy: dict[str, Any] | None = None,
    metadata: dict[str, Any] | None = None,
) -> str:
    """创建触发器。用于沉淀定时、事件、指标等自主唤起规则；启用后会被调度器消费。"""

    body = TriggerCreateIn(
        name=name,
        description=description,
        owner=owner,
        team=team,
        source_type=source_type,
        condition=condition or {},
        target_session_id=target_session_id,
        client_id=client_id,
        task_template=task_template,
        risk_level=risk_level,
        enabled=enabled,
        cooldown_seconds=cooldown_seconds,
        max_concurrency=max_concurrency,
        approval_policy=approval_policy or {},
        metadata=metadata or {},
    )
    trigger = get_trigger_store().create_trigger(body.to_definition())
    return _json_text({"ok": True, "trigger": trigger.model_dump()})


@tool("trigger_update")
def trigger_update(
    trigger_id: str,
    name: str | None = None,
    task_template: str | None = None,
    description: str | None = None,
    owner: str | None = None,
    team: str | None = None,
    source_type: TriggerSourceArg | None = None,
    condition: dict[str, Any] | None = None,
    target_session_id: str | None = None,
    client_id: str | None = None,
    risk_level: TriggerRiskArg | None = None,
    enabled: bool | None = None,
    cooldown_seconds: int | None = None,
    max_concurrency: int | None = None,
    approval_policy: dict[str, Any] | None = None,
    metadata: dict[str, Any] | None = None,
) -> str:
    """更新触发器配置。用于调整触发条件、任务模板、风险等级或启用状态。"""

    patch = TriggerUpdateIn(
        **{
            key: value
            for key, value in {
                "name": name,
                "task_template": task_template,
                "description": description,
                "owner": owner,
                "team": team,
                "source_type": source_type,
                "condition": condition,
                "target_session_id": target_session_id,
                "client_id": client_id,
                "risk_level": risk_level,
                "enabled": enabled,
                "cooldown_seconds": cooldown_seconds,
                "max_concurrency": max_concurrency,
                "approval_policy": approval_policy,
                "metadata": metadata,
            }.items()
            if value is not None
        }
    )
    try:
        trigger = get_trigger_store().update_trigger(trigger_id.strip(), patch)
    except KeyError:
        return _json_text({"ok": False, "error": "trigger not found", "trigger_id": trigger_id})
    return _json_text({"ok": True, "trigger": trigger.model_dump()})


@tool("trigger_delete")
def trigger_delete(trigger_id: str) -> str:
    """删除触发器。用于移除不再需要的自主行动规则。"""

    deleted = get_trigger_store().delete_trigger(trigger_id.strip())
    return _json_text({"ok": True, "trigger_id": trigger_id, "deleted": deleted})


async def trigger_fire(
    trigger_id: str,
    reason: str = "agent_tool",
    payload: dict[str, Any] | None = None,
    force: bool = False,
    context: OpenAIConversationContext | None = None,
) -> str:
    """手动触发一个触发器，并将其任务投递到 AgentService 队列。不会绕过工具审批。"""

    del context
    scheduler = get_trigger_scheduler()
    if scheduler is None:
        return _json_text({"ok": False, "error": "trigger scheduler is not available"})
    try:
        record = await scheduler.fire_trigger(
            trigger_id.strip(),
            reason=reason,
            payload=payload or {},
            force=force,
        )
    except KeyError:
        return _json_text({"ok": False, "error": "trigger not found", "trigger_id": trigger_id})
    return _json_text({"ok": record.status == "queued", "record": record.model_dump()})


trigger_list.args_schema = TriggerListArgs  # type: ignore[attr-defined]
trigger_get.args_schema = TriggerGetArgs  # type: ignore[attr-defined]
trigger_create.args_schema = TriggerCreateArgs  # type: ignore[attr-defined]
trigger_update.args_schema = TriggerUpdateArgs  # type: ignore[attr-defined]
trigger_delete.args_schema = TriggerDeleteArgs  # type: ignore[attr-defined]
trigger_fire.name = "trigger_fire"  # type: ignore[attr-defined]
trigger_fire.description = "手动触发一个触发器，并将其任务投递到 AgentService 队列。不会绕过工具审批。"  # type: ignore[attr-defined]
trigger_fire.args_schema = TriggerFireArgs  # type: ignore[attr-defined]


def _json_text(payload: dict[str, Any]) -> str:
    return json.dumps(payload, ensure_ascii=False, sort_keys=True)
