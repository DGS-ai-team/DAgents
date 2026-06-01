# node/internal/promptcontext

读取 `.runtime/prompt_context/` 侧车 Markdown（`soul.md` / `user.md` / `custom.md`）与 `.runtime/memory/long_term.md`，供 system prompt 拼接。

| 文件 | 说明 |
|------|------|
| `reader.go` | `Reader`：mtime 缓存、EnsureSidecarFiles、段落构建 |
