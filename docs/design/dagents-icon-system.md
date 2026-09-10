# DAgents 专有图标系统

`dagents-icon-system.svg` 是 Node、Manage 共用的 SVG 设计画板。它只新增设计资产，不改变现有业务 UI。画板按产品结构分成五组，共 60 个可复用的语义 key；每个卡片都使用 24×24 逻辑网格，主线宽 1.6px，`round cap / join`，颜色通过 `currentColor` 继承。

## 盘点基线

本次盘点覆盖了 Node、Manage 和桌面端代码中当前可见的图标来源：

| 来源 | 统计 / 范围 | 处理方式 |
| --- | --- | --- |
| Node、Manage、desktop inline SVG | 82 个 `<svg>` 标签，分布在 31 个源码文件 | 映射到本画板的语义 key；后续按组件迁移 |
| Unicode / 字符图标与 CSS 伪元素 | 49 个包含候选字符的源码文件 | 将功能性字符替换为 SVG；纯文本符号、数学/日志内容继续保留 |
| 工具来源图标 | `ToolGroupIcon.vue` 的 bash、terminal、browser、child、computer、mcp、linux、fs、hitl、memory、skills、triggers、wecom、wrench | 通用工具使用本系统；企业微信等第三方品牌保留 |
| 品牌图片 | `@dagents-brand/brand-icon.png` 在 Node、Manage 的品牌、空状态、活动状态中复用 | 继续使用品牌资源；`brand-snowflake` 是未来 SVG 品牌替代稿 |

### 迁移边界

`brand-snowflake`、`agent`、`workgroup`、`auto`、工具和状态图标适合逐步替换现有 inline SVG。`create`、`search`、`more`、`close`、`expand-collapse`、`success` 等适合替换侧边栏、设置页、对话操作中的字符或 CSS 伪元素。第三方标识和数据来源标识不得用通用图标冒充：DeepSeek、OpenAI、Mimo、企业微信以及未来接入的品牌/数据源图标继续使用其品牌资源或专用徽标。

## 图标清单

“替换现有”描述的是迁移建议，不代表本提交已经改动业务组件。

### 01 品牌与导航

| key | 语义 | 使用位置 | 替换现有 |
| --- | --- | --- | --- |
| `brand-snowflake` | DAgents 品牌雪花与晶体中心 | Node / Manage 品牌、启动页、favicon | 设计稿；保留当前 PNG 直到品牌资源迁移 |
| `nav-agents` | 智能体分组 | Node `NavRail.vue` | 是，替换字符/通用轮廓 |
| `nav-workgroups` | 工作组分组 | Node `NavRail.vue`、Manage 工作组页 | 是 |
| `nav-auto` | 自主智能体分组 | Node `NavRail.vue`、Auto 入口 | 是，使用轨道/脉冲，不使用完整雪花 |
| `nav-node` | Node 节点与本地工作区 | Node 连接、节点切换 | 是 |
| `nav-manage` | Manage 管理控制台 | Manage 顶栏、管理员入口 | 是 |
| `nav-settings` | 设置与配置 | Node `SettingsLayout.vue`、设置侧栏 | 是 |
| `nav-feedback` | 帮助与反馈 | Node 帮助反馈入口 | 是 |
| `create` | 新建资源 | Node 智能体/工作组新建按钮 | 是 |
| `search` | 搜索 | Node/Manage 列表和设置筛选 | 是 |
| `menu` | 移动端菜单 | Node 移动侧栏、Manage 响应式顶栏 | 是 |
| `more` | 更多操作 | section action、行级操作菜单 | 是，替换 `⋯`、`⋮` |

### 02 智能体与 Auto

| key | 语义 | 使用位置 | 替换现有 |
| --- | --- | --- | --- |
| `agent` | 普通智能体 | Agent 列表、空状态、设置页 | 是 |
| `auto` | 自主智能体 | `AutoBadge.vue`、Agent 类型标识 | 是，替换 `✦` |
| `auto-overview` | Auto 总览 | Node Auto 总览页、侧栏 section action | 是 |
| `wake-trigger` | 自主激活频率与 Trigger | Agent autonomy 设置、Trigger 设置 | 是 |
| `dreaming` | 每日经验整理 | dreaming 设置、运行状态 | 是 |
| `experience-handbook` | 经验索引与手册目录 | Agent 手册面板、文件系统手册 | 是 |
| `todo-list` | Auto 默认读取和修改的任务清单 | Auto todo 面板 | 是 |
| `autonomy` | 自主能力、职责边界和单次轮次 | Agent autonomy 设置 | 是 |
| `model` | LLM 模型配置 | `LlmProfileModal.vue`、Manage LLM 配置 | 是；供应商文字/徽标保留 |
| `skills` | 可见技能 | Agent 能力设置、Skills 面板 | 是 |
| `tools` | 工具能力与审批策略 | Agent 能力设置、Policy 面板 | 是 |
| `memory` | 记忆与上下文整理 | memory 工具、经验状态 | 是 |

