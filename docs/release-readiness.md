# v1.0.0 发布验收清单

评估日期：2026-10-26。最近正式发布仍为 `v0.11.0`（2026-09-11）。v1.0.0 发布 PR #359 已于 2026-10-26 合入 `main`，提交为 `d416c5c93b1dcbf650fb1e5d9ee4393842ac39a1`，PR 检查全部通过。用户要求本次先同步 `main → dev`，再创建正式 `v1.0.0` tag；同步 PR 与 tag 发布资产核对仍待完成。既有隔离验收使用候选 Node 与 Browser 二进制完成真实 DeepSeek 对话、Browser 本地页面导航/DOM/截图，并在对应源码构建的 Tauri Desktop 中确认 Browser 资源正确显示。候选包体积与历史验收记录见[本轮发布构建记录](design/desktop-product/audits/2026-10-22/v1-release-build.md)。用户明确要求本次不再验证最终安装/升级流程与 Auto 耐久；两项仍未验证，按用户决定不作为本次放行阻断项。

## 发布判断与范围

本次目标是 **v1.0.0：DAgents 首个稳定版**，包含独立 Windows Desktop、API-only Node、资源工作台及 Auto Agent。候选元数据已通过 `prepare_release.py` 更新；合并后的 CI、CodeQL、完整候选包构建、真实 Provider 对话及 Browser/Desktop 资源联动均已通过。正式发布 PR 的独立审查、合并和 tag 产物核对仍须完成。未关闭的 P0 项在下文逐项列出，不能仅凭开发分支 CI 通过视为正式发布完成。

10 月 20 日完成的临时标准用户 QA 关闭了隔离 QA 包的初装、升级、卸载保留数据和重装恢复核心路径；它不等于最终发行候选放行。该 QA 包的安装器版本为 `0.11.0.1`，其中 payload 和 bundle `VERSION` 仍为 `0.11.0`，不是发布候选。用户已决定本次不再验证最终候选安装/升级/恢复流程，也不做 Auto 耐久测试；这两项保留为未验证记录，不列入本次阻断项。真实 Provider 与本机 Browser 联动已在隔离候选环境完成；最终 tag 产物仍须按发布流程生成并核对。10 月 17 日的完整包基于更早源码，不能作为当前资源改动的最终发行候选。

本版覆盖 Windows Desktop、本机 Node 与可选 Manage 协作。Desktop 固定连接同机 Node，不保存远程 Node 或 SSH 隧道配置；Node 不再提供 `/ui/`。多个 Desktop 可各自使用本机 Node，不引入多人账号与 runtime 隔离。远程主机操作通过 Node 的 SSH/远程终端工具完成。浏览器资源当前是 Node 会话的只读观看，不承诺人工接管或本地嵌入式浏览器。

2026-10-17 用户已确认：下一版 Windows 仅发布 x64 完整包，暂停 x86 完整包。核查发现锁定的 greenlet 缺少 win32 wheel，Playwright 包内驱动为 x64，不能把 x86 Go/Desktop 编译成功当作完整产品可用。历史 x86 更新仍严格按平台选择，不能回退到 x64。Linux 发布 Node/Client/Browser 服务包，不发布 Linux Desktop。

## 已完成的主要能力

