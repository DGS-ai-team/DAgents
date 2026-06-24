# node/plugins

Skill / 全局 Hook 的 `.so` 插件源码（`-buildmode=plugin`）。须在 **node 模块内**构建，以便合法引用 `node/internal/hooks`。

| 目录 | 说明 |
|------|------|
| `protect-loaded-skill/` | 与内置 `builtin.loaded_skill_file_guard` 同逻辑的示例 plugin |

构建：

```bash
go build -buildmode=plugin -o protect-loaded-skill.so ./plugins/protect-loaded-skill
```

（在 `node/` 目录下执行。）

`packaging/runtime/skills/write-skill/hooks/build.sh` 封装同上命令并将产物写到 skill hooks 目录。

单测：`go test ./plugins/...`（仅 `linux` 构建 tag 下编译 plugin）。
