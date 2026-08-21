# node/internal/skills

Go Node 侧 skills 目录扫描、元数据与 prompt 渲染（对齐 Python `harness/skills`）。

| 文件 | 用途 |
|------|------|
| `skills.go` | `Catalog`：扫描 `{root}/*/SKILL.md`、loaded 集合管理、渲染 system prompt 段 |

## Catalog 列表缓存

`List()` / `ListMetadata()` / `SelectByName()` 共用内存缓存：

- **签名**：各子目录 `SKILL.md` 的目录名 + `mtime` + `size`（排序后拼接）；
- **失效**：新增/删除 skill 目录、修改 `SKILL.md` 后自动重扫；未变时仅 `Stat`，不重复 `ReadFile`；
- **并发**：`sync.RWMutex` 保护；每 session runtime 持有一个 `Catalog` 实例。

每步 `BuildSystemPrompt` 会调用 `RenderLoadedSection`；启用 skills 工具组时，可用 catalog 元数据追加到 system prompt 尾部。tool loop 中 `load_skills` 亦会 `SelectByName`；缓存避免高频读盘。

## SKILL.md 格式

```markdown
---
name: my-skill
description: 单行摘要（做什么 + 何时用）
---

正文…
```

- **`name`** 须与目录名一致；**`description`** 为模型选择依据。
- 目录下所有含 `SKILL.md` 的子目录均参与元数据扫描（无 per-skill `enabled`）。
- 可选 **`hooks/`** 子目录：`load_skills` 时 `plugin.Open` 其中 `*.so`（导出 `Register`）；`unload_skills` / `clear_skills` 时按 `skill/<name>/` 前缀移除。

配置见 `shared/config.Config.Skills`（全局 `enabled`、`root`、`max_in_prompt`）。
