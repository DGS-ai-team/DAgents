# Desktop 启动时关闭的 stderr 导致 panic

日期：2026-10-19。修复前基线为 `0450f5ec`，其 Desktop 与共享前端相对候选产品源码 `95e3f98c` 没有差异。本记录只覆盖启动崩溃专项，不替代完整安装或稳定版放行。

## 问题与定位

首次安装验收曾出现 Desktop 启动后退出，Windows 记录 `0xC0000409`、子码 `7`。原始 WER dump 与匹配 PDB 确认 Rust panic-abort；dump 中还有 Win32 `232` 和 stderr 打印相关线索，但无法恢复可靠业务调用栈。因此没有直接依据这些栈扫描值修改源码。

随后在隔离目录使用实际候选 Desktop 做对照：同样连接一个没有 Node 监听的 loopback 端口，仅改变父进程是否保留 stderr 管道读端。

| 构建与条件 | 结果 |
| --- | --- |
| 原候选，stderr 持续读取 | 运行超过 15 秒，记录 Agent 同步与 SSE 连接拒绝，正常退出请求和应用退出码均为 `0`。 |
| 相同原候选，启动后关闭 stderr 读端 | 约 3 秒退出，退出码 `-1073740791`，即 `0xC0000409`。 |
| 同源诊断版，关闭 stderr 读端 | 同样退出；独立文件捕获 `failed printing to stderr: 管道正在被关闭。 (os error 232)`。 |

诊断版从同源 Rust 代码构建，只给入口增加 QA panic hook，并为应用 crate 保留符号；保持 release 的 `panic = "abort"`。直接 Cargo 构建时没有启用 Tauri CLI 的 `custom-protocol` 特性，因此它只用于后台 panic 定位，不计作正式界面或发行构建验收。hook 不调用默认 stderr hook，文件写失败按 Result 处理，不作为生产修复发布。第一次回溯因 PDB 文件名与 CodeView 名称不一致而没有业务符号；将匹配 PDB 以 `dagents_shell.pdb` 放在映像旁后重新运行，获得如下调用链：

```text
Subscriber::start 的线程闭包             events.rs:57
  → stream_loop                       events.rs:88
  → connect_once                      events.rs:98
  → sync_agents                       events.rs:121
  → std::io::stdio::_eprint
  → std::io::stdio::print_to           std/io/stdio.rs:1166
  → panic: failed printing to stderr, OS error 232
```

Node 未就绪、临时断线是正常错误。真正使进程退出的是错误处理路径使用 `eprintln!` 向关闭的 stderr 再次写入；release 的 abort 策略使整个 Desktop 退出。受控复现已证明这个缺陷及实际调用位置，与最初崩溃的异常码、Rust abort 和管道错误线索一致。原始 dump 本身没有恢复出 panic 文本，不能声称从原 dump 独立证明了相同的完整调用链。

## 复现范围和证据

所有案例使用 `.runtime-test-startup-panic-20261019/` 下独立 home、配置和缓存，不启动 Node：

- `connection_mode = tunnel`，endpoint 固定为 `http://127.0.0.1:18973`，每次预检无监听。
- 关闭通知和更新检查，显式指定 Desktop 配置；WebView2 使用每案例独立的 `WEBVIEW2_USER_DATA_FOLDER`。
- 固定映像 SHA256，检查目录无 reparse point；不枚举或结束其他安装的进程。
- 观测 15 秒。存活的案例通过同一映像、同一 home 的 `--quit-desktop` 正常关闭；没有按名称杀进程。
- 原安装候选及其 manifest 不修改。现有 18977 Node 不属于测试范围。

原候选 Desktop SHA256：
`7c05f8e37e9683514e25d545756e6543e13550352a1a2758bdb627a5201f7493`

诊断 Desktop SHA256：
`1c117338eef03eeb52cdea47c719d25186951cbf72ef9753feb622cb1f1be0b1`

诊断 PDB SHA256：
`c47a545ca6955d725238ce0a265530b2c702c1b18df8233b55871d7af0006856`

EXE CodeView 与 PDB 的 GUID/age 均为 `51F9E501-24C0-451D-B4C6-E2FB08DF1C49` / `1`。执行这组对照的 runner SHA256 为 `8ea01f53b96ecd346cee092d3f42bb89183aaea2bad67a7b425d9ca466d2004c`。案例结果与 panic 原文保存在本地 ignored QA 目录，不提交 WER dump 或用户环境。

## 修复与验收结果

