# 下一版发布收束清单

评估日期：2026-10-19。最近正式发布仍为 `v0.11.0`（2026-09-11）；当前源码版本仍为 `0.11.0`。PR #347、#348 已合入 `dev`，本轮另收束资源自动刷新、消息资源引用、显式 HTML/SVG 展示和资源页布局。本清单区分构建、Mock、原生窗口和正式安装验收，当前复核见[10 月 19 日发版验收](design/desktop-product/audits/2026-10-19/release-acceptance.md)。

## 发布判断与范围

建议下一版定位为 **v0.12.0：独立 Desktop 与 Node API 收束版**，先冻结功能、生成候选包、完成下列验收，再进入正式发布。版本号是本次建议，尚未修改 `VERSION` 或创建标签。当前不宜直接发布稳定版，主要缺口是最终发行包的安装、恢复和外部环境验收。

10 月 19 日的本地代码、自动测试与核心 Desktop 原生流程达到提交开发 PR 的条件；这不等于稳定版放行。最终版本的完整安装包尚未覆盖标准用户初装/升级/恢复、真实远程服务与至少 24 小时 Auto 耐久。10 月 17 日的完整包基于更早源码，不能作为当前资源改动的最终发行候选。

本版覆盖 Windows Desktop、本机 Node、通过用户自建 SSH 隧道访问自己的 Node，以及可选 Manage 协作。Node 不再提供 `/ui/`；多个 Desktop 共享同一使用者的 Node 事实，不引入多人账号与 runtime 隔离。远端文件、终端、浏览器属于 Node 所在机器。浏览器资源当前是 Node 会话的只读观看，不承诺人工接管或本地嵌入式浏览器。

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
| 配置密钥与安装链路修复 | 密钥保留和 MCP 工具开关/JSON 往返已有真实 Tauri + 合成 HTTP 服务验证；活动 Agent 的模型 ID 引用保护已落地并通过 API/Store/组件回归。历史双架构安装器编译证据保留，下一版只验证 x64 完整候选，真实安装行为仍未完成。 |

## 本轮已关闭的工作

- [x] 活动普通/Auto Agent 使用中的模型配置禁止直接改名/删除，返回中文 409；无效新绑定拒绝持久化，相关写路径使用同一锁。
- [x] 修复 Bash 等待工作区写锁时收到 UI 取消却返回 `workspace_busy`；增加确定性回归，针对取消边界重复 20 次通过，Go 1.26.9 五模块全量回归通过。
- [x] v0.11.0 持久化数据结构核对、脱敏恢复与配对密钥/缺失密钥回归；落地[迁移指南](user/migrate-from-v0.11.md)。不把数据 Store 测试计作安装迁移。
- [x] 原生 LLM 空 Key 探测→保存→重开探测、MCP 工具开关→JSON 保存、遮罩凭据往返及无效草稿失败验证；[配置验收记录](design/desktop-product/audits/2026-10-17/configuration-and-data.md)。
- [x] 两个设计原型的兼容依赖修复，分别 build 与 4 项测试通过、npm audit 0；确认 `glib` 只在非发行的 Linux Desktop 图中，Windows x64/x86 不包含它。
- [x] 移除 Desktop 未使用的 `zip 7.2.0` 及其专属依赖，关闭三项 RustSec 漏洞；锁文件扫描为 0 漏洞、7 项明示警告，CI 增加固定版本的无忽略项审计。
- [x] 正式与手动发布工作流仅组装 x64 完整包；更新查询增加 `platform_supported`，无匹配架构时明确提示且不下载其他架构。旧 Manage/Node 缺字段默认允许原有行为；覆盖 Node 失败状态、托盘提示和 CLI 检查分支。
- [x] 固定产品源码 `29fc308`，重建 Go 1.26.9 Node/Client、Tauri Desktop 和实际 Browser 输入，完成 x64 完整 bundle 与 QA/正式 Inno 安装器编译。主线程独立核对全部 51 个文件和两种安装器哈希；[候选记录](design/desktop-product/audits/2026-10-17/release-candidate.md)。版本仍为 `0.11.0` 开发验收值，没有发布或执行安装。
- [x] 发布说明补齐 Desktop/API-only、配置分离、资源、工具、浏览器协议和安装行为；README 增加当前原生截图，文档入口与路线图同步。

## P0：进入稳定版前必须关闭

以下是本次建议的发布门槛，不表示这些能力都尚未实现。每项以最终候选提交和实际包为准，记录环境、包哈希、步骤与结果；失败项修复后重测受影响链路。

