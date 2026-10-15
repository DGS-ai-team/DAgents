# Desktop 安装与升级验收

日期：2026-10-14。范围：当前源码的 Windows x64 本机交付路径。Luna 编码，主线程审查、构建准备、真实安装与原生窗口验收。未发布、未提交或推送。

## 环境与隔离

宿主为 Windows 11 x64，当前账户具有管理员权限，但安装器实际运行在当前用户、非 administrative install mode。本机没有测试 VM，Windows Sandbox 未启用。因此采用仅编译期启用的 `IsolatedQaBuild=1`：独立 QA AppId，禁用快捷方式、登录自启、PATH 改动、自动运行和生产桌面快捷方式删除；Desktop 设置位于测试安装目录内。Windows 与 WebView2 检查、配置种子、写权限检查、升级退出/停 Node、文件替换和卸载保留逻辑均继续使用真实实现。

安装目录在 `.runtime-test-install-delivery/fixture/DAgents QA 安装 Fixture`，包含空格和中文。Node 使用 18972；没有读取或复制生产 LLM 密钥。生产 Run key、用户 PATH、Desktop 设置与桌面快捷方式仅保存哈希，在各阶段比较，不输出内容。

编译器从 [Inno 官方下载页](https://jrsoftware.org/isdl.php) 获取 6.7.3，Windows Authenticode 校验为有效 Pyrsys B.V. 签名。编译器临时安装在工作区工具目录，验收后已卸载；未修改系统默认工具链。

## 本轮修复

1. Windows 版本检查错误地把 `GetWindowsVersionEx` procedure 当成 Boolean function。真实 ISCC 编译发现问题后修正，正式与 QA 分支均编译成功。
2. 安装目录权限探针把 `GenerateUniqueName` 的目录参数误拼上文件前缀，实际安装在 PrepareToInstall 返回 7。改为使用真实 `{tmp}` 目录生成文件名，仍以 `CopyFile(..., True)` 测试目标目录，不降低权限保护。[官方 API 说明](https://jrsoftware.org/ishelp/topic_isxfunc_generateuniquename.htm)
3. 构建参数版本可能与 bundle `VERSION` 不一致。构建脚本新增必需检查，在 staging/编译前拒绝缺失或不一致版本；QA manifest 记录 `version` 和 `bundleVersion`。用错误版本构建的拒绝测试通过。
4. 增加显式 QA 编译隔离与 staged runner；递归 staging 路径增加工作区、非链接与输入/目标分离检查。正式分支仍使用原 AppId、DAgents 菜单组及原有系统集成行为。

Browser 真实打包的资源缺失另被确认是 MAX_PATH：文件存在，283 字符路径下 Python 文件检查失败；临时将仓库映射到短驱动器后，同一依赖、官方 hook 和资源完整收集，onefile 构建成功。映射已解除，没有伪造资源或删减 Chromium 包。

## 构建结果

真实 Node、Client、Desktop 与 Browser 已完整组装，无 `SKIP_BROWSER`。Browser EXE 为 380,197,502 bytes，`--help` 通过。Node 为当前 Go 构建，Desktop 为内嵌前端 debug 构建；截图使用 `tauri.localhost`，不依赖 Vite server。

| 产物 | 当前结论 |
| --- | --- |
| QA `0.11.0.1` | 完整编译，并用于首装实测。 |
| QA `0.11.0.2`，版本一致性修正前 | 安装器 metadata 升级、原生重开、卸载、数据保留实测通过；之后发现包内 `VERSION` 仍是 `.1`，不作为最终候选。 |
| 最终 QA `0.11.0.2` | 重新 assemble 与编译，manifest 中 installer/bundle 版本皆 `.2`；完整复测未执行，见下方限制。 |
| 最终 normal `0.11.0.2` | 正式配置分支完整编译成功，未安装或发布。 |
| x86 | 本轮未实际构建与安装；发布矩阵保留不能视作验收通过。 |

最终 QA SHA256：`16122E4A234F14FF197C302117DCDBFB7F739A65B396A4A64FA20093C216C8E2`。

最终 normal SHA256：`8A6F1DB3D2F288D401D864D346B51545B86D4330716CDF04D68486078E68415B`。

两版 QA 使用同一当前源码的应用二进制，候选四段版本用于测试安装协议，并非旧发行版迁移。二进制版本仍含开发标识；正式分支产物仅验证编译，不是可直接公开发布的 release build。

## 实测矩阵

下面通过项属于版本一致性修正前的完整隔离循环，不能替代最终包复测。

| 场景 | 结果与证据 |
| --- | --- |
| 首装到中文/空格目录 | 成功，Node/Client/Browser/Desktop 文件哈希与 manifest 匹配；bootstrap 和 policy 已落地。[Install](evidence/install-stage.log) |
| 安装后原生 Desktop | 正常启动、自动启动受管 Node、连接 18972，并显示两步首配。[截图](native/installed-desktop-first-run.jpg) |
| 运行中升级 `.1` → `.2` | 安装器通过真实 CLI 退出 Desktop、停止受管 Node，随后替换文件；安装记录版本变为 `.2`。[Upgrade](evidence/upgrade-stage.log) |
| 配置与数据保留 | 升级前后 bootstrap、policy 修改、sentinel、Desktop JSON 字节一致；Node SQLite 只读逻辑摘要一致。初始化时正常发生的 bootstrap 注释归一化不作为安装器改写。 |
| 升级后重开 | 等待就绪后 Node 编号、通过 API 保存的“安装验收 Node”名称、onboarding 状态恢复，直接进入工作台。[截图](native/after-upgrade-desktop.jpg) |
| 运行中卸载 | 真实 uninstaller 退出 Desktop、停止受管 Node，删除应用二进制，保留上述配置、数据库、policy 与 sentinel。[Uninstall](evidence/uninstall-stage.log) |
| 卸载后复查 | QA 注册项消失；18972 无监听、GUI 已退出，保留 44 个 runtime 文件；生产状态哈希未改变。[Verify](evidence/verify-stage.log) |
| 缺失协议 marker 的安装目录 | 模拟未标记安装，准备阶段返回 7、拒绝调用旧 Shell 与替换文件；原 EXE/config 哈希未变，无 QA 注册项创建。[拒绝日志](evidence/guard-inno.log) |

失败日志和测试状态保留在忽略的 fixture 目录。早期不含 Browser 的编译烟测包已移除，避免误当完整包使用。

## 复验入口

`packaging/windows/test-isolated-installer.ps1` 提供 Install / Seed / Upgrade / Uninstall / Verify 阶段。使用 PowerShell 7+ (`pwsh`) 运行；这只是 QA 工具依赖，不改变 Node/Desktop 的运行要求。它只接受工作区内含空格/中文的安装路径与有效 QA manifest，验证包的 macro/AppId/SHA、安装注册项和 uninstaller 身份，并要求升级版本严格增长。各阶段产生 Inno 日志，失败保留状态，不自动删除未知目录。

先按对应版本 assemble 完整 bundle，再调用 `INSTALLER_QA=1` 的安装器构建脚本。两个版本需各自包含一致的 `VERSION`。首装和 Seed 后手动启动测试 Desktop，等待 Node 就绪并通过测试 Node API 设置无凭据档案；然后 Upgrade，再重开与 Uninstall，最后 Verify。勿执行 normal 包替代 QA 包。

## 尚未通过的项目

- 最终版本一致包的完整复测组合命令被自动审批审核拒绝，仅返回 `blocked by policy`，无具体原因；没有执行，也没有换命令绕过。此前循环通过与最终包编译通过分别记录。
- 打包 Browser 服务与实际 Chromium 启动测试同样被自动审批策略拒绝，未执行；`--help` 和打包成功不代表该运行场景通过。
- 缺少普通用户测试 VM，未覆盖无 WebView2、真正无网络首装、x86、Win10/Server、生产快捷方式、自启、PATH、另一安装并存或外部 Node、旧正式版本迁移。
- 未做文件替换中途故障与完整回滚。未标记安装的安全拒绝不等同回滚测试。
- 本轮未重测真模型、Auto、Dreaming、Manage 和所有资源日常功能；这些属于下一批日常使用回归。

当前结论：已修复实编译与实际安装阻断问题，核心安装生命周期具有隔离实测证据；仍不能据此宣称最终 release 包或全部 Windows 支持范围已验收。
