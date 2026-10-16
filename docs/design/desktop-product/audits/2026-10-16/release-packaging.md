# Desktop 发布与安装流程收束

日期：2026-10-16。状态：源码、单测、原生构建和安装器编译通过；正式安装与升级行为待隔离 Windows 环境验收。

## 发布链路

- Release 与手动打包的构建前检查增加 Desktop UI 的依赖安装、测试和构建，并缓存其 lockfile；纯 Go 构建任务移除无用的 Node.js 安装。
- Windows Desktop 只输出、组装和安装 `dagents-shell.exe`。移除 `SHELL_TAURI_BIN` 隐式回退及重复的 `dagents-shell-tauri.exe` 产物；安装器预检拒绝旧别名残留。
- x64/x86 矩阵、公开安装包名称和更新器选包协议保持一致。修正手动打包非矩阵测试任务引用 `matrix.python_arch` 的错误。
- Packaging Smoke 增加安装器输入保护测试，继续编译 x64/x86 的正式与隔离 QA 安装器。其占位二进制不执行，也不作为发布产物上传。

## 安装与启动

开始菜单只保留 **DAgents Desktop**。开始菜单和可选桌面快捷方式直接启动 `bin\dagents-shell.exe`，工作目录为安装目录，参数为：

```text
--home "<安装目录>" --config "<安装目录>\config.yaml"
```

这避免快捷方式依赖命令行包装器，也让 Node 路径、运行数据、日志和所有权记录固定到本次安装。`--home` 的优先级高于 `DAGENTS_HOME`；未提供时继续按环境变量、可执行文件的 bin 父目录、当前目录解析。Desktop 偏好仍默认保存到 AppData，未与 Node 运行目录合并。

桌面图标、登录自启动是独立任务，初次安装默认不勾选。升级时从当前安装精确匹配的 Run 命令恢复自启状态；Run 值已删除时保持关闭。静默升级也初始化该状态，显式 `/TASKS`、`/MERGETASKS` 参数优先。旧快捷方式仅在目标与参数匹配当前安装时清理；旧 Run 值只在命令精确匹配时删除。`dagents.cmd` 保留为 CLI 入口。

安装仍使用用户可写目录；已有配置、Node 数据库、整个 `.runtime` 和 Desktop 偏好继续保留。WebView2 是外部前提，安装器不下载它。QA 分支继续隔离 AppId、Desktop 偏好和系统集成。

## 本地验证

| 检查 | 结果 |
| --- | --- |
| `cargo fmt --check`、`cargo test --lib` | 通过，42 项测试 |
| `npm run test:ui --prefix desktop/tray-tauri` | 通过，64 项测试 |
| `npm run build --prefix desktop/tray-tauri` | 通过 |
| `npm run tauri:build -- --no-bundle`（Desktop 目录） | Windows 原生 Release 构建通过；包含内嵌 UI |
| assembler contract | 输入来自旧 staging、缺失输入、旧入口不回退及最终布局检查通过 |
| installer contract | 缺失/空 Desktop、旧别名残留、源目录属于 staging（含 Windows 大小写别名）均在覆盖 staging 前拒绝 |
| Shell 语法、QA PowerShell 语法、workflow YAML | 通过 |
| Workflow Action 固定版本、品牌资源检查 | 通过 |
| Inno Setup 6.7.3：x64 正式、x64 QA、x86 正式、x86 QA | 四种安装器均编译成功，QA manifest 安装器哈希校验通过 |

编译证据保存在本地忽略目录 `.runtime-test-release-packaging-20261016/`；四个安装器使用版本 `0.0.0` 和不可执行的占位输入。它们只证明编译链路，不能用于日常安装或发行。编译使用官方 Inno Setup 的 [portable 模式](https://jrsoftware.org/ishelp/topic_technotes.htm)，未执行 DAgents 安装器。

## 尚需环境验收

在一次性 Windows 用户或 VM 中验证初装、已启用/已关闭自启的交互与静默升级、旧快捷方式迁移、卸载、标准用户权限及故障路径；再确认真实 x86 运行和浏览器服务启动。当前本地结果不代表 GitHub CI 已运行，也不替代这些验收。

操作清单见 [Windows 安装与升级验收](../../../../../packaging/windows/install-upgrade-acceptance.md)。
