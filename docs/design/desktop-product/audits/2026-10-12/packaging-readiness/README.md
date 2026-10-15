> **已被后续架构取代（2026-10-12）** 本记录保留重构前的验收范围与证据，不再作为当前架构或操作指南。请参阅[Node–Desktop API 边界报告](../node-api-boundary/README.md)。

# Desktop 打包与本机集成验收清单（2026-10-12）

本打包专项基于静态代码与 CI 配置核对。编写清单时我没有启动 Node、Tauri UI 或目录选择器，也没有生成安装包、签名或发布；root 随后为设置页验收成功启动隔离 Node/Tauri，但未执行本清单中的安装、目录选择器或 bridge 生命周期检查。Rust 本机单测仅用于确认当前 Shell 代码回归。

## 静态核查结果

| 范围 | 当前实现 | 尚需验收 |
| --- | --- | --- |
| Tauri Shell 构建 | `tauri.conf.json` 声明 NSIS bundler；仓库脚本 `scripts/ci/build_dagents_shell_tauri.sh` 在 Windows 构建 Tauri Release EXE，默认 `SKIP_BUNDLE=1`，产出 `dagents-shell-tauri.exe` 和默认名副本。正式 Windows 包由外层 Inno Setup 将现代 Tauri Shell 与 Go 兼容 Shell 作为可选任务打包。CI 分别覆盖 x64/x86 Release EXE 与 Inno 包装。 | 当前工作树未构建安装器；检查干净 Windows x64/x86 机器上的安装、覆盖安装、卸载、默认 Shell 选择和兼容 Shell 选择。WebView2 缺失时验证向导默认兼容模式，并核实用户数据保留策略。 |
| 更新与签名 | Shell 更新流程识别 Windows Inno 安装包并启动安装器；本轮只读核查代码和 workflow，没有检查证书、签名策略或最终 Release 资产。 | 在隔离测试版本验证版本元数据、下载资产、哈希/manifest、Windows SmartScreen/签名状态、更新中关闭应用行为、失败恢复和卸载。不得将未签名本机产物当作发布验收。 |
| Node 原生目录选择器 | `POST /v1/platform/directory-picker` 由 Node 进程执行平台 picker，不依赖 Desktop bridge。Windows 选择 PowerShell（优先 `powershell.exe`，回退 `pwsh.exe`），以 `-STA` 启动 WinForms `FolderBrowserDialog`；macOS 用 `osascript`，Linux 依次查找 `zenity`、`kdialog`、`yad`。取消以成功响应 `cancelled=true` 返回。Agent 工作区选择组件允许手动输入绝对路径。 | 在 Windows 安装版实际点选、取消和重试，确认路径含空格/中文/网络盘时正确；检查权限受限路径处理、PowerShell 不可用时可见降级、目录选择不会在取消时保存空路径。Linux/macOS 的系统弹窗尚未验。 |
| Desktop bridge 权限 | Rust bridge 只绑定 `127.0.0.1:18767`，除 `/health` 外要求 bearer token；token 由 Shell 随机生成并通过 Node 子进程环境变量交给 Node，Web UI 经 Tauri `invoke` 转发 Node 请求，不直接调用 bridge。Node `/v1/platform/*` 再转发剪贴板、窗口焦点与更新等能力。 | 安装版逐项验证缺 token/错误 token 拒绝、正确 token 通过、非 localhost 请求来源限制、Node 未启用 Shell 时能力探测和降级。健康端点无认证是探活设计，应确认不返回身份或敏感信息。 |
| Bridge 生命周期与单实例 | Bridge 在线程中启动，退出事件设置 stop flag；Shell mutex 以解析后的 config 路径哈希形成实例标识。因此同一配置下阻止第二 Shell，但不同配置路径允许另一实例。bridge 地址当前固定为 `18767`。 | 单配置重复启动、托盘隐藏/恢复、正常退出后端口释放、异常退出后重开；启动 Node 失败、bridge 端口被占用、Node 重启后 Shell 重连均需验收。不同配置路径并行 Shell 会发生固定端口竞争，当前不应视为支持场景。 |

## 最小可执行验收顺序

1. 在干净 Windows VM 安装签名/发布候选 Inno 包，记录版本、架构、WebView2 状态和安装选项；分别走现代 Shell 与兼容 Shell 安装路径。当前未要求本轮创建候选包。
2. 首次启动现代 Shell，确认只启动一个 Node、托盘与 Desktop 可打开；对同配置二次启动应聚焦/复用现有 Shell 或按产品设计明确提示，不得悄悄启动第二个 Node。
3. 在 Agent 创建/工作区表单打开系统目录选择器：选一个含中文和空格的目录并核对返回绝对路径；重复操作后取消，确认原值不变；输入手动路径保存并重开确认。
4. 重启 Node、隐藏/恢复窗口、通过正常 Shell quit 退出，依次核对 `GET /v1/platform/capabilities` 状态、bridge token 校验、`18767` 正常释放及重新启动恢复。验收日志不得记录 token。CLI 强制结束进程后端口释放不能替代正常 quit 验收。
5. 在更新隔离通道验证安装包版本检查、下载校验、启动 Inno、升级后用户数据保留及失败恢复；不使用正式用户数据，也不直接触发正式更新源。
6. 单独记录第二配置路径启动 Shell 的结果。目前固定 bridge 端口不支持这种并行方式；除非产品确认需要多配置并行，否则将其作为限制记录，不用它替代正常单实例验收。

## 本轮证据与未覆盖范围

- `cargo test --locked --offline --manifest-path desktop/tray-tauri/src-tauri/Cargo.toml`：29 项通过。测试过程中没有启动 Shell UI 或 Node，也没有联网下载依赖。
- Tauri Windows CI workflow 构建 x64 与 x86 Shell 二进制；发布 workflow 再通过 Inno 组装安装器。当前未检查实际 CI 运行产物，未构建本地安装器，未签名或发布。
- Node picker 的现有 Go 测试覆盖 picker 能力探测不打开弹窗、返回所选路径和取消语义；这些不是 Windows 实机弹窗验收。
- 辅助 bridge 的实现与此前“两个不同配置 Desktop 实例争用固定 `18767`”证据一致，本轮只列为已知限制，没有改端口策略或权限架构。root设置页验收期间仅观察到 `18767` 由 Desktop `20500` 监听，CLI强制结束后端口释放；bridge health/auth/clipboard API与正常 quit lifecycle组合均未执行（审批拒绝），不计为验收证据。
- 安装/卸载、覆盖升级、Windows Defender/SmartScreen、目录弹窗实际交互、bridge 异常退出恢复、正常退出释放端口、辅助 bridge 启动冲突尚未验收。本打包审计者没有启动 Shell；root后来为设置页隔离验收启动了 Tauri，但没有执行本清单对应的打包/bridge检查。
