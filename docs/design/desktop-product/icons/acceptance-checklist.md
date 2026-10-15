# Desktop 图标逐项验收清单

范围：`shared/icons/desktop/manifest.json` 中的 **33 个 SVG**，其中 **27 个已用于可操作界面、6 个暂未使用**。每项链接直达 canonical 源 SVG；[图标总览](./gallery.html) 展示 16/18/20px，以及默认、悬浮、选中和禁用状态。大多数图标以 24×24 viewBox、约 1.8–2px 圆角描边、`currentColor` 为基准；填充图标按来源保留 viewBox。验收时比较同组图标的可见占位、线宽、留白及语义。

## 对象与侧栏（6）

- [ ] **01 [nav-agents](../../../../shared/icons/desktop/agent.svg)** — “智能体”分组标题、侧栏收起后的分组入口。核对与用户提供的 `user.svg` 一致；它与条目图标共用图形，语义由导航位置表达。
- [ ] **02 [agent](../../../../shared/icons/desktop/agent.svg)** — 普通智能体条目、固定“对话”页签。核对与用户提供的 `user.svg` 一致，并确认 16px、24px 下 CSS mask 显示清楚；语义由条目位置表达。
- [ ] **03 [nav-workgroups](../../../../shared/icons/desktop/nav-workgroups.svg)** — “工作组”分组标题、收起后的入口。与 `workgroup` 共用用户提供的 `team.svg` 双人图形，语义由导航位置表达。
- [ ] **04 [workgroup](../../../../shared/icons/desktop/nav-workgroups.svg)** — 前端团队、后端团队条目。与 `nav-workgroups` 共用用户提供的 `team.svg` 双人图形，语义由团队条目位置表达；确认 16px、24px 下 CSS mask 显示正常。
- [ ] **05 [nav-auto](../../../../shared/icons/desktop/auto.svg)** — “自主智能体”分组标题、收起后的入口。与条目图标 `auto` 使用相同的 Web UI 四角星符号，语义由所在位置表达。
- [ ] **06 [auto](../../../../shared/icons/desktop/auto.svg)** — 自主智能体条目。与分组图标 `nav-auto` 使用相同的四角星符号，核对主星与小星在 16px、24px 下居中并可通过 CSS mask 清楚显示。

## 资源、文件与返回对话（6）

- [ ] **07 [terminal](../../../../shared/icons/desktop/terminal.svg)** — 终端页签、资源切换及新增资源菜单。
- [ ] **08 [browser](../../../../shared/icons/desktop/browser.svg)** — 浏览器页签、地址栏及资源菜单；使用简洁窗口轮廓。
- [ ] **09 [file](../../../../shared/icons/desktop/file.svg)** — 文件页签、文件资源页及资源菜单。
- [ ] **10 [folder-open](../../../../shared/icons/desktop/folder-open.svg)** — “资源 · N”切换入口。检查与单个 `file` 的层级区别。
- [ ] **11 [attachment](../../../../shared/icons/desktop/attachment.svg)** — 对话输入栏“添加附件”按钮；勿与上传语义 `upload` 混用。
- [ ] **12 [skills](../../../../shared/icons/lucide/puzzle.svg)** — Node 设置导航和技能状态；尺寸按紧凑图形光学缩至 14px，并与 Auto 四角星区分。
- [ ] **13 [message](../../../../shared/icons/desktop/message.svg)** — 资源页状态坞“返回完整对话”按钮。

## 布局与导航（6）

- [ ] **14 [panel-left-close](../../../../shared/icons/desktop/panel-left-close.svg)** — 顶栏“收起侧栏”按钮。
- [ ] **15 [panel-left-open](../../../../shared/icons/desktop/panel-left-open.svg)** — 顶栏“展开侧栏”按钮。与上一项成对检查方向感。
- [ ] **16 [chevron-down](../../../../shared/icons/desktop/chevron-down.svg)** — 资源切换、模型选择、状态坞展开态。
- [ ] **17 [chevron-up](../../../../shared/icons/desktop/chevron-up.svg)** — 状态坞收起态。与 `chevron-down` 成对检查。
- [ ] **18 [circle-x](../../../../shared/icons/desktop/circle-x.svg)** — 关闭资源页签。检查小尺寸命中目标及误触风险。
- [ ] **19 [more](../../../../shared/icons/desktop/more.svg)** — 终端/资源的更多操作菜单。