`99ab8c8c` 将 Desktop 的 stderr 输出统一为 best-effort writer：写入失败不再 panic，保留原错误文本、换行和业务退出码。覆盖 SSE 同步/重连、通知失败、启动错误与 CLI 错误输出。连接重试、线程生命周期和 release abort 策略保持原样，没有生产 panic hook 或全局 catch。

补查 CLI 时发现 stdout 有相同问题。直接在 PowerShell 捕获 GUI 子系统 EXE 的输出不能可靠等待进程，因此改用显式 `Process`、异步排空输出并有界等待退出的独立 runner。原候选在 stdout 正常时输出准确的 `{"running":false}\n`、退出 `0`；读端关闭后捕获到 `failed printing to stdout`、OS error `232` 和 `0xC0000409`。

`09c6fb1c` 补齐 stdout 容错，保留状态 JSON 和更新状态文本的正常字节。升级确认提示仍无结尾换行并执行 flush；提示写入或 flush 失败时按取消处理，不再等待不可见提示的 stdin 输入。

最终源码 `09c6fb1c0bc663cbdbb0051d3404e601508fab5e` 的验证：

- `cargo test --locked --lib`：51 项通过，包括 BrokenPipe、零进度写入、UTF-8/换行、状态 JSON、提示无换行和 flush 失败。
- `cargo fmt --all -- --check`、`git diff --check` 通过。
- 以 git archive 冻结源码，使用同一套未变更的前端 dist；最终 release 构建显式启用 `--features tauri/custom-protocol`，确认对应 Tauri fingerprint 含该特性。

最初直接 Cargo 构建的修复产物缺少 `custom-protocol`，已排除为 non-production attempt，不作为界面或发行验收输入。最终产物为 x64（PE `0x8664`），FileVersion/ProductVersion 均为开发验收值 `0.11.0`，默认 release 的 LTO、strip、`panic = "abort"` 生效，无 QA hook。

| 修复产物与条件 | 结果 |
| --- | --- |
| `99ab8c8c` 正式模式，stderr 正常 | 存活 15 秒，连接错误仍按原文输出，正常请求退出和应用退出均为 `0`。 |
| 同产物，stderr 读端关闭 | 存活 120 秒，正常请求退出和应用退出均为 `0`。原生窗口打开离线对话、顶栏菜单和 Node 通用设置页，UIA 文档地址为 `http://tauri.localhost/`；1422 无开发服务监听。 |
| 最终 `09c6fb1c` 正式模式，stderr 读端关闭 | 存活 30 秒，正常请求退出和应用退出均为 `0`。此轮未在自动退出前再次捕获窗口，不重复声称最终产物有原生交互截图。共享前端与此前产物一致，增量只涉及 stdout。 |
| 最终产物，CLI stdout 正常 | `--desktop-status` 精确输出 `{"running":false}\n`，退出 `0`。 |
| 最终产物，CLI stdout 读端关闭 | 同一命令退出 `0`，没有 panic 文本。 |

`99ab8c8c` 正式模式 Desktop：4,768,256 字节，SHA256
`73f15a57aeca6349caef9f44ff20a4a382ce093bce2cfba5a5b73673cdb7335f`。

最终 `09c6fb1c` Desktop：4,766,720 字节，SHA256
`d1bd926a0dcdc12c2b34e690585f1aa483621569399a86468e68094e46d0d7db`。

最终源码 archive SHA256：
`d0b67aa21cf30c9693fd8c46c70eb7ae91d2a7531a1630a02b4349584b3cb82a`。

最终 GUI runner SHA256：`b96d41274c34631a8de6dd3fc7af60e3c5da6ac6a5ea8133be7477470d65f7ad`；CLI runner SHA256：`54545b1b183cfc06eb09cb5267bbb9cd73cb4ba66329c7fd9cd192108e552130`。每项案例使用新目录，历史输入与结果没有覆盖。测试后原 18977 Node 仍为同一进程，18973 没有 Node 监听。

本地专项已关闭这个已复现的启动/输出崩溃缺陷。后续 PR #350 已通过检查并合入 `dev`，冻结提交为 `6a0107c2`；[最终候选复验](release-candidate-6a0107c2.md)已覆盖修复产物的实际安装、原生使用、运行中升级、恢复和卸载。不将本次修复解释为所有可能启动失败均已覆盖。

旧候选的安装、升级、卸载证据继续保留；修复后二进制的结论以新候选的独立哈希和验收记录为准。标准用户、正式安装分支及其他未覆盖环境仍受发版清单约束。
