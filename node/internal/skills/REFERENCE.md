# skills 模块参考

## skills.go

| 符号 | 类型 | 说明 |
|------|------|------|
| `LoadedSkill` | struct | 已加载 skill 元信息（`skill_name`、`description`） |
| `Definition` | struct | 磁盘 skill 完整定义 |
| `Catalog` | struct | skill 目录访问与 prompt 渲染 |
| `NewCatalog` | func | 构造 Catalog |
| `(c *Catalog) Enabled` | method | skills 功能是否开启 |
| `(c *Catalog) ListEnabled` | method | 扫描并返回启用的 skill 定义 |
| `(c *Catalog) ListMetadata` | method | 返回可用 skill 元数据列表 |
| `(c *Catalog) SelectByName` | method | 按名称查找 skill |
| `(c *Catalog) RenderPromptSection` | method | 将 loaded skills 渲染为 prompt 段 |
| `(c *Catalog) SetLoadedSkills` | method | 按名称整组替换 loaded（`load_skills` 语义） |
| `(c *Catalog) UnloadSkills` | method | 从 loaded 集合移除指定名称 |
