"""触发器 Agent 工具：经 `runtime.get_trigger_*` 读写 JSON 存储并可选投递调度器。"""

from __future__ import annotations

import json
from typing import Any

from pydantic import BaseModel, ConfigDict, Field

from app.context.models import OpenAIConversationContext
from app.harness.tools.tool import tool
from app.harness.triggers.models import TriggerCreateIn, TriggerUpdateIn
from app.harness.triggers.runtime import get_trigger_scheduler, get_trigger_store


class TriggerListArgs(BaseModel):
    """`trigger_list` 入参 schema。"""

    model_config = ConfigDict(extra="forbid")

    include_disabled: bool = True


class TriggerGetArgs(BaseModel):
    """`trigger_get` 入参 schema。"""

    model_config = ConfigDict(extra="forbid")

    trigger_id: str = Field(min_length=1)


class TriggerCreateArgs(BaseModel):
    """`trigger_create` 入参 schema（`context` 由运行时注入，不在 schema 中）。"""

    model_config = ConfigDict(extra="forbid")

    name: str = Field(min_length=1)
    task_template: str = Field(min_length=1)
    condition: dict[str, Any]


class TriggerUpdateArgs(BaseModel):
    """`trigger_update` 入参 schema；未传字段表示不修改。"""

    model_config = ConfigDict(extra="forbid")

    trigger_id: str = Field(min_length=1)
    name: str | None = None
    task_template: str | None = None
    condition: dict[str, Any] | None = None


class TriggerDeleteArgs(BaseModel):
    """`trigger_delete` 入参 schema。"""

    model_config = ConfigDict(extra="forbid")

    trigger_id: str = Field(min_length=1)


class TriggerFireArgs(BaseModel):
    """`trigger_fire` 入参 schema。"""

    model_config = ConfigDict(extra="forbid")

    trigger_id: str = Field(min_length=1)
    reason: str = "agent_tool"
    payload: dict[str, Any] = Field(default_factory=dict)
    force: bool = False


def _session_client_from_context(
    context: OpenAIConversationContext | None,
) -> tuple[str | None, str | None]:
    """从推理上下文解析投递目标 session / client（供 trigger_create 绑定当前会话）。"""
    if not isinstance(context, OpenAIConversationContext):
        return None, None
    session_id = (context.session_id or "").strip() or None
    client_id = (context.sse_client_id or context.active_client_id or "").strip() or None
    return session_id, client_id


@tool("trigger_list")
def trigger_list(include_disabled: bool = True) -> str:
    """使用场景：查看已配置的触发器列表；只读，不会执行或投递任务。

    字段说明：
    - include_disabled: 是否包含 `enabled=false` 的项（默认 true）。
    """
    triggers = get_trigger_store().list_triggers()
    if not include_disabled:
        triggers = [item for item in triggers if item.enabled]
    return _json_text({"ok": True, "triggers": [item.model_dump() for item in triggers]})


@tool("trigger_get")
def trigger_get(trigger_id: str) -> str:
    """使用场景：查看单个触发器配置与 `next_fire_at`；不执行触发。

    字段说明：
    - trigger_id: 触发器 ID（必填）。

    """
    trigger = get_trigger_store().get_trigger(trigger_id.strip())
    if trigger is None:
        return _json_text({"ok": False, "error": "trigger not found", "trigger_id": trigger_id})
    return _json_text({"ok": True, "trigger": trigger.model_dump()})


@tool("trigger_create")
def trigger_create(
    name: str,
    task_template: str,
    condition: dict[str, Any],
    context: OpenAIConversationContext | None = None,
) -> str:
    """使用场景：新建触发器；须提供非空 `condition` 并由调度器按其中键执行。

    字段说明：
    - name、task_template、condition: 必填。
    - condition: `{"interval_seconds": N}` 周期，或 `{"fire_at": unix秒}` 单次；不可为 `{}`。

    注意：task_template必须带上必要的上下文，防止触发时需要再次向用户询问，或者造成执行偏差。

    """
    target_session_id, client_id = _session_client_from_context(context)
    body = TriggerCreateIn(
        name=name,
        task_template=task_template,
        condition=condition,
        target_session_id=target_session_id,
        client_id=client_id,
    )
    trigger = get_trigger_store().create_trigger(body.to_definition())
    return _json_text({"ok": True, "trigger": trigger.model_dump()})


@tool("trigger_update")
def trigger_update(
    trigger_id: str,
    name: str | None = None,
    task_template: str | None = None,
    condition: dict[str, Any] | None = None,
) -> str:
    """使用场景：修改已有触发器的名称、模板或 condition；未传字段保持不变。

    字段说明：
    - trigger_id: 必填。
    - name / task_template / condition: 可选局部更新。

    """
    patch = TriggerUpdateIn(
        **{
            key: value
            for key, value in {
                "name": name,
                "task_template": task_template,
                "condition": condition,
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
    """使用场景：删除不再需要的触发器规则。

    字段说明：
    - trigger_id: 必填。

    """
    deleted = get_trigger_store().delete_trigger(trigger_id.strip())
    return _json_text({"ok": True, "trigger_id": trigger_id, "deleted": deleted})


async def trigger_fire(
    trigger_id: str,
    reason: str = "agent_tool",
    payload: dict[str, Any] | None = None,
    force: bool = False,
    context: OpenAIConversationContext | None = None,
) -> str:
    """使用场景：立即执行一次触发器，将渲染后的任务投递到 Agent 队列。

    字段说明：
    - trigger_id: 必填。
    - reason、payload、force: 同 HTTP fire API。
    - context: 运行时注入，本工具当前不使用（投递目标以资源内绑定为准）。

    返回说明：
    - 成功且投递：`ok=true`，`record.status=queued`。
    - 调度器未启动：`error=trigger scheduler is not available`。

    调用范例：
    - `trigger_fire({"trigger_id":"<uuid>"})`
    """
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
trigger_fire.description = trigger_fire.__doc__ or ""  # type: ignore[attr-defined]
trigger_fire.args_schema = TriggerFireArgs  # type: ignore[attr-defined]


def _json_text(payload: dict[str, Any]) -> str:
    """将工具结果序列化为排序后的 JSON 字符串。"""
    return json.dumps(payload, ensure_ascii=False, sort_keys=True)
