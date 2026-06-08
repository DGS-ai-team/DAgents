# node/internal/skills

Go Node 侧 skills 目录扫描、元数据与 prompt 渲染（对齐 Python `harness/skills`）。

| 文件 | 用途 |
|------|------|
| `skills.go` | `Catalog`：扫描 `{root}/*/SKILL.md`、loaded 集合管理、渲染 system prompt 段 |

## SKILL.md 格式

```markdown
---
name: my-skill
description: 单行摘要（做什么 + 何时用）
---

正文…
```

- **`name`** 须与目录名一致；**`description`** 为模型选择依据。
- 目录下所有含 `SKILL.md` 的子目录均参与元数据扫描（无 per-skill `enabled`）。

配置见 `shared/config.Config.Skills`（全局 `enabled`、`root`、`max_in_prompt`）。
