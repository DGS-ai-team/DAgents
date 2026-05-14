# `.runtime/skills/`（发布包内）

随预编译包放入 **`bundle/.runtime/skills/`**；与代码包 **`app/harness/skills/`**（**`load_skills`** 实现）不同。

技能根目录由 **`AGENT_SKILLS_DIR`** 决定，默认 **`<运行根>/.runtime/skills`**（相对 **`resolve_runtime_root()`**）。

## 初始随包技能

| 目录名 | 说明 |
|--------|------|
| **`write-skill/`** | 如何编写 **`SKILL.md`**、路径与 **`load_skills`** 的配合；见 **`write-skill/SKILL.md`**。 |

其它技能：在 **`<运行根>/.runtime/skills/<skill_name>/SKILL.md`** 下按需增删（默认配置下；若改了 **`AGENT_SKILLS_DIR`** 则以配置为准）。
