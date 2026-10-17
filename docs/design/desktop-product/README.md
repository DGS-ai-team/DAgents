# DAgents Desktop 产品设计资料包

> 当前入口（2026-10-17）：本目录下的图稿和原型保留设计来源，生产实现已进入独立 Windows Desktop 收束阶段。当前产品边界见[个人 Node 与 Desktop 职责](product-direction-simplification-2026-10-14.md)，API 见[Node 接入说明](../../api/node-integration.md)，已接入能力见[日常使用接入](daily-use-integration.md)，剩余发布条件见[发布收束清单](../../release-readiness.md)。下文 9 月设计快照中的“尚未接入”“正式契约未冻结”不作为当前实现状态。

更新：2026-09-24。这里集中保存 Desktop 设计已经产生的材料，供产品决策、视觉设计和下一轮开发共同使用。**这是设计资料快照，不代表这些功能已经在正式产品中实现。**现有文件仍保留在原位置，以免破坏项目中的引用；修改共享色彩或图标时，应先更新原始文件，再同步本资料包，避免产生两个事实来源。

每项材料的原始路径见 [来源清单](SOURCE-MANIFEST.md)。

2026-10-08 实装进度见 [Desktop MVP 收束与验收记录](mvp-closeout.md)。其中区分了已接入的 Windows 本机工作台、模拟原型和尚未完成的产品能力；不要将原型中的浏览器/文件交互视为已经实现。

## 从哪里开始

1. 看 [当前 Desktop 界面图](visuals/current-desktop-mockup.png)，再运行 [可操作原型](prototype/) 验证布局和主要点击路径。启动：在 `prototype/` 下执行 `npm install`、`npm run dev`。原型只使用模拟数据。
2. 看 [技术方案](architecture/desktop-node-separation-plan.md) 理解 Desktop / Node / Manage、原生 Shell、浏览器、文件和跨平台边界。
3. 看 [工作区细化方案 v3](workspace-layout-v3.md) 了解资源页快捷输入、终端页签、顶栏折叠和消息样式；[v2](workspace-layout-v2.md) 保留对话/资源分离的初始决策。
4. 看 [产品设计缺口与补齐顺序](product-design-gaps.md)。它区分已有决策、尚未设计的体验和开发前必须冻结的规则。

## 已归集材料

| 目录 | 内容 | 来源与效力 |
| --- | --- | --- |
| [`visuals/current-desktop-mockup.png`](visuals/current-desktop-mockup.png) | 最新一版 Desktop 界面图 | 本轮生成图稿；**当前布局参考**，尚未经过完整产品设计定稿 |
| [`visuals/exploration/`](visuals/exploration/) | 同轮 12 张探索及修改过程图 | 保留设计过程，不作为实施依据；部分是标注或过渡稿 |
| [`prototype/`](prototype/) | React/Vite 可操作原型、图标同步脚本、测试与 [v3 验收记录](prototype/design-qa-v3.md) | 从 `docs/design/desktop-interactive-prototype/` 起步，持续按工作区方案改造；这是后续 Desktop 原型的当前版本，不是生产 UI 或 Node API 实现 |
| [`icons/`](icons/) | Desktop 独立 SVG 源、来源清单及[图标画板](icons/gallery.html) | 基于已验收的 DAgents/Lucide 形态筛选并补绘 Desktop 操作图标；原型构建时同步，不修改 Node/Manage 共享资产 |
| [`architecture/`](architecture/) | Desktop / Node 分离方案和早期 SVG 布局图 | 复制自 `docs/architecture/`；**技术边界参考**，其中三栏资源区、Auto 总览和 Node 快切的旧 UI 描述已被最新原型覆盖 |
| [`visual-system/`](visual-system/) | 品牌图、配色、图标文件/画板、Node 与 Manage UI 审计 | 复制自 `shared/branding/` 与 `docs/design/`；属跨产品视觉材料，Desktop 适用性仍需单独定稿 |
| [`references/ui-product-grade-redesign.md`](references/ui-product-grade-redesign.md) | 现有 Node Web UI 的产品级改造背景 | 复制自 `docs/design/ui-product-grade-redesign.md`；帮助保留现有体验，不等于 Desktop 规格 |

## 当前明确的界面方向

- 顶部使用**单行菜单与窗口控制**，不叠加独立标题栏；不放显式搜索框，也不放右上角的 Node 快捷切换控件。Node 的连接和切换仍需要设计可发现的正式入口。
- 左侧按**智能体、工作组、自主智能体**分组；分组标题右侧不放下拉图标。侧栏不再放 Auto 总览按钮。
- 对话是独立工作状态；终端、浏览器和文件使用资源页签。资源打开时占据原消息内容区，右下角悬浮消息坞可快速发消息，并按需展开最近消息、工具与审批摘要。
- 聊天消息不增加助手头像。资源与审批的归属要明确对应当前 Node、智能体和会话。
- 当前原型只验证一个代表性工作区与几个模拟动作。Node 连接、真实浏览器、文件传输、终端和系统菜单尚未接入。

## 资料使用规则

**界面冲突时以最新图稿和可操作原型为准；技术边界以技术方案为准；正式开发契约尚未冻结。**例如技术方案第 3.5–3.7 节及后文验收条目中的常驻三栏资源面板、侧栏 Auto 总览和顶栏 Node 快切是早期方案，不能直接照搬。视觉系统中的 Node dark / Manage light token 也不自动成为 Desktop 的最终主题；当前原型使用浅色工作区，仍需主题规范。

本包中的共享视觉与技术文档是截至 2026-09-24 的资料快照，`prototype/` 则是当前 Desktop 原型，旧 `docs/design/desktop-interactive-prototype/` 不再继续迭代。生产实现和现行 API 仍以代码与当前架构文档为准；完成产品设计后应新增可执行的屏幕/状态/交互规格，并在技术方案中消除被替代的布局描述。