| 能力 | 现有证据与边界 |
| --- | --- |
| 独立 Desktop、配置分离、首配 | Node API-only、Desktop 本机偏好与 Node 业务配置分开；两步首配、离线本机设置已原生验收。见 [产品收束记录](design/desktop-product/audits/2026-10-14/product-closeout/README.md)。 |
| 日常对话、模型切换、附件、停止 | 真实模型读写文件、审批、切换模型、上传、停止后继续已验证。见 [日常回归](design/desktop-product/audits/2026-10-15/daily-use/README.md)。 |
| 资源工作台与终端生命周期 | 资源页消息坞、输入栏过渡、工具卡片、终端恢复、仅关闭展示/断开/结束的区分已有原生与 transport 证据。见 [10 月 16 日记录](design/desktop-product/audits/2026-10-16/)。真实 SSH 仍须补测。 |
| 工具上限与失败收束 | 普通 Agent 达到工具轮次上限后的无工具收尾、最后 Bash 失败、混合结果显示和刷新恢复已有真实模型及原生窗口验证。见 [专项记录](design/desktop-product/audits/2026-10-16/turn-limits-and-failed-bash/README.md)。 |
| Auto、Todo、Dreaming、工作组 | 真实唤醒、Todo 审批、Dreaming 更新经验与清空活动上下文、Supervisor→worker→汇总已验证；非多日耐久或所有故障场景通过。见 [Auto 与工作组](design/desktop-product/audits/2026-10-12/auto-workgroup/README.md)。 |
| 空闲后新上下文 | 自动回归、出站请求捕获、真实 Tauri + Mock 两轮对话、归档及重新加载后的分隔线已通过。见 [上下文边界](design/idle-conversation-boundary.md)。 |
| 资源刷新与消息展示 | 条件读取、草稿/版本冲突保护、资源链接、显式隔离 HTML/SVG、预览实际高度、页面过渡与紧凑消息卡已实现；安全与组件回归、Windows Tauri 原生专项通过。见[资源页收束](design/desktop-product/audits/2026-10-19/resource-layout-polish.md)及本轮发版验收。不是实际 SSH、Browser 发行包或标准用户安装验收。 |
| Desktop 本地 Node 连接 | Desktop 固定连接 `127.0.0.1:18765`，Browser sidecar 固定连接 `127.0.0.1:18766`；移除了远程 Node/SSH tunnel 设置和生命周期分支。真实 DeepSeek 对话已通过隔离 Node 写入并由原生 Desktop 收到、渲染；版本/API 能力握手仍是后续 P0 缺口。 |
| 配置密钥与安装链路修复 | 密钥保留和 MCP 工具开关/JSON 往返已有真实 Tauri + 合成 HTTP 服务验证；活动 Agent 的模型 ID 引用保护已通过 API/Store/组件回归。管理员开发机和临时标准 Windows 用户均完成各自 QA 范围内的安装生命周期验证；10 月 20 日标准用户轮次覆盖静默初装、Mock 对话、升级、卸载数据保留与重装恢复。此证据仅针对 QA-only `0.11.0.1` 安装器；缺失 WebView2、文件占用、不可写路径、备份恢复故障注入、自启开启状态保留、旧发行迁移、真实 Provider/SSH 和长时 Auto 仍未验收。 |

## 本轮已关闭的工作

- [x] 活动普通/Auto Agent 使用中的模型配置禁止直接改名/删除，返回中文 409；无效新绑定拒绝持久化，相关写路径使用同一锁。
- [x] 修复 Bash 等待工作区写锁时收到 UI 取消却返回 `workspace_busy`；增加确定性回归，针对取消边界重复 20 次通过，Go 1.26.9 五模块全量回归通过。
- [x] v0.11.0 持久化数据结构核对、脱敏恢复与配对密钥/缺失密钥回归；落地[迁移指南](user/migrate-from-v0.11.md)。不把数据 Store 测试计作安装迁移。
- [x] 原生 LLM 空 Key 探测→保存→重开探测、MCP 工具开关→JSON 保存、遮罩凭据往返及无效草稿失败验证；[配置验收记录](design/desktop-product/audits/2026-10-17/configuration-and-data.md)。
- [x] Desktop 自定义输入栏补齐 `/clear`、`/compress`、`/thinking` 和 `/upload` 分发；主对话与资源页悬浮输入栏复用共享命令解析，未知命令显示错误而不提交给模型。`/compress` 保留正在显示的流式回复，`/clear` 清除待落盘的乐观消息。该修复现已随 PR #355 合入 `dev`；针对当时 82 项 Desktop UI 测试和生产构建通过。
- [x] 两个设计原型的兼容依赖修复，分别 build 与 4 项测试通过、npm audit 0；确认 `glib` 只在非发行的 Linux Desktop 图中，Windows x64/x86 不包含它。
- [x] 移除 Desktop 未使用的 `zip 7.2.0` 及其专属依赖，关闭三项 RustSec 漏洞；锁文件扫描为 0 漏洞、7 项明示警告，CI 增加固定版本的无忽略项审计。
- [x] 正式与手动发布工作流仅组装 x64 完整包；更新查询增加 `platform_supported`，无匹配架构时明确提示且不下载其他架构。旧 Manage/Node 缺字段默认允许原有行为；覆盖 Node 失败状态、托盘提示和 CLI 检查分支。
- [x] 固定产品源码 `29fc308`，重建 Go 1.26.9 Node/Client、Tauri Desktop 和实际 Browser 输入，完成 x64 完整 bundle 与 QA/正式 Inno 安装器编译。主线程独立核对全部 51 个文件和两种安装器哈希；[候选记录](design/desktop-product/audits/2026-10-17/release-candidate.md)。版本仍为 `0.11.0` 开发验收值，没有发布或执行安装。
- [x] 发布说明补齐 Desktop/API-only、配置分离、资源、工具、浏览器协议和安装行为；README 增加当前原生截图，文档入口与路线图同步。

