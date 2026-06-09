# node/internal/skills — 符号索引

## `skills.go`

| 符号 | 类型 | 说明 |
|------|------|------|
| `LoadedSkill` | struct | 会话已加载 skill 元数据（`skill_name` + `description`） |
| `Definition` | struct | 磁盘 skill 定义（目录名/`name`、description、正文） |
| `Catalog` | struct | 扫描 `{root}/*/SKILL.md`、loaded 集合、prompt 段渲染 |
| `NewCatalog` | func | 构造 Catalog（全局 `skills.enabled`、root、max_in_prompt） |
| `(c *Catalog) Enabled` | method | skills 功能总开关 |
| `(c *Catalog) List` | method | 扫描目录下全部 skill；按 SKILL.md mtime+size 签名缓存 |
| `(c *Catalog) ListMetadata` | method | 返回 `skill_name` / `description` 列表 |
| `(c *Catalog) SelectByName` | method | 按目录名或 frontmatter `name` 查找 |
| `(c *Catalog) RenderMetadataSection` | method | system prompt 可用技能段 |
| `(c *Catalog) RenderLoadedSection` | method | 已加载 skill 正文段 |
| `(c *Catalog) SetLoadedSkills` | method | 按名称整组替换 loaded（`load_skills` 语义） |
| `(c *Catalog) UnloadSkills` | method | 从 loaded 集合移除指定名称 |

## SKILL.md 约定

frontmatter 标准字段：**`name`** + **`description`**（单行 `key: value`）。目录下存在 `SKILL.md` 即纳入元数据扫描，无 per-skill `enabled` 字段。
