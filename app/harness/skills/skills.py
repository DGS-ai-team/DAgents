"""skills 加载、匹配与 prompt 渲染。"""

from __future__ import annotations

from pathlib import Path
from typing import Any

from app.config.runtime_layout import skills_dir
from pydantic import BaseModel, ConfigDict, Field

_skill_meta_cache: dict[str, tuple[dict[str, Any], float]] = {}
_skill_markdown_cache: dict[str, tuple[str, float]] = {}


class SkillDefinition(BaseModel):
    """技能定义（元信息 + markdown 内容）。

    逻辑：
    1. **`skill_name`** 仅来自技能目录名（与磁盘路径一致），作为唯一标识；
    2. **`description`/`enabled`** 来自 `SKILL.md` frontmatter（不再使用单独的展示名，避免与目录名重复）；
    3. **`content`** 来自正文段。

    关键边界：
    - `content` 允许为空，渲染阶段会过滤；
    - frontmatter 中的 **`name`** 字段若存在将被忽略（仍以目录名为准）。
    """

    model_config = ConfigDict(frozen=True)

    skill_name: str = Field(description="技能唯一名称（与技能根目录下 `<skill_name>/` 子目录名一致；默认根为 `<运行根>/.runtime/skills`）。")
    description: str = Field(default="", description="技能简要说明。")
    enabled: bool = Field(default=True, description="技能是否启用。")
    content: str = Field(default="", description="技能正文（来自 `SKILL.md` 正文段）。")


def _parse_skill_frontmatter(meta_text: str) -> dict[str, Any]:
    """解析 `SKILL.md` 头部元数据文本为字典。

    逻辑：
    1. 按行解析 `key: value` 结构；
    2. 仅取首个 `:` 作为分隔，保留 value 原始文本；
    3. 对布尔字符串做轻量转换（`true/false`）。

    关键边界：
    - 不支持复杂嵌套 YAML；
    - 无效行直接跳过，不抛异常。
    """
    out: dict[str, Any] = {}
    for line in meta_text.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if ":" not in stripped:
            continue
        key_raw, value_raw = stripped.split(":", 1)
        key = key_raw.strip()
        if not key:
            continue
        value_text = value_raw.strip()
        lowered = value_text.lower()
        if lowered == "true":
            value: Any = True
        elif lowered == "false":
            value = False
        else:
            value = value_text
        out[key] = value
    return out


def _read_skill_markdown_cached(path: Path) -> tuple[dict[str, Any], str]:
    """读取并缓存 `SKILL.md`（元数据头 + 正文）。

    逻辑：
    1. 文件不存在时返回空元数据与空正文；
    2. mtime 未变化时命中元数据与正文缓存；
    3. 读取全文后尝试解析 frontmatter（`---` 包裹）；
    4. 返回 `(meta, body)` 并更新缓存。

    关键边界：
    - 无 frontmatter 时元数据为空；
    - frontmatter 解析失败不抛异常，回退为空元数据。
    """

    if not path.is_file():
        return {}, ""
    try:
        mtime = path.stat().st_mtime
    except OSError:
        return {}, ""
    cache_key = str(path.resolve())
    cached_meta = _skill_meta_cache.get(cache_key)
    cached_body = _skill_markdown_cache.get(cache_key)
    if (
        cached_meta is not None
        and cached_body is not None
        and cached_meta[1] == mtime
        and cached_body[1] == mtime
    ):
        return cached_meta[0], cached_body[0]
    try:
        raw = path.read_text(encoding="utf-8")
    except OSError:
        return {}, ""
    text = raw.strip()
    meta: dict[str, Any] = {}
    body = text
    if text.startswith("---\n"):
        end_idx = text.find("\n---\n", 4)
        if end_idx != -1:
            meta_block = text[4:end_idx].strip()
            body = text[end_idx + 5 :].strip()
            meta = _parse_skill_frontmatter(meta_block)
    _skill_meta_cache[cache_key] = (meta, mtime)
    _skill_markdown_cache[cache_key] = (body, mtime)
    return meta, body