## P0：进入稳定版前必须关闭

以下是本次建议的发布门槛，不表示这些能力都尚未实现。每项以最终候选提交和实际包为准，记录环境、包哈希、步骤与结果；失败项修复后重测受影响链路。

清单标记：`[x]` 已验证，`[ ]` 尚待完成，`[~]` 用户明确豁免、仍未验证且不作为本次放行阻断项。

- [ ] **`main → dev` 发布后同步。** 发布 PR #359 已合入 `main`（`d416c5c93b1dcbf650fb1e5d9ee4393842ac39a1`）。当前正在准备 `main → dev` 同步 PR；由于 Desktop Composer 状态提示与 `dev` 后续 UI 变更交叠，需在同步中保留 `main` 已验收的输入框上方等待状态胶囊，并通过同步 PR 检查后合入。
- [x] **已复现的 Desktop 首次启动崩溃。** 同一发行候选在 stderr 正常时存活，关闭读端后复现 `0xc0000409`；诊断版捕获 `failed printing to stderr`、错误码 232 和 `events::sync_agents` 的可靠调用栈。修复统一处理进程输出 I/O 失败，并补齐同类 CLI stdout 问题；51 项 Rust 测试通过，正式模式构建通过闭管道进程与 CLI 输出验证。stderr 修复还完成 120 秒闭管道运行和原生内嵌 UI 交互。原始 dump 的完整业务栈未恢复；受控复现与原异常线索一致，但不把其他启动故障一并视为通过。各构建哈希及验收范围见[专项记录](design/desktop-product/audits/2026-10-19/desktop-startup-panic.md)。新代码的 PR 集成和最终包验收仍由相邻门槛约束。
- [x] **标准用户 QA 核心初装、升级、卸载保留与重装恢复。** 临时标准 Windows 用户在开发机上使用 QA-only `0.11.0.1` 安装器完成静默初装、原生首配与 Mock 对话、快捷方式退出/重开持久性、`0.11.0 → 0.11.0.1` 静默升级、卸载数据保留及重新安装后 Agent/历史恢复；最终测试卸载已完成。安装器版本为 `0.11.0.1`，但 bundle/payload `VERSION` 为 `0.11.0`，不可视为发行候选。证据、快照与哈希见[标准用户安装恢复审计](design/desktop-product/audits/2026-10-20/standard-user-install-and-recovery.md)。

  该项只关闭上述核心路径。缺失 WebView2、文件占用、不可写路径、备份恢复故障注入、自启开启状态保留仍未验证；真实 Provider/SSH 和至少 24 小时 Auto 耐久分别由后续门槛覆盖。该 QA 不测试外部 Provider 密钥，只使用 Mock，也不证明最终冻结包或发布包已通过安装验收。不得声称已有应用级事务回滚。操作见[安装升级清单](../packaging/windows/install-upgrade-acceptance.md)。

  10 月 19 日管理员开发机 QA 已按用户授权完成 `95e3f98c` 基线轮次，随后对包含启动修复的 `6a0107c2` x64 候选执行初装、原生首配/创建 Agent/Mock 对话、同产品 payload 的 `0.11.0 → 0.11.0.1` 合成安装器升级、重开历史恢复、运行中卸载和最终校验。新一轮升级前、升级后、卸载前后四份九数据库与历史文件快照完全一致；Node/Agent 身份、两条完整消息、Mock 绑定及首配状态恢复，正式安装受检设置与另一开发 Node 未变。本轮自动启动未发生策略拒绝。该记录是管理员 QA，不能代替标准用户 QA 或最终发布候选验收，见[最终候选记录](design/desktop-product/audits/2026-10-19/release-candidate-6a0107c2.md)。