### 03 工作组与管理

| key | 语义 | 使用位置 | 替换现有 |
| --- | --- | --- | --- |
| `workgroup` | 工作组协作空间 | Node/Manage 工作组列表和详情 | 是 |
| `workgroup-members` | 工作组成员与 AgentRef | 成员弹窗、成员列表 | 是 |
| `manage-dashboard` | Manage 首页总览 | `HomeDashboard.vue` | 是 |
| `admin-access` | 管理员与访问控制 | 登录、管理员设置、权限面板 | 是 |
| `publish` | 发布、上架或推送 | Manage 发布、包上传、工作组发布 | 是 |
| `sync` | Node/Manage 同步和刷新 | 节点状态、同步操作 | 是 |
| `feedback-inbox` | 管理员反馈收件箱 | Manage 反馈列表和详情抽屉 | 是 |
| `release-update` | 版本与更新中心 | Manage 更新、桌面更新状态 | 是 |
| `agent-registry` | Agent 注册表、快照 | Manage Agent 管理 | 是 |
| `audit-log` | 运行记录与操作审计 | Manage 详情、Node 运行记录 | 是 |
| `invite` | 邀请成员、添加 Agent | 工作组成员管理 | 是 |
| `permissions` | 权限、能力授权和策略 | Agent/Workgroup 权限设置 | 是 |

### 04 工具与工作区

| key | 语义 | 使用位置 | 替换现有 |
| --- | --- | --- | --- |
| `filesystem` | 文件系统与手册根目录 | Agent 工作区、手册设置 | 是 |
| `folder-open` | 打开或选择工作目录 | `AgentWorkspacePicker.vue`、Node 原生目录选择器 | 是 |
| `file` | 文件、配置和文档 | 手册树、工具结果、设置 | 是 |
| `terminal` | 终端会话 | `TerminalWorkbench.vue`、终端工具 | 是，统一 `ToolGroupIcon.vue` 的 terminal |
| `browser` | 浏览器工具和页面 | 浏览器面板、工具组 | 是，通用浏览器轮廓；浏览器供应商标识保留 |
| `mcp` | MCP 服务和外部工具 | MCP 设置、工具来源 | 是 |
| `child-agent` | 子智能体和委派任务 | Child Agent 进度面板 | 是 |
| `shell` | Shell 命令和本地通道 | bash/shell 工具结果、审批面板 | 是 |
| `clipboard` | 剪贴板与复制内容 | Desktop Shell 能力、复制操作 | 是 |
| `upload` | 上传与导入资源 | 包上传、文件输入 | 是 |
| `tool-skills` | 工具组 / 可见技能卡片 | `ToolGroupIcon.vue`、Skills 面板 | 是 |
| `tool-memory` | memory 工具来源 | `ToolGroupIcon.vue`、工具结果 | 是 |

### 05 状态与操作

| key | 语义 | 使用位置 | 替换现有 |
| --- | --- | --- | --- |
| `success` | 成功、已完成 | 工具结果、提交反馈、状态标签 | 是，替换 `✓` |
| `warning` | 警告、需要关注 | 状态面板、配置校验 | 是 |
| `error` | 失败、错误 | 错误提示、失败工具结果 | 是，替换 `×` |
| `info` | 提示和帮助说明 | 设置说明、状态提示 | 是 |
| `pending` | 排队、连接中、处理中 | SSE、任务和更新状态 | 是 |
| `run` | 运行、激活、播放 | Auto 激活、工具重试 | 是 |
| `stop` | 停止、取消 | 流式响应、终端和 Auto 控制 | 是，替换 `−` 或文本按钮图标 |
| `refresh` | 刷新、重试、同步 | Node/Manage 列表与状态 | 是 |
| `save` | 保存配置 | Agent/Node/Manage 设置 | 是 |
| `edit` | 编辑、重命名 | `NavRail.vue`、设置表单 | 是，替换 `✎` |
| `delete` | 删除、移除 | Agent/工作组行操作 | 是，替换 `×` |
| `expand-collapse` | 展开收起 | Auto todo、Transfer status、CSS 伪元素 | 是，替换 `⌄`、`⌃` |