- [ ] **最终候选提交的 CI、审查与集成。** PR #348 已合入 `dev`，原产品候选源码为 `95e3f98cbce0a210e41a15590e45d42e353c119a`，其[全量 CI](https://github.com/DGS-ai-team/DAgents/actions/runs/39202528888) 与 [CodeQL](https://github.com/DGS-ai-team/DAgents/actions/runs/39202528519) 均通过。安装验收工具和记录已通过 PR #349 合入 `dev`。本轮另有启动/输出崩溃修复 `09c6fb1c`，还需通过开发 PR 检查并集成，再冻结新的最终候选；原候选 CI 不代替新提交的 CI，也不代替发行包安装验收。
- [x] **已复现的 Desktop 首次启动崩溃。** 同一发行候选在 stderr 正常时存活，关闭读端后复现 `0xc0000409`；诊断版捕获 `failed printing to stderr`、错误码 232 和 `events::sync_agents` 的可靠调用栈。修复统一处理进程输出 I/O 失败，并补齐同类 CLI stdout 问题；51 项 Rust 测试通过，正式模式构建通过闭管道进程与 CLI 输出验证。stderr 修复还完成 120 秒闭管道运行和原生内嵌 UI 交互。原始 dump 的完整业务栈未恢复；受控复现与原异常线索一致，但不把其他启动故障一并视为通过。各构建哈希及验收范围见[专项记录](design/desktop-product/audits/2026-10-19/desktop-startup-panic.md)。新代码的 PR 集成和最终包验收仍由相邻门槛约束。
- [ ] **最终包的标准用户初装、升级、卸载与恢复。** 在一次性 Windows 用户或 VM 中，用真实二进制跑初装→首配→使用→运行中升级→重开→卸载；覆盖中文/空格路径、快捷方式、可选自启的开/关状态及静默升级、非本安装 Node/另一安装不受影响。确认配置、LLM 密钥、Agent 历史/记忆、工作区、Desktop 偏好保留。补无 WebView2、不可写路径、文件占用失败和从备份恢复的演练。不得声称已有应用级事务回滚。操作见 [安装升级清单](../packaging/windows/install-upgrade-acceptance.md)。

  10 月 19 日已按用户授权在开发机完成当前 `95e3f98c` x64 QA 包初装、原生首配/创建 Agent/Mock 对话、同产品 payload 的 `0.11.0 → 0.11.0.1` 合成安装器升级，以及用户手动重启后的历史恢复、运行中卸载和最终校验。安装后文件哈希、配置/策略/Desktop 样本与 Node 设置逻辑摘要通过；另比对九个 fixture 数据库与历史文件，卸载前后逻辑/文件哈希一致，正式安装的受检设置未变。当前只是管理员开发机 QA 范围；正式快捷方式/自启/PATH、标准用户、已配置密钥、旧发行迁移及故障恢复仍未完成，见[候选记录](design/desktop-product/audits/2026-10-19/release-candidate.md)。
- [ ] **v0.11.0 用户迁移。** 脱敏旧数据打开、配对解密、Agent/Auto/workspace/session 与未决 Trigger 恢复已通过，备份后卸载重装流程已成文。v0.11.0 本身已使用 SQLite，不新增 fat YAML 导入兼容；旧空闲压缩值不继承为新上下文阈值。剩余为真实旧发行安装包备份→卸载→新装→恢复与失败恢复演练；无 marker 拒绝覆盖不能计作迁移通过。
- [ ] **LLM 与 MCP 配置往返。** 当前真实窗口已通过空 Key 探测/保存/重开探测与 MCP 工具开关/高级 JSON/凭据往返，失败草稿不覆盖已存配置。HTTP 对端为本地合成服务，仍需在最终安装候选与实际 Provider/外部 MCP 上做冒烟；清空/替换 Key、取消和探测失败仍以自动回归为主，未逐一原生复验。
- [x] **配置 ID 改名的 Agent 引用。** 活动 Agent 引用中的 ID 改名/删除返回 `409 llm_profile_in_use`，列出 Agent 并要求先修改绑定或删除 Agent；软归档 Agent 不阻止变更。拒绝发生于设置和密钥持久化前，创建/PATCH 还会拒绝不存在的目标 ID，详见 [API 契约](api/node-integration.md) 与配置验收记录。
- [ ] **最终候选的任务恢复与 Auto 耐久。** 做一轮不少于 24 小时的持续运行，覆盖唤醒、无事可做、用户插话、Todo 修改、待审批、拒绝、停止、Dreaming、经验注入和手册文件。分别在运行/待审批/空闲状态重启 Desktop 或 Node，核对队列、历史、工具副作用和调度，不重复执行未知副作用；工具无工具收尾、失败批次及空闲上下文边界一并做候选包冒烟。既有各专项证据保留，不要求重复全部测试矩阵。
- [ ] **实际 SSH 与发行服务。** Windows Desktop 经用户创建的 SSH loopback 隧道连接现代 Linux Node，验证掉线/重连、远端文件上传下载/冲突、终端恢复与明确结束，确认不会启动本机 Node。打包 Browser/Chromium 已重建与静态检查，但尚未运行页面与任务冒烟；Linux 包也需真实环境启动。Windows x86 完整包已按用户决策暂停，不再作为本版必测目标。
- [ ] **版本资料、依赖审计与发布产物。** `Unreleased`、README 原生截图、操作手册、路线图与支持范围已同步。剩余为冻结后用 `prepare_release.py` 统一正式版本，再运行 `validate_release.py`；当前校验仅证明 `0.11.0` 开发验收元数据一致。核对真实发布构建的文件版本、包内容、许可证、SHA256SUMS、release-manifest 和更新选包/下载后安装链路。