def list_enabled_skills() -> list[SkillDefinition]:
    """枚举并返回启用的 skills。

    逻辑：
    1. 扫描 **`<技能根>/*/SKILL.md`**（技能根为 **`skills_dir()`** → **`<运行根>/.runtime/skills`**）；
    2. 读取并解析文件头部元数据（frontmatter）；
    3. 读取同文件正文；
    4. 过滤 `enabled=False` 与目录名为空的项；
    5. 按 `skill_name ASC` 排序。

    关键边界：
    - 目录不存在、文件损坏均按空列表处理；
    - 未提供 `description` 时为空串。
    """

    skills_root = skills_dir()
    if not skills_root.is_dir():
        return []
    out: list[SkillDefinition] = []
    for skill_md_path in skills_root.glob("*/SKILL.md"):
        meta, content = _read_skill_markdown_cached(skill_md_path)
        skill_name = str(skill_md_path.parent.name).strip()
        if not skill_name:
            continue
        description = str(meta.get("description") or "").strip()
        enabled_raw = meta.get("enabled", True)
        enabled = bool(enabled_raw)
        if enabled:
            out.append(
                SkillDefinition(
                    skill_name=skill_name,
                    description=description,
                    enabled=True,
                    content=content,
                )
            )
        else:
            continue
    out.sort(key=lambda item: item.skill_name)
    return out


def list_enabled_skill_metadata() -> list[dict[str, str]]:
    """返回启用技能的元数据列表（`skill_name/description`）。

    逻辑：
    1. 调用 `list_enabled_skills` 获取技能定义；
    2. 仅提取 `skill_name/description`；
    3. 保持排序顺序，供 system prompt 常驻展示。

    关键边界：
    - 无启用技能时返回空列表；
    - 不暴露 `enabled/content` 等内部字段。
    """

    return [
        {
            "skill_name": skill.skill_name,
            "description": skill.description,
        }
        for skill in list_enabled_skills()
    ]


def select_skill_by_name(skill_name: str) -> SkillDefinition | None:
    """按单个技能名（目录名）返回技能定义。

    逻辑：
    1. 读取启用技能并构建 `skill_name -> SkillDefinition` 索引；
    2. 用传入 `skill_name` 命中索引；
    3. 命中返回单个技能，未命中返回 `None`。

    关键边界：
    - `skill_name` 为空白时直接返回 `None`；
    - 仅返回启用技能，不会返回禁用技能。
    """

    final_name = str(skill_name or "").strip()
    if not final_name:
        return None
    mapping = {skill.skill_name: skill for skill in list_enabled_skills()}
    skill = mapping.get(final_name)
    if skill is not None:
        return skill
    return None


def render_skill_metadata_prompt(skill_meta_list: list[dict[str, str]]) -> str:
    """渲染 skills 元数据清单到 system prompt。

    逻辑：
    1. 遍历元数据并提取 `skill_name/description`；
    2. 按固定格式输出列表，保证模型可稳定解析；
    3. 无可用条目时返回空串。

    关键边界：
    - 缺失 `skill_name` 的条目直接跳过；
    - `description` 允许为空。
    """

    lines: list[str] = []
    for item in skill_meta_list:
        skill_name = str(item.get("skill_name") or "").strip()
        if not skill_name:
            continue
        description = str(item.get("description", "") or "").strip()
        lines.append(f"- skill_name={skill_name}; description={description}")
    if lines:
        return "## 以下是可用 skills 元数据清单（常驻）\n" + "\n".join(lines)
    else:
        return ""


def render_skills_prompt(skills: list[SkillDefinition]) -> str:
    """将选中的 skills 渲染为 system prompt 片段。

    逻辑：
    1. 过滤无正文内容的 skill；
    2. 每个 skill 以固定标题包装；
    3. 拼接成单段文本返回，供 `get_system_prompt` 追加。

    关键边界：
    - 传入空列表或正文全空时返回空串；
    - 不做 markdown 语法校验，按原文透传。
    """

    blocks: list[str] = []
    for skill in skills:
        body = str(skill.content or "").strip()
        if not body:
            continue
        blocks.append(f"### 技能：{skill.skill_name}\n{body}")
    if blocks:
        return "## 以下是可用技能（按本轮相关性筛选）\n\n" + "\n\n".join(blocks)
    else:
        return ""
