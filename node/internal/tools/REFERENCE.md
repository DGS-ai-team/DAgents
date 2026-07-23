# REFERENCE — `node/internal/tools`

| 符号 | 说明 |
|------|------|
| `ToolDef` / `FunctionDef` | OpenAI tools JSON 结构 |
| `Registry` | 工具注册与 dispatch |
| `NewRegistry(fsRoot, bashTimeoutSeconds, encodings...)` | 创建 registry；`encodings[0]`=bash 输出编码，`encodings[1]`=文件编码 |
| `resolveShellOutputEncoding` / `decodeShellOutput` | bash_run 输出按 GBK/UTF-8 等解码为 UTF-8 |
| `encodeFileContent` / `encodeTextToLegacyChinese` | 写盘编码；GBK 失败回退 GB18030，再失败按 rune 替换 `?` |
| `choosePathEncoding` / `readTextLinesAt` / `rememberPathEncoding` | 路径编码：参数 → 缓存(mtime) → 字节检测 → 配置默认；读写后写缓存 |
| `encodeFileContentWithBOM` / `shouldWriteUTF8BOM` | 写入 utf-8 时可选 BOM；`.ps1`/`.cmd` 新建或替换时自动加 BOM（PowerShell 5.1 须 UTF-8 BOM）；已有 BOM 的文件仍保留 |
| `formatEncodingHeaderLines` | read 结果 header：`文件编码`、`编码来源`、可选 `编码提示` |
| `SetBuiltinEnabled(names)` | 配置 LLM 可见内置工具允许列表（空=全部） |
| `Definitions()` | LLM tools 列表 |
| `Execute(ctx, name, arguments)` | 执行工具 |
| `execReadFile` / `execWriteFile` / `execSearchReplace` / `execGlobFiles` / `execGrepFile` / `execGrepFiles` / `execSearchFile` | 内置实现（`search_file` 为 `grep_file` 别名） |
| `runBashSyncWithAutoDegrade` | bash 同步等待；显式 timeout 可降后台，省略则硬上限杀进程；支持 UI 终止/转后台 |
| `resolveRunCWD` / `resolveShellType` / `blockedNonRootPasswordPromptingShell` | bash_run 参数与安全策略 |
| `applyShellProcAttr` / `signalKillProcessGroup` | POSIX/Windows 进程组（`shell_platform_*.go`） |
| `WithBackgroundExecution` | 内部：标记后台 Execute，跳过同步窗口（不对 schema 暴露） |
| `StartBackground(ctx, sessionID, toolName, toolCallID, cleanedArgs)` | 后台执行并返回 ACK |
| `ParseRunInBackground(arguments)` | 剥离 call_purpose / 历史 run_in_background |
| `injectCallPurposeParam` | 注入 call_purpose（加入 required） |
| `SetTriggerRuntime(store, sched, agentID)` | 注入触发器运行时 |
| `SetWeComClient(client)` | 注入企业微信 webhook 客户端（暴露 wecom_*） |
| `execTriggerList` / `execTriggerGet` / `execTriggerCreate` / `execTriggerUpdate` / `execTriggerDelete` | 触发器工具 |
| `IsBackgroundJobTool(name)` | 后台管理工具（强制同步） |
| `fs_helpers.go` | `textSuffixes`、`isTextReadable`、`readAllLines`、`windowFromTotal`、`applyMaxTokensToBody`、`mergeLineRanges` 等 |
