# Desktop 图标逐项验收清单

范围：`icons/manifest.json` 中的 **32 个 SVG**，其中 **25 个已用于可操作原型、7 个暂未使用**。每项链接直达源 SVG；[图标总览](./gallery.html) 同时展示 24px、16px 及不同底色。大多数图标以 24×24 viewBox、2px 圆角描边、`currentColor` 为基准；填充图标按来源保留 viewBox。验收时建议同时看 24px、16px、浅色侧栏和深色背景，并比较同组图标的线宽、留白及语义。

## 对象与侧栏（6）

- [ ] **01 [nav-agents](./nav-agents.svg)** — “智能体”分组标题、侧栏收起后的分组入口。核对与用户提供的 `user.svg` 一致；它与条目图标共用图形，语义由导航位置表达。
- [ ] **02 [agent](./agent.svg)** — 普通智能体条目、固定“对话”页签。核对与用户提供的 `user.svg` 一致，并确认 16px、24px 下 CSS mask 显示清楚；语义由条目位置表达。
- [ ] **03 [nav-workgroups](./nav-workgroups.svg)** — “工作组”分组标题、收起后的入口。与 `workgroup` 共用用户提供的 `team.svg` 双人图形，语义由导航位置表达。
- [ ] **04 [workgroup](./workgroup.svg)** — 前端团队、后端团队条目。与 `nav-workgroups` 共用用户提供的 `team.svg` 双人图形，语义由团队条目位置表达；确认 16px、24px 下 CSS mask 显示正常。
- [ ] **05 [nav-auto](./nav-auto.svg)** — “自主智能体”分组标题、收起后的入口。必须与条目图标 `auto` 使用完全一致的机器人图形，语义由所在位置表达。
- [ ] **06 [auto](./auto.svg)** — Auto Dev、Auto Ops 条目。与分组图标 `nav-auto` 使用相同图形；确认保留机器人头框、双眼与嘴部，不显示底座或星芒，并核对 16px、24px 下居中和 CSS mask 显示正常。

## 资源、文件与返回对话（6）

- [ ] **07 [terminal](./terminal.svg)** — 终端页签、资源切换及新增资源菜单。
- [ ] **08 [browser](./browser.svg)** — 浏览器页签、地址栏及资源菜单。
- [ ] **09 [file](./file.svg)** — 文件页签、文件资源页及资源菜单。
- [ ] **10 [folder-open](./folder-open.svg)** — “资源 · N”切换入口。检查与单个 `file` 的层级区别。
- [ ] **11 [attachment](./attachment.svg)** — 对话输入栏“添加附件”按钮；勿与上传语义 `upload` 混用。
- [ ] **12 [message](./message.svg)** — 资源页状态坞“返回完整对话”按钮。

## 布局与导航（6）

- [ ] **13 [panel-left-close](./panel-left-close.svg)** — 顶栏“收起侧栏”按钮。
- [ ] **14 [panel-left-open](./panel-left-open.svg)** — 顶栏“展开侧栏”按钮。与上一项成对检查方向感。
- [ ] **15 [chevron-down](./chevron-down.svg)** — 资源切换、模型选择、状态坞展开态。
- [ ] **16 [chevron-up](./chevron-up.svg)** — 状态坞收起态。与 `chevron-down` 成对检查。
- [ ] **17 [circle-x](./circle-x.svg)** — 关闭资源页签。检查小尺寸命中目标及误触风险。
- [ ] **18 [more](./more.svg)** — 终端/资源的更多操作菜单。

## 创建、输入与执行（3）

- [ ] **19 [create](./create.svg)** — 页签行“新增资源”。检查它与已移除的“新建对话”加号不会再混淆。
- [ ] **20 [play](./play.svg)** — 仅用于终端命令执行；消息输入不使用播放图标。
- [ ] **21 [send](./send.svg)** — 主对话和资源页悬浮输入栏共用。几何提取自 Node Web UI `UiIcon.vue` 的 `send`，与 `play` 语义区分。

## 状态与反馈（4）

- [ ] **22 [pending](./pending.svg)** — 待审批卡标题。
- [ ] **23 [success](./success.svg)** — 审批通过状态。
- [ ] **24 [error](./error.svg)** — 审批拒绝状态。
- [ ] **25 [circle-check](./circle-check.svg)** — 模型选择菜单中的当前项。检查它和 `success` 的用途是否易区分。

## 已入库但当前未使用（7）

- [ ] **26 [tools](./tools.svg)** — 用户提供的 wrench 图标资产；当前没有可见工具图标控件，顶栏“工具”保持文字菜单。只在未来出现具体工具动作入口时使用。
- [ ] **27 [arrow-left-right](./arrow-left-right.svg)** — 原状态坞“返回完整对话”图标，现由语义更清楚的 `message` 承担。
- [ ] **28 [upload](./upload.svg)** — 保留用于真实上传语义；当前“添加附件”按钮改用 `attachment`。
- [ ] **29 [collapse](./collapse.svg)** — 旧折叠符号；原型已由 `panel-left-close` 和 `chevron-*` 承担。决定保留为后续控件图标，或从 Desktop 图标库移除。
- [ ] **30 [expand](./expand.svg)** — 旧展开符号；同上。与 `collapse` 成对决策。
- [ ] **31 [image](./image.svg)** — 预留素材图标；仅在未来出现图片资源控件时使用。
- [ ] **32 [edit](./edit.svg)** — 预留编辑图标；不为它凭空增加界面控件。

## 不属于上述 SVG 清单的可见符号

- [ ] 顶栏窗口控制当前是文本符号 **`−` / `□` / `×`**，不是 SVG。验收是否与整套线性图标协调；它们在浏览器原型中不执行真实窗口操作。
- [ ] 资源状态坞的状态圆点由 CSS 绘制，需检查空闲、生成中、待审批的颜色与文字是否一起可辨识。
- [ ] 品牌雪花沿用 [PNG 图片](../visual-system/brand-icon.png)，用于程序/托盘品牌场景；按既定方向不转成按钮 SVG。

顶栏 **DAgents / 文件 / 编辑 / 视图 / 运行 / 工具 / 帮助** 当前采用文字菜单，不存在漏列的菜单 SVG。原型实现见 [Prototype.tsx](../prototype/src/Prototype.tsx)；[manifest.json](./manifest.json) 记录逐项来源与许可。

## Web UI 图标复用记录

当前 `node/webui/frontend` 没有独立 SVG 图标目录；生产界面图形内联在 [UiIcon.vue](../../../../node/webui/frontend/src/components/UiIcon.vue)。可单独复用的 DAgents SVG 来自 [docs/design/dagents-icons](../../dagents-icons/README.md)（与 `visual-system/dagents-icons` 的 60 枚 SVG 文件哈希一致）。逐项复用与 2026-09-29 下载候选取舍见 [reuse audit](./reuse-audit.md)。