- [~] **最终候选安装/升级与 v0.11.0 迁移（用户豁免）。** 用户明确要求本次不验证安装流程，因此不再执行最终候选的初装、升级、卸载/重装或旧发行迁移演练。这些流程仍未由最终候选验证；10 月 22 日标准用户安装器尝试未成功，没有安装候选文件，也未触碰当前 Node。既有 QA-only `0.11.0.1` 验收仅作为实现证据。具体状态见[本轮构建记录](design/desktop-product/audits/2026-10-22/v1-release-build.md)。
- [x] **LLM 配置往返与真实 Provider 对话。** 原生窗口已通过空 Key 探测/保存/重开探测、MCP 工具开关/高级 JSON/凭据往返，失败草稿不会覆盖已存配置。候选包运行的隔离 Node 使用已保存的 DeepSeek profile 完成了真实对话，并由隔离 Tauri Desktop 收到、渲染回复。此验收未配置外部 MCP server；清空/替换 Key、取消和探测失败仍主要由自动回归覆盖，不代表外部 MCP 集成已验收。
- [x] **配置 ID 改名的 Agent 引用。** 活动 Agent 引用中的 ID 改名/删除返回 `409 llm_profile_in_use`，列出 Agent 并要求先修改绑定或删除 Agent；软归档 Agent 不阻止变更。拒绝发生于设置和密钥持久化前，创建/PATCH 还会拒绝不存在的目标 ID，详见 [API 契约](api/node-integration.md) 与配置验收记录。
- [~] **Auto 耐久（用户豁免）。** 用户明确要求本次不做 Auto 测试，因此跳过不少于 24 小时的最终候选运行/恢复测试；相关耐久范围仍未验证，不作为本次放行阻断项。既有唤醒、Todo、Dreaming、工作组和恢复专项证据保留，不表述为本次候选已通过耐久测试。
- [x] **本机 Browser 与 Desktop 资源联动。** 本轮完整包 run `39917868139` 的 Windows x64 Node、Browser 二进制通过健康/协议检查，启动 Chromium 并完成本地页面导航、DOM 读取与截图；真实 Browser 工具链结束后，隔离 Tauri Desktop 成功打开只读 Browser 资源并显示标题与页面内容。外站与升级后状态未覆盖。Linux Node 按独立服务包验收，不属于 Desktop 远程连接矩阵。Windows x86 完整包已按用户决策暂停。
- [ ] **正式版本资料审查与 tag 发布产物。** v1.0.0 版本元数据、README、手册、路线图和 changelog 已更新；`validate_release.py --version 1.0.0` 通过，完整候选打包工作流 run `39917868139` 成功。CI 产物元数据显示 Windows x64 安装器约 391 MB、Linux 助手包约 365 MB、Manage 离线 bundle 约 426 MB；Manage bundle 已移除镜像已 seed 的重复 Linux 安装归档。PR #359 已合入 `main`，同步合入 `dev` 后将在 main 发布 `v1.0.0`；tag 构建后须核对文件版本、许可证、`SHA256SUMS`、`release-manifest.json` 与更新选包/下载链路。当前工作流不对 Windows 安装包做 Authenticode 签名，v1.0.0 若沿用现行发布配置将显示 Windows SmartScreen 提示；发布资产和说明必须明确这一点。

