# REFERENCE — `node/internal/tools`

| 符号 | 说明 |
|------|------|
| `ToolDef` / `FunctionDef` | OpenAI tools JSON 结构 |
| `Registry` | 工具注册与 dispatch |
| `NewRegistry(fsRoot, bashTimeoutSeconds, shellOutputEncoding...)` | 创建 registry；编码来自 `config.yaml` `tools.bash_output_encoding` |
| `resolveShellOutputEncoding` / `decodeShellOutput` | bash_run 输出按 GBK/UTF-8 等解码为 UTF-8 |
| `Definitions()` | LLM tools 列表 |
| `Execute(ctx, name, arguments)` | 执行工具 |
| `execReadFile` / `execWriteFile` / `execSearchReplace` / `execGlobFiles` / `execGrepFile` / `execGrepFiles` / `execSearchFile` | 内置实现（`search_file` 为 `grep_file` 别名） |
| `runBashSyncWithAutoDegrade` | bash 同步超时自动降级后台 |
| `resolveRunCWD` / `resolveShellType` / `blockedNonRootPasswordPromptingShell` | bash_run 参数与安全策略 |
| `applyShellProcAttr` / `signalKillProcessGroup` | POSIX/Windows 进程组（`shell_platform_*.go`） |
| `WithBackgroundExecution` | 标记显式后台 Execute，跳过同步窗口 |
| `StartBackground(ctx, sessionID, toolName, toolCallID, cleanedArgs)` | 后台执行并返回 ACK |
| `ParseRunInBackground(arguments)` | 解析并剥离 run_in_background |
| `injectRunInBackgroundParam` / `ensureToolSchemaRequired` | 注入 run_in_background；保证 parameters 含 `required` 数组 |
| `SetTriggerRuntime(store, sched, agentID)` | 注入触发器运行时 |
| `execTriggerList` / `execTriggerGet` / `execTriggerCreate` / `execTriggerUpdate` / `execTriggerDelete` / `execTriggerFire` | 触发器工具 |
| `IsBackgroundJobTool(name)` | 后台管理工具（强制同步） |
| `fs_helpers.go` | `readAllLines`、`windowFromTotal`、`applyMaxBytesToBody`、`mergeLineRanges` 等 |
