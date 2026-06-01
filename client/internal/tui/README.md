# client/internal/tui — Go Client 终端（与 Python Textual 分离）

| 路径 | 用途 |
|------|------|
| [`dispatch.go`](dispatch.go) | 入口 `Run`：按环境/`--plain` 选择 full 或 repl |
| [`full/`](full/README.md) | **bubbletea 全屏 TUI**（默认；上输出 / 下输入） |
| [`repl/`](repl/README.md) | **行模式 REPL**（`--plain`、`TERM=dumb` 兜底） |
| [`shared/`](shared/README.md) | transcript、tool 折叠（full/repl 共用） |

Python Textual 在 [`app/cli/tui/`](../../../app/cli/tui/README.md)，互不引用。

## 用法

```bash
# 默认全屏（SSH 推荐）
go run ./client/cmd/dagents-client tui

# 强制行模式 REPL
go run ./client/cmd/dagents-client tui --plain

# 环境变量
export DAGENTS_TUI=plain   # 或 full
```
