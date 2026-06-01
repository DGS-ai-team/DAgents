# node/internal/skills

Go Node 侧 skills 目录扫描、元数据与 prompt 渲染（对齐 Python `harness/skills`）。

| 文件 | 用途 |
|------|------|
| `skills.go` | `Catalog`：扫描 `{root}/*/SKILL.md`、loaded 集合管理、渲染 system prompt 段 |

配置见 `shared/config.Config.Skills`（`enabled`、`root`、`max_in_prompt`）。