## 依赖告警的处置范围

2026-10-17 读取 GitHub Dependabot：开放 21 项，其中 20 项位于两个设计原型的 npm lockfile（14 high、6 medium），另 1 项为 Desktop Cargo.lock 的 `glib` medium 告警。它们不能直接等同于 Windows 发行包的 21 个运行时漏洞。

- [x] 本轮依赖审计汇总。生产前端三套 audit 为 0，Python 主依赖及 Browser 既有明确例外清单审计通过；Go 1.26.9 五模块扫描为 0 可达漏洞。默认 Go 1.26.5 曾检出标准库问题，禁止用于候选构建。移除未使用的 ZIP 库后，完整 RustSec 锁文件扫描为 0 漏洞、7 项警告；使用本轮已成功获取的数据库，后续刷新失败限制见[依赖记录](development/dependency-readiness-2026-10-17.md)。最终发布 CI 仍须重新联网审计，非发行 Linux 图和未维护依赖警告保留，不能声称所有依赖无告警。
- [x] 核实 `glib`：Windows 目标不包含该依赖；Linux GTK3 依赖族需协调上游升级，未做不兼容的 lockfile 强制替换，不新增忽略项。
- [x] 两个设计原型 Vite 及兼容传递依赖已更新，各自 npm audit 为 0，build 与 test:sites 通过。

10 月 19 日补充：生产前端三套 npm audit 为 0；Python 主锁无已知漏洞，Browser 锁沿用工作流现有明确忽略项；本地 RustSec 使用缓存数据库为 0 漏洞、7 警告。Go 1.26.9 五模块扫描无可达漏洞，Node 所需的 `golang.org/x/crypto` 模块包含 `GO-2026-5932`（未维护的 OpenPGP 包），当前代码没有导入该包或调用对应路径，不应写成“所有依赖无告警”。正式候选仍以冻结提交的联网 CI 审计为准。

## P1：体验收尾，可随候选验收并行

- [ ] 小窗口与 125%/150% DPI：设置边缘渐隐、长模型名称、长工具名称、工具卡、附件、审批卡和悬浮消息坞不截断或互相遮挡；键盘焦点、减少动画偏好可用。现有最大化/还原检查不能替代完整缩放矩阵。
- [ ] 真实外部 MCP、Manage 反馈提交→管理员处理→回复同步，以及工作组审批/取消/恢复做一次组合冒烟；发布说明仅承诺已验证范围。若发现阻断或数据问题，提升为 P0。
- [x] 统一当前文档入口。Desktop 资料包、早期 MVP 和设计入口已加当前状态指针，路线图移除过时的未落地 Goal/多用户主线；历史验收事实保留。

## 不作为本版前置条件

浏览器人工接管与可交互嵌入式页签、Desktop 自建 SSH 隧道、LAN/WAN 直连鉴权、多用户/租户隔离、macOS/Linux Desktop、新事件总线/工作流系统，以及再次全面重写 UI/runtime，均留到后续独立立项。当前个人 Node + 外部 SSH 转发的边界不变。

## 执行顺序

1. 本轮资源与展示开发 PR #348 已完成 CI 和 `dev` 集成，冻结新增功能及产品源码 `95e3f98c`。
2. 保留已完成的配置引用保护和迁移说明，以冻结提交重建带真实应用二进制的完整候选包；安装验收工具的修正另记来源。
3. 先完成标准用户初装/旧版迁移/升级恢复，再完成原生配置与资源冒烟；耐久测试可同时进行。
4. 关闭上述验收发现的问题，重建候选并只重测受影响链路；补齐版本资料与最终依赖审计。
5. 按 [发布流程](release-process.md) 建立 `dev → main` 发布 PR，通过检查和审查后，在 `main` 对应提交打标签；发布后校验实际资产、安装与升级，再同步 `main → dev`。

本轮只提交开发 PR 和评估清单，不创建发布标签，不把未验证项目标为完成。
