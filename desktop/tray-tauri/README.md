# DAgents Desktop（Tauri）

当前 Windows 人机入口是内嵌前端的 Tauri 2 Desktop。Node 只提供运行 API，不提供 /ui/ 页面；已退休 Go Shell 的模块和构建入口已移除。主线是一个使用者拥有一个 Node，可从多个 Desktop 访问，不实现多用户租户隔离。

## 已接入与职责

- 对话、模型切换、工具与审批、Agent 管理、Auto 设置与总览。
- Node 终端、工作区文件查看/编辑/上传、现有浏览器会话只读查看、Manage 工作组。
- Node 设置通过 Rust transport 保存；Desktop 的连接、通知和更新偏好单独保存。
- 首配只配置当前 Node，称呼可选，移除外观步骤；未首配或离线仍可打开 Desktop 设置。
- 关闭窗口隐藏到托盘，重复启动恢复原窗口；退出 Desktop 后 Node 继续运行。
- 本机 Node 启停核验路径、配置、PID 和进程创建时间，不认领外部 Node。

设置复用 node/webui/frontend 内的 Vue 组件源码，Node 构建不依赖前端产物。当前浏览器资源是 Node 侧会话的只读展示；人工接管和地址栏控制尚未实现。

## 连接模式

| 模式 | 行为 |
| --- | --- |
| local（默认） | 连接本机 Node，可启动与监护本安装拥有的 Node。 |
| tunnel | 连接用户已经建立的 SSH 隧道的 loopback HTTP 端口；不会启动、恢复、停止或重启本机 Node，不从远端 Node 安装本机更新。 |

两种模式都只接受 loopback HTTP origin，不接受公网/LAN 地址、URL 凭据、路径或 query。SSH 隧道由用户在 Desktop 外建立；当前没有自动 SSH 管理或 Node 通用 Bearer 用户认证。端口转发必须受访问控制，不能公开裸 Node API。

隧道模式的 Agent 工作目录、终端、文件和浏览器属于连接的 Node。工作目录输入 Node 侧路径，原生 Node 目录选择器禁用。Desktop 附件上传后转为 Node 引用，不把本机路径当作远端文件。

## 配置存储

Desktop JSON 默认位置：

- Windows：%APPDATA%/DAgents/desktop/settings.json
- macOS：~/Library/Application Support/DAgents/desktop/settings.json
- Linux：$XDG_CONFIG_HOME/dagents/desktop/settings.json（缺省使用用户 .config）

字段为 connection_mode、node_endpoint、notification_enabled、update_check_enabled、update_check_interval_seconds。旧 JSON 缺少模式时按 local 读取。保存后重启 Desktop 生效；读取失败时禁止覆盖默认值，保存失败保留草稿。

Node config.yaml 只保存 listen。Node 名称、使用者称呼、LLM、Manage、工具和运行设置留在 Node 的 .runtime 数据库。Manage 凭据不写入 Desktop JSON。

--config 指定本机 Node bootstrap，--desktop-config 指定 Desktop JSON；DAGENTS_HOME 是安装根，DAGENTS_DESKTOP_CONFIG 可指定 Desktop JSON。包装命令还支持 DAGENTS_CONFIG，它不是 Desktop 偏好来源。

## 构建与开发

在仓库根目录准备 Node 二进制和 bootstrap，再启动 Desktop：

```powershell
New-Item -ItemType Directory -Force bin | Out-Null
go build -o bin/dagents-node.exe ./node/cmd/dagents-node
Copy-Item packaging/agent-client/config.example.yaml config.yaml
$env:DAGENTS_HOME = (Resolve-Path .).Path
npm ci --prefix desktop/tray-tauri
Set-Location desktop/tray-tauri
npm run tauri:dev
```

只运行 Vite 是排版预览，不能伪装真实 Node。内嵌前端的候选构建：

```powershell
npx --no-install tauri build --debug --no-bundle --features tauri/custom-protocol
```

Release 使用 scripts/ci/build_dagents_shell_tauri.sh，正式 Windows 包由 Inno Setup 包装。完整能力以 Win11 x64 为主要目标；浏览器要求 Win11 / Server2019+，Win10 只保留基础能力。最低要求不等于所有目标平台已经验收。

## 生命周期命令

dagents-shell.exe 提供 --desktop-status、--node-status、--owned-node-status、--start-owned-node、--stop-owned-node [--require-idle]、--quit-desktop。控制入口先于 GUI 初始化；设备连接不创建另一份 runtime。

隧道模式不允许本机 Node 启停。安装器 require-idle 在没有存活本机 owner 时可以无操作完成；若残留此前本机模式启动的受管 Node，需切回本机模式处理，不能用远端状态判断本机进程。

升级下载后再次检查 Node readiness；当前无 Node 原子维护锁，检查和停止之间仍有新回合竞态。隧道模式的本机更新安装禁用，远端 Node 与本机 Desktop 的平台发布和升级需分别设计。

## 验证与限制

运行 npm run test:ui、cargo test --offline、cargo fmt --check。CI 见 [tauri-shell.yml](../../.github/workflows/tauri-shell.yml)。

实际证据及缺口见 [日常接入记录](../../docs/design/desktop-product/daily-use-integration.md)、[生命周期验收](../../docs/design/desktop-product/audits/2026-10-13/lifecycle-install/README.md)、[Node API 文档](../../docs/api/node-integration.md)。自动 SSH 管理、直连远程认证、浏览器人工接管、丰富文件查看器和多平台安装签名尚未完成。缺少 ISCC 时不能以构建 Tauri EXE 代替安装、升级和卸载验收。