## 依赖告警的处置范围

2026-10-17 读取 GitHub Dependabot：开放 21 项，其中 20 项位于两个设计原型的 npm lockfile（14 high、6 medium），另 1 项为 Desktop Cargo.lock 的 `glib` medium 告警。它们不能直接等同于 Windows 发行包的 21 个运行时漏洞。

- [x] 本轮依赖审计汇总。生产前端三套 audit 为 0，Python 主依赖及 Browser 既有明确例外清单审计通过；Go 1.26.9 五模块扫描为 0 可达漏洞。默认 Go 1.26.5 曾检出标准库问题，禁止用于候选构建。移除未使用的 ZIP 库后，完整 RustSec 锁文件扫描为 0 漏洞、7 项警告；使用本轮已成功获取的数据库，后续刷新失败限制见[依赖记录](development/dependency-readiness-2026-10-17.md)。最终发布 CI 仍须重新联网审计，非发行 Linux 图和未维护依赖警告保留，不能声称所有依赖无告警。
- [x] 核实 `glib`：Windows 目标不包含该依赖；Linux GTK3 依赖族需协调上游升级，未做不兼容的 lockfile 强制替换，不新增忽略项。
- [x] 两个设计原型 Vite 及兼容传递依赖已更新，各自 npm audit 为 0，build 与 test:sites 通过。

10 月 19 日补充：生产前端三套 npm audit 为 0；Python 主锁无已知漏洞，Browser 锁沿用工作流现有明确忽略项；本地 RustSec 使用缓存数据库为 0 漏洞、7 警告。Go 1.26.9 五模块扫描无可达漏洞，Node 所需的 `golang.org/x/crypto` 模块包含 `GO-2026-5932`（未维护的 OpenPGP 包），当前代码没有导入该包或调用对应路径，不应写成“所有依赖无告警”。正式候选仍以冻结提交的联网 CI 审计为准。

2026-10-22 复核 GitHub Dependabot：仍有 1 项未关闭的中危 `glib 0.18.5` 告警（GHSA-wrw7-89jp-8q8g）。Windows x64 Desktop 的发布目标不包含该 Linux GTK 依赖，Linux 发行包也不包含 Desktop；该告警不在 v1.0.0 已发布资产的运行依赖范围内，但必须继续跟踪上游兼容升级，不能表述为全仓依赖零告警。

## P1：体验收尾，可随候选验收并行

- [ ] 小窗口与 125%/150% DPI：设置边缘渐隐、长模型名称、长工具名称、工具卡、附件、审批卡和悬浮消息坞不截断或互相遮挡；键盘焦点、减少动画偏好可用。现有最大化/还原检查不能替代完整缩放矩阵。
- [ ] 真实外部 MCP、Manage 反馈提交→管理员处理→回复同步，以及工作组审批/取消/恢复做一次组合冒烟；发布说明仅承诺已验证范围。若发现阻断或数据问题，提升为 P0。
- [x] 统一当前文档入口。Desktop 资料包、早期 MVP 和设计入口已加当前状态指针，路线图移除过时的未落地 Goal/多用户主线；历史验收事实保留。

## 不作为本版前置条件

浏览器人工接管与可交互嵌入式页签、远程 Desktop→Node 连接、LAN/WAN Node 档案、多用户/租户隔离、macOS/Linux Desktop、新事件总线/工作流系统，以及再次全面重写 UI/runtime，均留到后续独立立项。当前产品边界是 Desktop 只连接同机 Node；远程 SSH 终端是 Node 的工具能力，不改变 Desktop 与 Node 的本地连接关系。

