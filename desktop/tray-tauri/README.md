# DAgents Desktop（Tauri）

当前唯一的 Agent 人机入口是内嵌前端的 Tauri 2 Desktop。Node 只提供运行 API，不提供 `/ui/` 页面；共享 Vue 源码不能独立启动为网页应用。已退休 Go Shell 的模块和构建入口已移除。一个 Desktop 固定连接同机 Node，不支持连接远程 Node。

## 已接入与职责

- 对话、模型切换、工具与审批、Agent 管理、Auto 设置与总览。
- Node 终端、工作区文件查看/编辑/上传、现有浏览器会话只读查看、Manage 工作组。
- Node 设置通过 Rust transport 保存；Desktop 的连接、通知和更新偏好单独保存。
- 首配只配置当前 Node，称呼可选，移除外观步骤；未首配或离线仍可打开 Desktop 设置。
- 关闭窗口隐藏到托盘，重复启动恢复原窗口；退出 Desktop 后 Node 继续运行。
- 本机 Node 启停核验路径、配置、PID 和进程创建时间，不认领外部 Node。

Desktop 工作台、设置界面与 Tauri 启动入口统一位于本目录的 `src/`，跨产品复用的图标、品牌与展示 helper 位于仓库根目录 `shared/`。Node 只提供 API，构建不依赖前端产物。当前浏览器资源是 Node 侧会话的只读展示；人工接管和地址栏控制尚未实现。

## 本机组件地址

Desktop 固定访问 Node `http://127.0.0.1:18765`；Node 管理的 Browser sidecar 固定使用 `http://127.0.0.1:18766`。用户设置中不保存 endpoint、连接模式或隧道配置。Node bootstrap YAML 仍保留 listener 字段供服务部署和测试启动使用；Desktop 不会根据该字段切换连接目标。

## 配置存储

Desktop JSON 默认位置：

- Windows：%APPDATA%/DAgents/desktop/settings.json
- macOS：~/Library/Application Support/DAgents/desktop/settings.json
- Linux：$XDG_CONFIG_HOME/dagents/desktop/settings.json（缺省使用用户 .config）

Desktop JSON 只保存 notification_enabled、update_check_enabled、update_check_interval_seconds。旧 JSON 中的 node_endpoint 与 connection_mode 会被忽略，保存时不会再写回。保存后重启 Desktop 生效；读取失败时禁止覆盖默认值，保存失败保留草稿。

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

升级下载后再次检查 Node readiness；当前无 Node 原子维护锁，检查和停止之间仍有新回合竞态。

## 验证与限制

运行 npm run test:ui、cargo test --offline、cargo fmt --check。CI 见 [tauri-shell.yml](../../.github/workflows/tauri-shell.yml)。

实际证据及缺口见 [日常接入记录](../../docs/design/desktop-product/daily-use-integration.md)、[生命周期验收](../../docs/design/desktop-product/audits/2026-10-13/lifecycle-install/README.md)、[Node API 文档](../../docs/api/node-integration.md)。自动 SSH 管理、直连远程认证、浏览器人工接管、丰富文件查看器和多平台安装签名尚未完成。缺少 ISCC 时不能以构建 Tauri EXE 代替安装、升级和卸载验收。
