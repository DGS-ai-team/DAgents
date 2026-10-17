# Desktop 终端生命周期原生验收

日期：2026-10-16。使用 Computer Use 操作真实 Tauri 窗口；Node 状态通过 API 对照。未在终端界面键入或执行命令。

## 环境

- 独立测试 Node：`http://127.0.0.1:18976`。
- 初始构建：`bin/dagents-shell-terminals.exe`，SHA256 `20BBA3D84922D3E8EFFC600A00017244A02A43C0CDA4A2F5089BA39E0485A7C8`。
- 测试 Agent：`agt-8deb9bb81aae8b1f`；真实模型 `mimo-v2.5-pro`。
- 该测试 Agent 原启用 `fs/bash`；本轮通过 API 临时添加 `terminal` 工具组，原审批策略保留，仅批准本次 `terminal_config_list` 和 `terminal_open` 调用。

## 已完成的原生验收

| 场景 | 原生界面与 Node 对照结果 |
| --- | --- |
| 打开终端入口 | 顶栏资源菜单与 Ctrl+Shift+T 均能打开选择器，已有会话和新建目标分区清楚；本机选项识别为 PowerShell。 |
| 模型创建，Desktop 恢复 | 模型先调用 `terminal_config_list`，再调用 `terminal_open`，创建 `local-terminal-4`；两次工具均成功，回合正常结束。选择器可发现该会话，恢复后显示原有 PowerShell 提示符。 |
| 恢复只读与重放 | 恢复后显示只读说明及启用手动输入操作；多次断开和恢复没有叠加重复的 PowerShell 提示符。未通过 UI 向终端发送按键。 |
| 断开连接 | 页签保留、状态变为连接已断开，Node 列表仍有同一个 `local-terminal-4`；重新连接成功。 |
| 关闭展示 | 点击页签 X 后页签移除、返回对话；Node 中同一 ID 仍为 running；重新打开选择器后可再次恢复。 |
| 取消结束 | 结束确认框明确提示会中断命令；Esc 关闭确认框，Node 会话数量仍为 1。 |
| 明确结束 | 对本次创建的空测试终端点击确认结束；页签移除，Node 列表从 1 变为 0。 |
| 从 Desktop 新建 | 选择本机 PowerShell 创建 `local-terminal-5`；Node 会话数量为 1，连接成功。 |
| 重复选择已连接终端 | 再次选择 `local-terminal-5`，复用同一个页签与连接，无 already connected 错误，Node 会话数量仍为 1。 |
| 切换 Agent | 切换到 `agt-11a2d995048d248b` 不显示其他 Agent 的终端；原 Agent 中仍有 1 个会话，另一个 Agent 为 0。返回原 Agent 保留其终端页签。 |

## 发现并修复

1. Node 已返回工作目录与目标，但 Rust `TerminalSummary` 漏传了相关字段。初始构建错误显示“默认目录”，SSH 也可能误标为本机。已补传 `agent_id/config_id/target_kind/target_id/cwd`，新增 mock HTTP 反序列化和序列化回归。
2. 启用手动输入按钮原本位于终端输出区，挤占输出高度。已移至终端工具栏，归属/只读说明使用紧凑单行，输出区使用剩余空间。

修正版：`bin/dagents-shell-terminals-reviewed.exe`，18,848,768 字节，SHA256 `442C177DA8B34BB56B6EAFE759869B48E8A0B63688E5CB6B85ED9FAA0506B666`。Rust 34 项、Desktop UI 64 项、终端选择/结束弹窗 8 项测试通过，原生内嵌前端构建成功。

## 修正版复验

- 通过进程路径确认运行的是 `dagents-shell-terminals-reviewed.exe`，再在真实窗口复验。
- 终端选择器显示 Node 返回的真实工作目录，不再误显示“默认目录”；本机 PowerShell、短 ID、恢复入口均可辨识，长目录换行后仍能完整阅读。
- 保持 Node 运行，退出旧 Desktop 并启动 reviewed 构建。新窗口没有旧的终端页签，但从 Node 列表找回了同一个 `local-terminal-5`；恢复后显示原有 PowerShell 提示符，Node 数量仍为 1，创建时间未变。
- 恢复默认只读。点击工具栏“启用手动输入”后，说明变为“手动输入已启用”，原生无障碍树中的 Terminal input 从不可设置变为 settable；未向终端键入任何命令。
- 工具栏放置启用输入/断开/结束操作，归属提示保持一行；黑色终端输出区延伸至面板底部内边距，右下悬浮输入栏覆盖其上，不再为启用按钮额外预留一行。
- 在 reviewed 窗口明确结束 `local-terminal-5`，页签移除并返回对话；Node 列表为 0。已恢复测试 Agent 原有 `fs/bash` 工具组，Node health 为 ok，修正版 Desktop 保持打开。

结论：本机终端新建、恢复、只读/手动输入、断开、关闭展示、明确结束、重启恢复和元数据展示通过。未连接真实 SSH 主机；SSH 元数据透传由 mock HTTP 回归覆盖，不计作真实 SSH 连接证据。
