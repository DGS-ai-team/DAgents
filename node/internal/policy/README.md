# 本地策略（Agent Node N4）

| 文件 | 说明 |
|------|------|
| `engine.go` | `DecideTool`：工具 + bash shell 细粒度审批 |
| `bootstrap.go` | 确保 `.runtime/policy` 存在并从 `packaging/runtime/policy` 种子复制 |
| `entry_file.go` | 解析 `key=mode` txt 策略（always / never / rule） |
| `shell_parse.go` | bash/cmd/powershell 命令拆分与 root command 提取 |
| `mode.go` | 审批模式常量 |

策略文件布局（与 Python v1 对齐）：

- `<runtime>/policy/tool.approval.txt` — 工具级：`tool_name=mode`
- `<runtime>/policy/shell/bash.approval.txt` — bash 首词：`command=mode`
- `<runtime>/policy/shell/cmd.approval.txt`
- `<runtime>/policy/shell/powershell.approval.txt`

`bash_run=rule` 时按 shell 策略逐段判定；未命中默认为 `rule`（需审批）。种子见 [`packaging/runtime/policy`](../../packaging/runtime/policy)。
