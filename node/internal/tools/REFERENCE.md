# REFERENCE — `node/internal/tools`

| 符号 | 说明 |
|------|------|
| `ToolDef` / `FunctionDef` | OpenAI tools JSON 结构 |
| `Registry` | 工具注册与 dispatch |
| `NewRegistry(fsRoot, bashTimeoutSeconds, encodings...)` | 创建 registry；`encodings[0]`=bash 输出编码，`encodings[1]`=文件编码 |
| `resolveShellOutputEncoding` / `decodeShellOutput` | bash_run 输出按 GBK/UTF-8 等解码为 UTF-8 |
| `encodeFileContent` / `encodeTextToLegacyChinese` | 写盘编码；GBK 失败回退 GB18030，再失败按 rune 替换 `?` |
| `SetBuiltinEnabled(names)` | 配置 LLM 可见内置工具允许列表（空=全部） |
| `Definitions()` | LLM tools 列表 |
| `Execute(ctx, name, arguments)` | 执行工具 |
| `execReadFile` / `execWriteFile` / `execSearchReplace` / `execGlobFiles` / `execGrepFile` / `execGrepFiles` / `execSearchFile` | 内置实现（`search_file` 为 `grep_file` 别名） |
| `runBashSyncWithAutoDegrade` | bash 同步超时自动降级后台 |
| `resolveRunCWD` / `resolveShellType` / `blockedNonRootPasswordPromptingShell` | bash_run 参数与安全策略 |
| `applyShellProcAttr` / `signalKillProcessGroup` | POSIX/Windows 进程组（`shell_platform_*.go`） |
| `WithBackgroundExecution` | 内部：标记后台 Execute，跳过同步窗口（不对 schema 暴露） |
| `StartBackground(ctx, sessionID, toolName, toolCallID, cleanedArgs)` | 后台执行并返回 ACK |
| `ParseRunInBackground(arguments)` | 剥离 call_purpose / 历史 run_in_background |
| `injectCallPurposeParam` | 注入 call_purpose（加入 required） |
| `SetTriggerRuntime(store, sched, agentID)` | 注入触发器运行时 |
| `execTriggerList` / `execTriggerGet` / `execTriggerCreate` / `execTriggerUpdate` / `execTriggerDelete` / `execTriggerFire` | 触发器工具 |
| `IsBackgroundJobTool(name)` | 后台管理工具（强制同步） |
| `fs_helpers.go` | `textSuffixes`、`isTextReadable`、`readAllLines`、`windowFromTotal`、`applyMaxBytesToBody`、`mergeLineRanges` 等 |
