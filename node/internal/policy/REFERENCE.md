# REFERENCE — `node/internal/policy`

## `engine.go` / `maps.go`

| 符号 | 类型 | 说明 |
|------|------|------|
| `Action` | `type` | `auto` / `require_approval` / `deny` |
| `Engine` | `struct` | 工具与 shell 策略查表 |
| `NewDefaultEngine` | `func` | 从当前版本策略种子构造默认引擎 |
| `(e *Engine) Decide` | `method` | 仅工具名（bash 无参数时保守） |
| `(e *Engine) DecideTool` | `method` | 工具名 + 参数（含 bash shell 子策略） |

## `file_loader.go`

| 符号 | 说明 |
|------|------|
| `LoadFromDir` | 读取指定目录下的 tool/shell txt 策略，供测试和本地检查使用 |

## `entry_file.go`

| 符号 | 说明 |
|------|------|
| `parseEntryFile` | 解析 `key=mode` 行 |

## `shell_parse.go`

| 符号 | 说明 |
|------|------|
| `ShellType` | `bash` / `cmd` / `powershell` |
| `ResolveShellType` | 解析 bash_run 的 shell 类型 |
| `ParseCommandRoots` | 拆分命令并提取每段首词 |
| `SplitBashStatements` | bash 语句切分（tools 复用） |

## `mode.go`

| 符号 | 说明 |
|------|------|
| `ApprovalMode` | `always` / `never` / `rule` |
