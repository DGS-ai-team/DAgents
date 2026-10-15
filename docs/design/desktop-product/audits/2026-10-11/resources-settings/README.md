# Desktop 资源与设置原生验收

日期：2026-10-11。对象：Windows Tauri 原生窗口。验收由 root 完成；本报告汇总本机 UI 证据及本轮 CLI/API 和自动测试结果。

## 环境与隔离

- 原生 Desktop 使用隔离 Node `18766`、browser-service `18768`、合成双页面站点 `18769`，模型为真实 `mimo-v2.5-pro`。主开发 Node `18765` 未修改。
- root 创建的原生验收 Agent：`agt-e07b194680b18b59`。设置验证仅修改 Node 名称为“Desktop 设置验收 2026-10-11”，未改 LLM 配置。
- 本轮独立 PTY live smoke 使用另一个临时 Agent `agt-6829a460ccff51ed`。测试完成后确认终端会话数为 0，通过 Node API 归档，并验证归档 Agent GET 返回 404；未触碰已有 Agent。
- Oct 11 原生验收收尾已由 root 确认完成：非秘密的 features/browser/agent/user 设置已恢复；原生验收 Agent `agt-e07b194680b18b59` 与终端 fixture `agt-5e11c18460409484` 均归档并 GET 404；testDesktop PID `18696`、隔离 Node PID `4748`、browser/site 和其他测试服务已停止，18766 不再监听。主 Desktop `6760` 与主 Node `16912` 保持在线，原生窗口已恢复。[终端清理后状态](10-terminal-cleanup.jpg)

## 原生验收结果

| 项目 | 结果 | 证据 |
|---|---|---|
| 新建 terminal | 通过 | 原生资源页新建终端后显示实际 PowerShell prompt。[终端已连接](01-terminal-connected.jpg) |
| Agent 隔离与恢复 | 通过 | 切换到另一个 Agent 时资源数为 0；切回原 Agent，点击保留的 Shell 页签后重新连接，prompt 保留。[恢复终端](02-terminal-resumed.jpg) |
| 明确关闭 terminal | 通过 | 关闭最后一个 Shell 页签后，`GET /v1/agents/{agent_id}/terminals` 返回 `count=0`，UI 资源数也为 0。[终端清理后状态](10-terminal-cleanup.jpg) |
| 同一 Agent 多 terminal | 通过 | 两个独立 Shell 均有各自 prompt，API 返回 `local-terminal-1` 和 `local-terminal-2`（count=2）。切换到另一个 Agent 再切回后，不重连直接关闭第一个 detached tab，API 只剩 `local-terminal-2`（count=1）；UI 自动切换并恢复该 Shell 的 prompt。关闭最后一个后 API count=0、UI 资源数=0。[双终端已连接](08-two-terminals-connected.jpg)、[关闭 detached tab 后恢复下一个](09-detached-terminal-closed-next-resumed.jpg)。 |
| 设置保存与重新读取 | 有限通过 | 原生设置页修改 Node 名称并保存；切到连接设置页再返回通用设置，名称仍一致。`GET /v1/setup/config` 的 `agent.name` 与 `GET /v1/agent/info` 的 `name` 相同。[保存设置](03-settings-saved.jpg)、[重新读取](04-settings-reloaded.jpg) |
| 设置提示 | 通过 | 保存后显示“部分设置需重启后生效”的通用提示。此次仅验证 Node 名称，不代表所有设置项的保存或运行时生效路径。 |
| 浏览器会话与第二页面 | 通过 | 第一回合模型主动调用 `stop`，没有留下 session 是该回合的结果。第二回合启动浏览器并打开 `127.0.0.1:18769`，点击 `target=_blank` 链接创建 `page_2`。view API 返回第二页面标题和 URL，页面列表含两个页面。[第二页面](05-browser-second-page.jpg) |
| Browser 资源页 | 通过 | Desktop “查看 Agent 浏览器”首先显示第二页标记 `DESKTOP_SECOND_PAGE_OK`；切换到第一页后显示 `NODE_BROWSER_RESOURCE_OK`，资源页签和标题随页面同步。[第二页内容](05-browser-second-page.jpg)、[第一页内容](06-browser-first-page.jpg) |

Browser viewer 是复用 Node Agent 会话的只读查看器。本轮没有人工接管、地址栏输入或页面点击操作；它不是完整嵌入式浏览器。

## CLI、API 与自动测试

真实 Node `18766` 上运行 Rust 测试 `terminal::tests::live_local_node_terminal_open_input_detach_resume_close_smoke`，通过 `DAGENTS_TERMINAL_SMOKE=1`、`DAGENTS_NODE_ENDPOINT=http://127.0.0.1:18766` 和临时 Agent `agt-6829a460ccff51ed` 执行实际 PTY open、输入 marker、detach、resume/replay、close；确认 Node session 列表最终为空。

Node Go 定向测试通过：

- `go test ./internal/api -run 'TestAgentTerminalToolSessionIsListedAndTerminated|TestAgentTerminalWebSocketRunsPTY|TestTerminalSessionRegistryEnforcesAgentLimitAndIsNotPersistent' -count=1`
- `go test ./internal/tools -run 'TestTerminalToolsUseSharedSessionBroker|TestBashRunSync|TestBashRunSyncTimeoutFailsWithoutBackgroundJob|TestBashRunUsesConfiguredShellProvider' -count=1`

这些用例证实 `bash_run` 是同步的一次性执行，不留下 PTY/后台 job；Desktop UI PTY 与同一 Agent 的 `terminal_*` 工具共享终端会话 broker，会话按 Agent 隔离。该共享属于既定设计，本轮未重构。

Oct 11 前半轮 Desktop UI 测试 `npm run test:ui`：37/37 通过；新增 detached terminal close flow 后复测为 40/40，`npm run build` 通过。构建仍显示既有 chunk 大小和动态/静态导入提示。

## 终端并发问题与修复

双终端原生复验最初发现第二个页签长期停留在“连接中”，Node 在第一个终端关闭前只列出 `local-terminal-1`；故障过程截图 [07：双终端连接中](07-two-terminal-tabs.jpg) 只记录问题，不是通过证据。

原因是本地交互 PTY 在整个 shell 生命周期持有 workspace 写租约，空闲终端会阻塞同一 Agent 的第二个终端和一次性写操作。现已移除交互 PTY 的生命周期租约，保留 `bash_run`、文件写入等有界操作的租约协调；并新增同工作区同时启动两个 PTY、PTY 保持打开时 `bash_run` 完成的真实本地回归。修复后 root 原生复验通过：两个 PTY 同时连接，detached tab 可直接关闭，剩余 tab 自动恢复，最后关闭后 API/UI 都清空。CLI 回归证明 PTY 保持打开时 `bash_run` 仍完成。

最终检查：`go test ./internal/tools -count=1`、`go test ./internal/api -count=1` 通过；隔离 Node 新 binary `go build` 通过。Desktop UI 在 terminal close/detach fallback 回归后，`npm run test:ui` 为 41/41，`npm run build` 通过。前一轮结果为 40/40。Desktop build 保留既有 chunk 大小和动态/静态导入提示。

## 尚未覆盖

这次设置 smoke 只覆盖 Node 名称，不代表全部设置页面、外部服务配置都通过。其余待验包括：工作组和 Auto 定时链路、全部审批类型、打包与辅助 bridge/单实例目录选择器、附件上传时跨 Agent 切换、独立缩放窗口复验，以及 Browser 人工接管/地址栏导航。真实会话的其他故障与恢复组合也未覆盖。
