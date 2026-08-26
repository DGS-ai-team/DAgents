# node/internal/skills

Go Node 侧 skills 目录扫描、元数据与 prompt 渲染（对齐 Python `harness/skills`）。

| 文件 | 用途 |
|------|------|
| `skills.go` | `Catalog`：扫描 `{root}/*/SKILL.md`、loaded 集合管理、提供 metadata/body 读取 |

## Catalog 列表缓存

`List()` / `ListMetadata()` / `SelectByName()` 共用目录元数据缓存，正文另有按 skill 的惰性缓存：

- **签名**：各子目录 `SKILL.md` 的目录名 + `mtime` + `size`（排序后拼接）；
- **失效**：新增/删除 skill 目录、修改 `SKILL.md` 后自动重扫并清空正文缓存；未变时只检查 `Stat`，不读取所有正文；
- **并发**：`sync.RWMutex` 保护；每 session runtime 持有一个 `Catalog` 实例。
- **别名去重**：同一 skill 的 frontmatter `name` 和目录名都可以作为请求名，但解析后按目录名去重，不会重复占用 `max_in_prompt` 或重复注册 hooks。
- **同名保护**：多个目录声明相同 frontmatter `name` 时，目录元数据显示目录名；按逻辑名请求会返回 `ambiguous`，不会按文件系统顺序静默选择。

每个 human Turn 开始时，runtime 从 live Catalog 创建 `NewTurnView()`。该 view 固定
metadata 和每个 `SKILL.md` 的边界摘要；活动 Turn 内的 `load_skills`、正文渲染和 skill
hooks 使用同一 view。正文仍然按需读取，但首次读取会校验边界摘要；如果外部文件已变化，
加载返回 `catalog_changed`，不会把新正文静默混入活动上下文。下一次 human Turn 才创建新 view。

边界摘要只在 human Turn 边界计算，不在每个模型 Step 重算；它用于弥补 mtime/size
签名对“内容变化但文件属性未变化”的检测盲区。

启用 skills 工具组时，可用 catalog 元数据追加到 system prompt；SKILL.md 正文不再拼入 system prompt。模型首次使用已加载 skill 时，turn 编排器按 Codex 式语义将正文包装为独立的 `role=user`、`source=plugin`、`form=instructions` 上下文消息写入 session history，并保留 `name=skill` 作为兼容字段；后续请求中只保留当前激活版本。tool loop 中 `load_skills` 使用详细加载结果并按需读取正文。

Catalog 还累计记录 metadata scan、正文读取/缓存命中、human-Turn 边界摘要和 token estimate 的耗时，供
`GET /v1/agents/{id}/context` 的 `skills_catalog_timing` 诊断字段使用。该字段不进入模型 prompt、工具定义、revision
或 cache key。`EstimateCatalogStats` 只由 context 诊断路径调用，不在普通模型 Step 的 system prompt 构建路径中调用；如果
未来发现 UI 高频轮询造成正文估算开销，应优先对 token stats 按 Catalog revision 做缓存，而不是把统计结果塞进模型上下文。

## SKILL.md 格式

```markdown
---
name: my-skill
description: 单行摘要（做什么 + 何时用）
---

正文…
```

- **`name`** 推荐与目录名一致；运行时同时保存逻辑 name 和目录名，保证正文与 hooks 路径不因 frontmatter 差异而失效。**`description`** 为模型选择依据。
- 目录下所有含 `SKILL.md` 的子目录均参与元数据扫描（无 per-skill `enabled`）。
- 可选 **`hooks/`** 子目录：`load_skills` 时 `plugin.Open` 其中 `*.so`（导出 `Register`）；`unload_skills` / `clear_skills` 时按已记录的目录名移除。

`load_skills` 会立即更新 session 状态和 hooks；正在进行的模型请求保持不变，但显式变更会在下一个模型 Step 创建新的 context segment，并追加独立的 skill 正文消息。卸载或正文版本变化不会改写旧 history；出站模型请求只保留当前激活且 digest 匹配的正文，避免旧版本继续影响模型。活动 Turn 中若发现正文边界摘要变化，会返回 `catalog_changed`；普通磁盘变化在下一次 human Turn 创建新 view。工具结果会同时返回请求、成功、拒绝和两个生效边界。

配置见 `shared/config.Config.Skills`（全局 `enabled`、`root`、`max_in_prompt`）。