## 执行顺序

1. PR #356 已合入 `dev`（`73f28d8a8129816eb70cc2788019236d111ed5a5`）；合并后 CI、CodeQL 和基于该提交的完整手动包构建均通过。
2. 候选包的真实 Provider 对话与 Browser/Desktop 联动已在隔离 runtime 完成；用户明确豁免最终安装/升级验收与 Auto 耐久，仍未验证但不作为本次放行阻断项。未配置外部 MCP server，不声称外部 MCP 验收通过。
3. 创建 `main → dev` 同步 PR，保留已发布 main 的修复并解决 Composer UI 重叠；检查通过后合入 `dev`。
4. 确认 `main` 仍指向 `d416c5c93b1dcbf650fb1e5d9ee4393842ac39a1`，校验 `v1.0.0` 元数据后创建并推送 tag。
5. 等待官方 release workflow 完成，核对正式发布资产、版本、许可证、`SHA256SUMS`、`release-manifest.json` 与下载/更新选包链路。

当前 v1.0.0 发布 PR #359 已合入 `main`，真实 Provider 与 Browser/Desktop 联动验收已完成。下一步是完成 `main → dev` 同步 PR，再在 `main` 创建 `v1.0.0` tag 并核验发布资产。用户豁免项仍标记为未验证，不把它们写成通过。

2026-10-20 补充：已完成打包 Node → Browser → 原生 Desktop 整链，以及管理员开发机上的正式安装任务开关、官方 v0.11.0 普通 Agent 冷备份迁移、合成加密 Key 恢复和运行中卸载。验收发现并修复中文路径卸载损坏 PATH、已有 policy 导致静默安装挂起两项问题，PR #352 已合并至 `dev`（`33106ead`），PR 检查通过；失败候选和成功修正候选分别留证。另在临时标准 Windows 用户下完成 QA-only `0.11.0.1` 安装器的静默初装、Mock 对话、快捷方式重开、`0.11.0 → 0.11.0.1` 升级、卸载保留数据及重装恢复，详见[标准用户安装恢复审计](design/desktop-product/audits/2026-10-20/standard-user-install-and-recovery.md)。该 QA 的 payload/bundle `VERSION` 仍为 `0.11.0`，不属于发布候选；该日的未完成项和放行判断为历史状态，当前以本页 10 月 22 日更新为准。

历史验证（2026-10-20）：从 `642cd15` 建立的 `codex/release-0.12.0-rc` 候选同步过正式版本元数据。Windows 本地验证通过：共享 UI 源码 534 项、Desktop UI 77 项、Python 202 项、Rust 51 项，Go 测试/vet/Staticcheck/构建通过；Manage 构建与 lint 通过，Desktop UI 构建通过。release metadata 单测、当时的 `validate_release.py --version 0.12.0` 和隔离安装器契约测试 30 项通过。候选提交 `80feadbb` 曾通过官方手动打包和安装冒烟；该包不含之后的斜杠命令修复且 Authenticode 未签名，不作为 v1.0.0 发行证据。

历史验证（2026-10-20）：发现并修复 Desktop Composer 将 `/clear`、`/compress`、`/thinking`、`/upload` 当作普通消息提交的问题，并修正上下文状态边界。Desktop UI 82 项、共享 UI 源码 534 项和 Desktop Vite 生产构建通过；修复现已包含在 PR #355 与当前 `dev` 基线中。此前手动打包工作流只用于中间候选，不代表 v1.0.0 发行包验收。

2026-10-26 更新：v1.0.0 发布 PR #359 已合入 `main`（`d416c5c93b1dcbf650fb1e5d9ee4393842ac39a1`），PR 检查全部通过。按用户要求先完成 `main → dev` 同步，再发布正式 tag；目前同步 PR 检查/合并与 tag 构建、资产核对仍待完成。用户豁免本轮最终安装/升级验收与 Auto 耐久（未验证，不阻断）。
