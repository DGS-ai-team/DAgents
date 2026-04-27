# `skills/`

存放可被主 Agent 动态注入的技能包。

## 目录结构

- `skills/<skill_id>/SKILL.md`：标准技能文件（由元数据头 + 正文组成）
- `skills/<skill_id>/assets/`（可选）：技能引用的附加资源（图片、示例文件等）

## 约定

- `skill_id` 使用目录名作为技能唯一标识（`id`）；
- `SKILL.md` 必须使用 frontmatter 元数据头，格式示例：

```markdown
---
name: split-to-prs
description: 把当前一大坨改动拆分为可评审的小 PR。
enabled: true
---

这里是技能正文...
```

- `enabled=false` 的技能不会被注入；
- 默认按 `id` 升序稳定排序；
- 每轮只注入前 N 个技能（由 `AGENT_SKILLS_MAX_IN_PROMPT` 控制）。
