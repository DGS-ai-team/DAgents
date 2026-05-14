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
    """使用场景：希望加载技能时调用。

    字段说明：
    - skill_names: 技能名称数组；每项须与 **技能根** **`<运行根>/.runtime/skills`**（**`runtime_layout.skills_dir()`**，不由环境变量覆盖）下 **`<skill_name>/`** 目录名一致。

    返回说明：
    - 成功：返回 JSON 字符串，包含 `loaded_skills`（本次加载结果）与 `available_skills`（全部启用技能元数据）。
    - 失败：返回 `ERROR: ...`（当前实现无主动异常分支，解析失败场景会返回空结果 JSON）。

    调用范例：
    - `load_skills({"skill_names":["write-skill"]})`
    """

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
        "loaded_skills": loaded,
        "available_skills": list_enabled_skill_metadata(),
    }
    return json.dumps(payload, ensure_ascii=False)
