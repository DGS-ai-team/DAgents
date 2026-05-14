# `app/harness/skills/` REFERENCE

## `skills.py`

- **`SkillDefinition`**：技能定义数据结构（**`skill_name`** + `description/enabled` + `SKILL.md` 正文）
- **`list_enabled_skills`**：在 **技能根**（**`skills_dir()`** → **`<运行根>/.runtime/skills`**）下扫描 **「一级子目录 / SKILL.md」**（即 `glob("*/SKILL.md")`），加载启用技能（含 mtime 缓存）
- **`list_enabled_skill_metadata`**：返回启用技能的元数据清单（`skill_name/description`）
- **`select_skill_by_name`**：按技能名（目录名）选择技能定义（命中返回单个技能，未命中返回 `None`）
- **`render_skill_metadata_prompt`**：将 skills 元数据清单渲染为 system prompt 常驻片段
- **`render_skills_prompt`**：把命中技能渲染为 system prompt 片段
- **`_parse_skill_frontmatter`**、**`_read_skill_markdown_cached`**：frontmatter 元数据与正文读取（带 mtime 缓存）
