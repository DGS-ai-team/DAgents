"""skills 工具：供模型显式加载会话技能。"""

from __future__ import annotations

import json

from app.config.settings import get_settings
from app.context.models import OpenAIConversationContext
from app.harness.skills.skills import (
    list_enabled_skill_metadata,
    select_skill_by_name,
)
from app.harness.tools.tool import tool


@tool("load_skills")
def load_skills(skill_names: list[str], context: OpenAIConversationContext) -> str:
    """使用场景：希望设置当前会话技能集合时调用；本工具是“整组替换”语义，传空数组表示清空。

    字段说明：
    - skill_names: 技能名称数组；每项须与 **技能根** **`<运行根>/.runtime/skills`** 下 **`<skill_name>/`** 目录名一致。

    返回说明：
    - 成功：返回 JSON 字符串，包含 `action=set_loaded_skills`、`loaded_skills` 与 `available_skills`。
    - 失败：返回 `ERROR: ...`（当前实现无主动异常分支，解析失败场景会返回空结果 JSON）。

    调用范例：
    - `load_skills({"skill_names":["write-skill"]})`
    - `load_skills({"skill_names":[]})`
    """

    if not bool(get_settings().agent_skills_enabled):
        return "ERROR: skills 功能已禁用（AGENT_SKILLS_ENABLED=false）。"
    max_skills = max(0, int(get_settings().agent_skills_max_in_prompt))
    selected = []
    seen: set[str] = set()
    for raw in skill_names:
        item = str(raw).strip()
        if not item:
            continue
        if item in seen:
            continue
        seen.add(item)
        skill = select_skill_by_name(item)
        if skill is None:
            continue
        selected.append(skill)
        if len(selected) >= max_skills:
            break
    loaded = [
        {"skill_name": item.skill_name, "description": item.description}
        for item in selected
    ]
    if isinstance(context, OpenAIConversationContext):
        context.loaded_skills = list(loaded)
    payload = {
        "action": "set_loaded_skills",
        "loaded_skills": loaded,
        "available_skills": list_enabled_skill_metadata(),
    }
    return json.dumps(payload, ensure_ascii=False)


@tool("unload_skills")
def unload_skills(skill_names: list[str], context: OpenAIConversationContext) -> str:
    """使用场景：希望从当前会话已加载技能中移除指定技能时调用；不影响磁盘上的 skill 文件。

    字段说明：
    - skill_names: 要卸载的技能名称数组；不存在或未加载的名称会被忽略。

    返回说明：
    - 成功：返回 JSON 字符串，包含 `action=unload_skills`、`loaded_skills` 与 `available_skills`。
    - 失败：skills 功能禁用时返回 `ERROR: ...`。

    调用范例：
    - `unload_skills({"skill_names":["write-skill"]})`
    - `unload_skills({"skill_names":["missing"]})`
    """
    if not bool(get_settings().agent_skills_enabled):
        return "ERROR: skills 功能已禁用（AGENT_SKILLS_ENABLED=false）。"
    names_to_remove = {str(item).strip() for item in skill_names if str(item).strip()}
    current = list(context.loaded_skills) if isinstance(context, OpenAIConversationContext) else []
    remaining = []
    for item in current:
        if not isinstance(item, dict):
            continue
        skill_name = str(item.get("skill_name") or "").strip()
        if not skill_name or skill_name in names_to_remove:
            continue
        remaining.append({"skill_name": skill_name, "description": str(item.get("description") or "")})
    if isinstance(context, OpenAIConversationContext):
        context.loaded_skills = list(remaining)
    payload = {
        "action": "unload_skills",
        "loaded_skills": remaining,
        "available_skills": list_enabled_skill_metadata(),
    }
    return json.dumps(payload, ensure_ascii=False)


@tool("clear_skills")
def clear_skills(context: OpenAIConversationContext) -> str:
    """使用场景：希望清空当前会话已加载技能时调用；不删除磁盘 skill。

    字段说明：
    - 无业务字段；`context` 由运行时注入。

    返回说明：
    - 成功：返回 JSON 字符串，包含 `action=clear_skills`、空 `loaded_skills` 与 `available_skills`。
    - 失败：skills 功能禁用时返回 `ERROR: ...`。

    调用范例：
    - `clear_skills({})`
    """
    if not bool(get_settings().agent_skills_enabled):
        return "ERROR: skills 功能已禁用（AGENT_SKILLS_ENABLED=false）。"
    if isinstance(context, OpenAIConversationContext):
        context.loaded_skills = []
    payload = {
        "action": "clear_skills",
        "loaded_skills": [],
        "available_skills": list_enabled_skill_metadata(),
    }
    return json.dumps(payload, ensure_ascii=False)
