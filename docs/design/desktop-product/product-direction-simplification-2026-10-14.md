# 产品路线收束：个人 Node、远程执行与 Runtime 边界

日期：2026-10-14。状态：用户已确认实施；本轮完成产品主线收束、退役代码清理、Runtime 构造统一与 Desktop/Node 配置分离。验收边界见 [本轮记录](audits/2026-10-14/product-closeout/README.md)。

> 2026-10-20 决策更新：本文中“Desktop 可连接远程 Node/SSH tunnel”的路线已被后续用户决定替代。当前 Desktop 只连接本机 Node；Linux 通过 Node 的 SSH/终端能力操作，不连接远程 Node。实现与当前限制见 [Desktop 接入范围与 Node 交互收束](desktop-node-interaction-review-2026-10-20.md)。

## 推荐定位

DAgents 是面向个人的 Agent 工作台与持续执行服务。Desktop 负责交互，Node 持有 Agent 状态并执行任务；同一个使用者可以从多台 Desktop 访问自己的 Node。Auto 是持续工作的数字员工，普通 Agent 是按需交互的助手。Manage 是可选的多节点、Agent 工作组协作控制面。

当前主线不做多人共用一个 Node 的账号、角色、私人记忆与租户隔离体系。Oct 13 用户身份方案作为后续多人部署的候选扩展保留，不作为首配或远程连接的开发前置条件。

## 真实使用场景

| 场景 | 形态与价值 | 建议 |
| --- | --- | --- |
| 个人本机使用 | Desktop + Node 同机，文件、Shell、浏览器与桌面能力按需启用 | 产品主线，安装与首配最简。 |
| 自己的远程 Node | Desktop 在工作电脑，Node 在无桌面 Linux、服务器或常开机器 | 保留；价值是执行环境分离、持续运行和数据位置，而非专门兼容旧 Windows。 |
| 操作旧 Linux 主机 | Node 在受支持机器，通过既有 SSH/SFTP 工具执行命令、操作文件 | 优先于在旧主机部署全套 Node；任务状态和 Auto 调度保留在 Node。 |
| 多台 Desktop 访问同一 Node | 同一个使用者从不同位置访问同一 Agent | 不增加用户或 Desktop 维度的 runtime，设备共享业务事实。 |
| 多机器/团队任务协作 | Manage 编排各自 Node 上的 Agent | 当前可选协作路线；Manage 本身不自动提供用户隔离或操作系统沙箱。 |
| 不同人共用 Node 且要求隐私隔离 | 私人上下文、文件、凭据与权限隔离 | 暂不纳入主线。必须出现实际需求后，另行设计与验收。 |

SSH 工具与远程 Node 有不同能力：前者主要为远端命令和文件操作，后者在目标机器运行完整 Agent、调度器和可用本机工具。SSH 不能替代远端 Computer Use、独立 Agent 运行或断网后的持续自主执行。

## Windows 与依赖路线

当前代码事实：

- `node/go.mod` 使用 Go 1.25.0，CI Go 工具链为 1.26.8。
- Node 静态构建脚本使用 `CGO_ENABLED=0`；核心 API/Agent 不要求用户安装 Desktop、Node.js、Python 或 WebView2。具体外部工具及可选服务另有依赖。
- Desktop 是 Tauri 2 / WebView2；浏览器侧车锁定 Playwright 1.63.0。
- 仓库 `browser-service/README.md` 与 `shared/config/browser_platform_windows.go` 已要求 Windows 11 build 22000+ 或 Server 2019+；Windows 10 不开放该浏览器能力。
- Windows 安装器当前仍包含 x64/x86 发布矩阵，但编译成功与目标平台全能力可用是不同结论。

官方基线：Go 从 1.21 起要求 Windows 10 / Server 2016 及以上；当前 Playwright 支持 Windows 11 / Server 2019 及以上；WebView2 已结束 Win7/8/8.1 的现代版本支持。