## 创建、输入与执行（3）

- [ ] **20 [create](../../../../shared/icons/desktop/create.svg)** — 页签行“新增资源”。检查它与已移除的“新建对话”加号不会再混淆。
- [ ] **21 [play](../../../../shared/icons/desktop/play.svg)** — 仅用于终端命令执行；消息输入不使用播放图标。
- [ ] **22 [send](../../../../shared/icons/desktop/send.svg)** — 主对话和资源页悬浮输入栏共用向上箭头，与 `play` 语义区分。

## 状态与反馈（4）

- [ ] **23 [pending](../../../../shared/icons/desktop/pending.svg)** — 待审批卡标题。
- [ ] **24 [success](../../../../shared/icons/desktop/success.svg)** — 审批通过状态。
- [ ] **25 [error](../../../../shared/icons/desktop/error.svg)** — 审批拒绝状态。
- [ ] **26 [circle-check](../../../../shared/icons/desktop/circle-check.svg)** — 模型选择菜单中的当前项。检查它和 `success` 的用途是否易区分。

## 跨端语义复用（1）

- [ ] **27 [tools](../../../../shared/icons/desktop/tools.svg)** — Node 与 Manage 的 `wrench` / `tool` 语义共用用户提供的 wrench 轮廓；Desktop 顶栏“工具”仍为文字菜单。

## 已入库但当前未使用（6）

- [ ] **28 [arrow-left-right](../../../../shared/icons/desktop/arrow-left-right.svg)** — 原状态坞“返回完整对话”图标，现由语义更清楚的 `message` 承担。
- [ ] **29 [upload](../../../../shared/icons/desktop/upload.svg)** — 保留用于真实上传语义；当前“添加附件”按钮改用 `attachment`。
- [ ] **30 [collapse](../../../../shared/icons/desktop/collapse.svg)** — 旧折叠符号；原型已由 `panel-left-close` 和 `chevron-*` 承担。决定保留为后续控件图标，或从 Desktop 图标库移除。
- [ ] **31 [expand](../../../../shared/icons/desktop/expand.svg)** — 旧展开符号；同上。与 `collapse` 成对决策。
- [ ] **32 [image](../../../../shared/icons/desktop/image.svg)** — 预留素材图标；仅在未来出现图片资源控件时使用。
- [ ] **33 [edit](../../../../shared/icons/desktop/edit.svg)** — 预留编辑图标；不为它凭空增加界面控件。

## 不属于上述 SVG 清单的可见符号

- [ ] 顶栏窗口控制当前是文本符号 **`−` / `□` / `×`**，不是 SVG。验收是否与整套线性图标协调；它们在浏览器原型中不执行真实窗口操作。
- [ ] 资源状态坞的状态圆点由 CSS 绘制，需检查空闲、生成中、待审批的颜色与文字是否一起可辨识。
- [ ] 品牌雪花沿用 [PNG 图片](../visual-system/brand-icon.png)，用于程序/托盘品牌场景；按既定方向不转成按钮 SVG。

顶栏 **DAgents / 文件 / 编辑 / 视图 / 运行 / 工具 / 帮助** 当前采用文字菜单，不存在漏列的菜单 SVG。原型实现见 [Prototype.tsx](../prototype/src/Prototype.tsx)；[shared Desktop manifest](../../../../shared/icons/desktop/manifest.json) 记录逐项来源与许可。

## Web UI 图标复用记录

Node 与 Manage 的生产 SVG 由 `shared/icons/ui/*-registry.js` 静态 raw registry 管理，薄 `UiIcon.vue` wrapper 负责尺寸和 SVG 根属性。旧版 60 个本地副本已于 2026-10-09 归集到 `shared/icons/` 并移除。逐项复用与 2026-09-29 下载候选取舍见 [reuse audit](./reuse-audit.md)。
