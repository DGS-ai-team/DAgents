---
description: 指导在`skills/` 下编写符合约定的内置 Agent 技能（SKILL.md 结构、元数据、加载方式）；在用户新增技能、改技能目录或询问技能格式时使用。
enabled: true
---


## 目录与标识

1. **skill_name**：与目录名一致，为唯一标识；仅使用小写字母、数字、连字符（如 `write-skill`、`my-task`）。
2. **文件路径**：`skills/<skill_name>/SKILL.md`（必选）；可选 `skills/<skill_name>/assets/` 存放示例、图片等。

## SKILL.md 头部元数据（frontmatter）

- 文件必须以 `---` 包裹的简单键值头开始（**单行 `key: value`**）；解析器不支持复杂 YAML（多行折叠、嵌套列表等）。
- 常用字段：
  - **`description`**：一行摘要（模型用它做「何时加载」判断）；建议写清 **做什么** + **何时用**，第三人称、勿空话。
  - **`enabled`**：`true` / `false`；为 `false` 时不会出现在可用列表，也无法被 `load_skills` 选中。初始值请设置为false。
- **`name` 字段会被忽略**：展示与匹配均以 **目录名 `skill_name`** 为准，请勿再维护重复名称。

示例：

```markdown
---
description: 简述能力与触发场景（单行）。
enabled: true
---

正文从这里开始…
```

## 正文编写要点

1. **步骤化**：用有序列表写清流程与分支（与用户确认 / 禁止的操作 / 输出格式）。
2. **安全**：技能正文勿包含密钥；示例命令避免默认破坏性 Git/磁盘操作。
3. **复用性**: 可以将复杂的步骤序列落地成python、shell等脚本，存放到`skills/<skill_name>/assets/`中，步骤中注明相关步骤可以通过调用特定脚本完成。

## 完成后自检

- 目录名（skill_name）稳定且与文档描述一致。
- frontmatter 可被简单 `key: value` 解析（无多行 YAML）。
- 是否逻辑清晰，没有前后文冲突

## 启用skill
- 在确认没有问题后，将 **`enabled`** 置为`true`，以启用该技能。