参考：[Go Windows 支持表](https://go.dev/wiki/Windows)、[Playwright Python 系统要求](https://playwright.dev/python/docs/intro#system-requirements)、[Microsoft WebView2 旧系统支持说明](https://blogs.windows.com/msedgedev/2022/12/09/microsoft-edge-and-webview2-ending-support-for-windows-7-and-windows-8-8-1/)。

建议：

1. 停止将 Windows 7/8/8.1、Server 2008/2012 作为主线目标；不另开固定旧 Go、浏览器或 Desktop 的兼容分支。
2. Windows 完整产品以 Windows 11 x64 为主验收基线，服务端优先 Server 2019+。这是建议目标，不是未经测试的支持承诺。
3. Windows 10 的现有基础能力可保留，但明确功能降级；不宣称全能力支持，不以冻结旧浏览器补齐。是否持续发布兼容包依据真实需求另定。
4. Linux Node 主线选择有明确验收的现代发行版；旧 Linux 优先经 SSH 管理，不沿用归档文档中对 RHEL6/7 的旧承诺。
5. x86 发布另做用户需求评估；架构为 32 位不等于系统版本旧，不能仅凭停 Win7 就自动删除全部 x86 产物。
6. 低配置设备仍可通过轻量 Node、按需加载浏览器和资源、空闲卸载 runtime 使用产品。高层可选依赖不提升核心 Node 的安装要求。

内置浏览器的 UI 展示与浏览器执行所在机器应有明确边界。当前已实现 Node 侧浏览器会话的 Desktop 查看链路；不要为了控制旧主机，顺带新增一套跨机器 Desktop 浏览器工具路由。此项如有真实需求需独立设计。

## Runtime 简化的边界

代码显示，runtime 膨胀不只来自会话数量，还包括审批等待、异步工具回灌、取消栅栏、快照与生命周期持久化、崩溃恢复、配置/技能快照、压缩、记忆、Trigger 和 Dreaming。

保留的产品需求：

- 不同 Agent 的模型、工作目录、权限、消息与队列隔离。
- 用户聊天和 Auto 激活进入同一个 Agent 主上下文。
- 同一个 Agent 只能按现有执行约束推进活动回合；多个 Desktop 的输入排队，审批和取消作用于同一 Node 权威状态。
- Workgroup 的执行历史与个人/Auto 主会话隔离。当前 `workgroup_agent_bridge.go` 已建立独立 durable session 并禁用个人记忆，不能简单合并后污染主上下文。
- 临时子 Agent 有独立任务上下文，完成后回收，复用执行引擎。
- Desktop 关闭不结束 Node 任务；Node 重启能恢复已有任务状态。

推荐概念：

| 对象 | 职责 |
| --- | --- |
| Agent | 长期配置、职责、工具、工作区、知识与 Auto 设置。 |
| 执行上下文（现有 Session 语义） | 一份历史与输入队列；通常为主上下文，必要时有 Workgroup 或临时子任务上下文。 |
| Runtime | 上述上下文当前加载的执行器，可空闲卸载；不按 Desktop 连接创建。 |
| Turn / Step | 一次任务回合及其模型/工具步骤；不承担用户或客户端身份。 |
| Desktop 连接 | 订阅、焦点、草稿与资源页；不持有另一份任务事实。 |

这是概念收束，未要求立即全仓改名或新增一套类。取消用户维度扩展，也不意味着可以退回整个 Node 一个全局 runtime；现有多 Agent 和工作组隔离仍有实际需求。

## 该审计和清理的内容

1. 统一主 Agent 与 Workgroup 的构造入口，抽取共同 build/config/policy 部分；保留不同上下文、工具授权与记忆范围。
2. 明确身份：agent_id 标识实例，session_id 标识执行上下文，turn/step 标识执行过程；不同时增加 user_id、desktop_id 作为执行分区键。
3. 逐项核对状态事实源。生命周期由 Coordinator 权威维护，界面与持久化投影消费它；可派生的 busy/pending 状态不再单独存一份。
4. 列出 epoch、generation、revision、digest 各自解决的竞态。先以取消、清空上下文、配置刷新、重启恢复等回归证明必要性，再合并冗余 fence；不凭变量多就删掉安全保证。
5. 输入 FIFO 和审批/取消/恢复控制事件分别描述；后者不能被待处理用户消息阻塞。无需以代码行数为理由强行合成一个队列。
6. Auto 仅追加可信激活输入和配置能力；Dreaming 复用主执行上下文的维护流程，不新增另一套永久运行引擎。
7. 临时任务复用同一执行引擎、明确结束/回收，不无限累积 session 和消费者。
8. 审计已弃用的兼容工厂、恢复补丁与旧 API 分支；每批清理保留现有真实场景验证。

上述清单是持续审计方向，不代表每项已经完成。本轮统一主 Agent / Workgroup 构造入口并处理重复 Open 与空闲释放后的恢复；删除退役 Go tray、对应构建与测试入口、未调用的 SysV 安装脚本。取消栅栏、恢复投影和真实数据迁移继续保留，不能将其统称为死代码。

## 身份与首配收束

一个 Node 一个使用者档案，允许多个授权 Desktop。称呼是该 Node 所属使用者的资料；不增加用户列表、用户级私人记忆、membership 或 Agent 人类 ACL。若将来支持多人，此限制要明确升级，不能把多个设备等同于多个已隔离用户。

远程连接采用用户预先建立的 SSH 本地转发：Desktop 的 `tunnel` 模式只连接 loopback HTTP endpoint，不启动或接管本机 Node。隧道身份验证与加密由外部 SSH 负责；Node HTTP Handler 没有设备鉴权，不能宣称支持受保护的 LAN/WAN 直连。Desktop 尚未实现 SSH 隧道创建和管理，也没有新增设备凭证或用户体系。

首配按实际部署分开：

- 本机：初始化使用者资料与 Node 名称、模型；Desktop 外观/通知单独保存，默认值即可使用。
- 连接已配置的远程 Node：确认连接与凭证后直接进入，不重复初始化、不修改已有称呼或模型。
- 连接未配置远程 Node：授权后引导配置该 Node，明确配置作用的主机与路径。

当前首配仅两步：可选使用者称呼与必填 Node 名称、模型配置。外观步骤已移除，通知与连接方式在本机 Desktop 设置中保存。首配完成只代表 Node 档案已初始化，不代表模型连通；提交后重读 Node 状态并保留其他既有模型 profile。已配置 Node 直接进入共享工作台，不创建 Desktop 专属 runtime。

## 优先级

1. 确认上述场景和平台范围，修正文档中过时承诺；当前不实施多用户系统。
2. 完成独立 Desktop/Node 的安装和升级闭环，简化本机首配，完善能力降级。Oct 14 已完成隔离安装生命周期实测并修复打包问题；最终候选复测、故障回滚与平台矩阵仍待验收，详见 [安装升级记录](audits/2026-10-14/install-upgrade/README.md)。
3. 为同一使用者的远程 Node 完成连接、鉴权、重连、能力协商及本地/远端资源边界。
4. 在既有场景回归保护下，整理 runtime 的构造、状态和恢复入口；不做一次性整套重写。
5. 优化 Auto 日常可靠性：持续执行、待审批可接管、Dreaming、断线与重启恢复、资源占用。
6. 多用户与不受信任租户隔离保持候选路线，有明确部署需求后另行启动。
