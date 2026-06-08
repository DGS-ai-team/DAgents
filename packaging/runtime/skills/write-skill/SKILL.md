---
name: write-skill
description: 指导在技能目录中编写符合约定的 Agent 技能（SKILL.md 结构、元数据、加载方式）；在用户新增技能、改技能目录或询问技能格式时使用。
---

## 目录与标识

1. **name**：与**技能目录名**一致，为唯一标识；仅使用小写字母、数字、连字符（如 `write-skill`、`my-task`）。
2. **文件路径（默认配置）**：**`.runtime/skills/<name>/SKILL.md`**（必选）；可选 **`.runtime/skills/<name>/assets/`** 存放示例、图片、脚本等。

## SKILL.md 头部元数据（frontmatter）

- 文件必须以 `---` 包裹的简单键值头开始（**单行 `key: value`**）；解析器不支持复杂 YAML（多行折叠、嵌套列表等）。
- **必填字段**：
  - **`name`**：技能标识，须与目录名一致。
  - **`description`**：一行摘要（模型用它做「何时加载」判断）；建议写清 **做什么** + **何时用**，第三人称、勿空话。

示例：

```markdown
---
name: my-task
description: 简述能力与触发场景（单行）。
---

正文从这里开始…
```

## 正文编写要点

1. **步骤化**：用有序列表写清流程与分支（与用户确认 / 禁止的操作 / 输出格式）。
2. **安全**：技能正文勿包含密钥；示例命令避免默认破坏性 Git/磁盘操作。
3. **复用性**：可将复杂步骤落成脚本，放在 **`.runtime/skills/<name>/assets/`** 下；正文中写明调用方式（相对技能根或绝对路径须与运行环境一致）。

## 完成后自检

- 目录名与 frontmatter **`name`** 一致。
- **`name`** 与 **`description`** 均已填写；frontmatter 可被简单 `key: value` 解析（无多行 YAML）。
- 逻辑清晰，无前后文冲突；路径描述与 **`.runtime/skills`** 一致。
