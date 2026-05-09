# `skills/`

存放可被主 Agent 动态注入的技能包。

## 内置技能（示例）

| skill_name | 说明 |
|------------|------|
| **`write-skill`** | 编写/评审本目录下新技能的约定与自检清单（推荐需要扩展 skills 时先 `load_skills` 加载） |

## 目录结构

- `skills/<skill_name>/SKILL.md`：标准技能文件（由元数据头 + 正文组成）
- `skills/<skill_name>/assets/`（可选）：技能引用的附加资源（图片、示例文件等）

## 约定

- **`skill_name`**：目录名即技能唯一名称；加载工具参数 **`skill_names`** 与之对应。
- `SKILL.md` 必须使用 frontmatter 元数据头，格式示例：

```markdown
---
description: 简述能力与触发场景（单行）。
enabled: true
---

这里是技能正文...
```

- frontmatter 中的 **`name`** 不参与加载（可省略）；请勿用其与目录名重复维护第二套标识。
- `enabled=false` 的技能不会被注入；
- 默认按 **`skill_name`**（目录名）升序稳定排序；
- 每轮只注入前 N 个技能（由 `AGENT_SKILLS_MAX_IN_PROMPT` 控制）。
