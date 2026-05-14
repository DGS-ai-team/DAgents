---
description: 指导在运行根下的技能目录中编写符合约定的 Agent 技能（SKILL.md 结构、元数据、加载方式）；在用户新增技能、改技能目录或询问技能格式时使用。
enabled: true
---


## 技能根目录（务必与实现对齐）

- 技能文件由 **`app.harness.skills.skills._resolve_skills_dir()`** 解析：取配置 **`AGENT_SKILLS_DIR`**（环境变量），未配置时默认为 **`.runtime/skills`**，且为**相对 `<运行根>`** 的路径（**`resolve_runtime_root()`** 与源码根 / 可执行文件目录一致，见 **`app/config/env.py`**）。
- **不要**把技能写在仓库根下裸的 **`skills/`** 目录（除非你把 **`AGENT_SKILLS_DIR`** 显式设成该路径）；默认情况下模型与 **`load_skills`** 只会扫描 **`<运行根>/.runtime/skills/`**。

下文在默认配置下，**「技能根」**均指：

```text
<运行根>/.runtime/skills/
```

若部署修改了 **`AGENT_SKILLS_DIR`**，则将下文中 **`.runtime/skills`** 替换为你的配置路径（绝对路径则直接使用该根）。

## 目录与标识

1. **skill_name**：与**技能根下**一级子目录名一致，为唯一标识；仅使用小写字母、数字、连字符（如 `write-skill`、`my-task`）。
2. **文件路径（默认配置）**：**`<运行根>/.runtime/skills/<skill_name>/SKILL.md`**（必选）；可选 **`<运行根>/.runtime/skills/<skill_name>/assets/`** 存放示例、图片、脚本等。

## SKILL.md 头部元数据（frontmatter）

- 文件必须以 `---` 包裹的简单键值头开始（**单行 `key: value`**）；解析器不支持复杂 YAML（多行折叠、嵌套列表等）。
- 常用字段：
  - **`description`**：一行摘要（模型用它做「何时加载」判断）；建议写清 **做什么** + **何时用**，第三人称、勿空话。
  - **`enabled`**：`true` / `false`；为 `false` 时不会出现在可用列表，也无法被 `load_skills` 选中。草稿阶段可设为 `false`，确认后再改为 `true`。
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
3. **复用性**：可将复杂步骤落成脚本，放在 **`<运行根>/.runtime/skills/<skill_name>/assets/`** 下；正文中写明调用方式（相对技能根或绝对路径须与运行环境一致）。

## 完成后自检

- 目录名（skill_name）稳定且与文档描述一致。
- frontmatter 可被简单 `key: value` 解析（无多行 YAML）。
- 逻辑清晰，无前后文冲突；路径描述与 **`AGENT_SKILLS_DIR`** / **`<运行根>`** 一致。

## 启用 skill

- 在确认没有问题后，将 **`enabled`** 置为 **`true`**，以启用该技能。
