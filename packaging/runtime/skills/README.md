# `.runtime/skills/`（发布包内）

随预编译包放入 **`bundle/.runtime/skills/`**；与代码包 **`app/harness/skills/`**（**`load_skills`** 实现）不同。

技能根目录固定为 **`<运行根>/.runtime/skills`**（**`runtime_layout.skills_dir()`**）。

## 初始随包技能

| 目录名 | 说明 |
|--------|------|
| **`write-skill/`** | 如何编写 **`SKILL.md`**、路径与 **`load_skills`** 的配合；见 **`write-skill/SKILL.md`**。 |

其它技能：在 **`<运行根>/.runtime/skills/<skill_name>/SKILL.md`** 下按需增删。