`check`、`close`、`back`、`forward`、`filter`、`copy` 等是画板 defs 中的基础控制图元，供 `success`、表单、抽屉和分页控件复用。它们不单独占一张卡片，以控制画板密度；对应的 Unicode `✓`、`×`、`←`、`→`、`›`、`‹`、CSS SVG arrow 和 `content: "⌄"` 均列入后续迁移范围。

## 保留例外

- `@dagents-brand/brand-icon.png`：当前 Node、Manage 品牌、空状态和活动指示器使用的现有品牌资源，待品牌资源统一后再切换 SVG。
- `DeepSeek`、`OpenAI`、`Mimo` 等 LLM provider 标识：这是供应商身份和数据来源，保留文字或供应商专用徽标，不用 `model` 图标代替。
- 企业微信 `wecom`、未来的 GitHub/GitLab 或其他外部连接器徽标：保留第三方品牌规范；系统图标只表示“连接器/工具”这一层语义。
- 工具结果中的 `×`、`·`、乘法/尺寸符号和日志正文中的普通字符：如果它们是数据内容而不是操作控件，不进行图标替换。

## 源码盘点索引

以下是本次 `82` 个 inline SVG 标签所在的 `31` 个源码文件。文件名是迁移时的定位索引，具体控件按上面的语义 key 复用，不为每个组件再建立一套视觉图标。

| 产品面 | 文件 |
| --- | --- |
| Node 对话与工作台 | `node/webui/frontend/src/components/ChatComposer.vue`、`ComposerToolbar.vue`、`ContextMeter.vue`、`MessageBubble.vue`、`ScrollToTailButton.vue`、`StatusPanel.vue`、`TerminalActionMenu.vue`、`TerminalPanel.vue`、`TerminalSessionIndicator.vue`、`TerminalTargetMenu.vue`、`TerminalWorkbench.vue`、`TerminalWorkbenchComposer.vue`、`ToolGroupIcon.vue`、`UiSelect.vue`、`WorkgroupComposer.vue`、`WorkgroupMemberModal.vue`、`WorkspaceSwitcher.vue` |
| Node 导航与设置 | `node/webui/frontend/src/components/NavRail.vue`、`node/webui/frontend/src/layouts/SettingsLayout.vue` |
| Node 状态与页面 | `node/webui/frontend/src/components/AgentEmptyState.vue`、`node/webui/frontend/src/components/McpStatusIndicator.vue`、`node/webui/frontend/src/components/SkillsPanel.vue`、`node/webui/frontend/src/components/SkillsStatusIndicator.vue`、`node/webui/frontend/src/views/WorkgroupView.vue` |
| Manage 控制台 | `manage/console/frontend/src/components/AppTopNav.vue`、`AskAiButton.vue`、`DetailDrawer.vue`、`HomeDashboard.vue`、`LoginView.vue`、`WorkgroupChatView.vue`、`WorkgroupView.vue` |

字符候选主要位于 `NavRail.vue`（`✦`、`✎`、`×`）、`AutoBadge.vue`（`✦`）、`AutoTodoPanel.vue` 与 `workbench.css`（`⌃`、`⌄`）、`AgentDetailSettings.vue`（`←`）、`McpSettings.vue`（`⌘`、`›`）、`UiSelect.vue` 与工具结果行（`✓`）、`ImageLightbox.vue`（`‹`、`›`）、`TransferStatusBar.vue`（`⌃`），以及 Manage `PageHeader.vue`（`›`）。其余命中的字符属于测试数据、日志文本、尺寸符号、路径分隔符或普通文案，保留原文。

## 视觉规则

- 结构使用六向、60° 的雪花几何与切角晶体节点；完整雪花只用于品牌锚点和少数成功/完成语义。
- Auto 使用轨道、脉冲和时钟表达“持续检查—唤醒—执行”；Dreaming 使用月牙，工作组使用三节点拓扑。
- 图标只描述一个动作或对象，冷蓝用于导航/工具，紫色用于 Auto/管理，绿色用于可执行/成功，暖色用于注意/Dreaming，红色用于错误/破坏性动作。
- 组件通过 `currentColor` 控制状态和主题色；不要在图标内部写死产品主题色。点击目标由外层按钮提供，SVG 保持 `aria-hidden="true"`，可访问名称由按钮的 `aria-label` 提供。
