---
name: write-skill
description: 指导在技能目录中编写符合约定的 Agent 技能（SKILL.md 结构、元数据、加载方式）；在用户新增技能、改技能目录或询问技能格式时使用。
---

## 目录与标识

1. **name**：与**技能目录名**一致，为唯一标识；仅使用小写字母、数字、连字符（如 `write-skill`、`my-task`）。**若 frontmatter `name` 与目录名不一致，`load_skills` 可能无法按目录名加载**——务必保持二者相同。
2. **文件路径（`{runtime_root}/skills/` 下，常见为 `.runtime/skills/`；该目录属于 Node 运行目录，不是 Agent workspace）**：
   - **`skills/<name>/SKILL.md`**（必选）
   - **`skills/<name>/assets/`**：示例、图片等静态资源
   - **`skills/<name>/scripts/`**：可复用脚本（目录名自定亦可，正文须写清路径）
   - **`skills/<name>/data/`**：任务需持续保留的数据（历史记录、持久化产物等）
   - **`skills/<name>/config/`**：配置类文件（见下文「config 与安全」）
   - **`skills/<name>/hooks/`**：可选，存放本 skill 的 Hook plugin（`*.so`）；编写约定见 **`write-hook`** skill
   - **`write-skill` 内置防护**：Node 自带 `builtin.loaded_skill_file_guard`（`tool.before_each`），当会话 `loaded_skills` 非空时，禁止 `write_file` / `search_replace` / 会改文件的 `bash_run` 触及 `skills/<已加载名>/` 下任意路径；返回「已加载的技能文件不允许修改」。**默认无需再加载插件**；仅在需要 skill 级独立 `.so` 定制时，可编译 `hooks/protect-loaded-skill/`（与内置同逻辑的示例）。

## SKILL.md 头部元数据（frontmatter）

- 文件必须以 `---` 包裹的简单键值头开始（**单行 `key: value`**）；解析器不支持复杂 YAML（多行折叠、嵌套列表等）。
- **仅 `name` 与 `description` 会被系统读取**；其它键（如 `version`、`author`）会被忽略，勿依赖。
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

### 最小 skill 骨架

```markdown
---
name: my-task
description: 单行：做什么 + 何时用。
---

## 适用场景
（何时启用本 skill）

## 流程
1. …
2. …

## 禁止
- …

## 输出格式
- …
```

## 正文编写要点

1. **步骤化**：用有序列表写清流程与分支（与用户确认 / 禁止的操作 / 输出格式）。
2. **安全**：技能正文勿包含密钥；示例命令避免默认破坏性 Git/磁盘操作。
3. **复用性**：将通用步骤落成脚本；正文中写明调用方式（相对 `skills/<name>/` 的路径）。
4. **结构化**：分支或特殊场景可另起文件（建议 `skills/<name>/reference/<topic>.md`），正文用相对路径引用；**正文只保留最高频、最通用的内容**。
5. **篇幅**：SKILL.md 正文宜精炼。细节、长示例放入 `assets/` 或 `reference/`；单文件过长会抬高 token 与 prompt 成本。

## config 与安全

- **`config/` 可存本地配置**（账号、环境变量、密钥路径等），但须遵守：
  - **勿将 `config/` 提交 Git**（加入 `.gitignore` 或仅保留本地）。
  - **SKILL.md 正文只写读取方式**（如「从 `skills/<name>/config/local.env` 读取」），**不写明文密钥**；示例用占位符。
  - 脚本从 `config/` 读敏感项，勿在 tool 输出或正文中回显。

## 编写过程要点

- 加载本 skill 后，可视为进入「编写 skill」流程；注意：
  - 在 skill **定稿前勿加载正在编写的 skill**；**修改 SKILL.md 前先 `unload_skills` 卸载该 skill**（或 `clear_skills`），避免旧正文残留在会话、且频繁改动导致上下文缓存失效。
  - 定稿后可用 **`load_skills` 加载验证**，并用 **`/context`** 或 **`/skill load <name>`** 确认 `loaded_skills` 与后续 Step 的独立 skill ContextInjection 正确。
  - 若在用户引导下边做任务边写 skill，**每完成一阶段须先落地步骤或转为 `scripts/` 脚本**。
  - 步骤若多次试错，**成功后须记录可行做法**（写入正文或 `reference/`）。
  - **重要步骤与成果须及时写入磁盘**，避免上下文压缩后丢失。

## 完成后自检

- 目录名与 frontmatter **`name`** 一致（不一致会导致加载失败或名称混乱）。
- **`name`** 与 **`description`** 均已填写；frontmatter 可被简单 `key: value` 解析（无多行 YAML）。
- 无多余 frontmatter 字段被误当作生效配置。
- 逻辑清晰，无前后文冲突；路径描述与 **`skills/<name>/`** 一致。
- 无密钥进正文；`config/` 已忽略或仅本地存在。
- **`load_skills` 验证通过**：`/context` 中可见对应 skill 正文段。
