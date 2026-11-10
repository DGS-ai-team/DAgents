# Desktop 控件样式规范

更新：2026-11-10。适用范围：`desktop/tray-tauri` 的生产界面。本文规定控件的样式归属和使用方式，历史原型不再作为控件尺寸与颜色的实现依据；Manage Console 不在此次迁移范围内。

## 设计原则

- 沿用浅色、低对比灰白界面；普通操作使用中性色，成功、警告和错误才使用对应状态色。
- 同类控件共用同一份外观定义。页面决定排列、宽度和业务内容，不重新定义边框、圆角、字体、背景、交互状态。
- 普通操作优先图标按钮，必须提供清晰的 `aria-label` 和悬浮说明；菜单选项、需要明确决策的保存、批准、取消等操作保留文字。
- 统一并不意味着所有可点击元素都变成按钮。资源列表行、窗口页签、代码编辑器、终端、消息输入区有独立语义，保留专用组件和布局。

## 样式归属

生产入口是 `desktop/tray-tauri/src/main.js`，只引入 `src/ui/styles/index.css`。该文件按固定顺序加载 tokens、layout、desktop-ui、workbench、overrides、controls，禁止在子页面重复导入这些全局样式。

| 层级 | 所在位置 | 负责内容 |
| --- | --- | --- |
| 设计变量 | `src/ui/styles/tokens.css` | 字体栈、字号、颜色、间距、圆角、尺寸、阴影及语义变量 |
| 通用控件外观 | `src/ui/styles/controls.css` | 按钮、字段、开关、多选框、选择器、菜单、弹窗及浮层表面 |
| 通用交互组件 | `src/ui/components/UiSelect.vue`、`UiContextMenu.vue`、`SettingsDisclosure.vue` | 键盘、焦点、打开关闭、定位、受控状态；折叠区样式由组件自身唯一持有 |
| 设置页面结构 | `SettingsPageHeader.vue`、`SettingsSectionHeader.vue`、`ConfigPanelShell.vue`、`SettingsSaveBar.vue` 等现有组件 | 标题、分区、只读与保存等共同结构 |
| 工作台与业务布局 | desktop-ui、workbench、layout、页面 scoped CSS | 区域大小、栅格、滚动、内容密度及专用业务组件 |

浅色主题颜色定义在根节点 `:root[data-theme="light"]`。不得在某个页面重新定义整套主题；否则挂载到 `body` 的菜单、选择器列表与页面会出现不同配色。语义变量引用的底层色值应在同一主题作用域确定，避免依赖子节点覆盖重新计算已继承的变量。

## 控件使用表

尺寸均为 CSS 像素。输入行和选择器正常高度为 36；内容过长可以换行的按钮使用最小高度，不能裁切文本。

| 控件 | 唯一入口 | 规格及使用边界 |
| --- | --- | --- |
| 普通按钮 | `.btn` | 默认 32px 最小高度；默认即次要操作，不需要另加 default 类 |
| 主操作 | `.btn.btn--primary` | 中性深灰填充；一组操作优先只有一个主操作 |
| 破坏性操作 | `.btn.btn--danger` | 删除、结束等不可忽略的破坏性行为；拒绝审批不等于危险操作 |
| 轻量操作 | `.btn.btn--ghost` | 透明底，统一悬浮和键盘焦点样式 |
| 紧凑按钮 | `.btn.btn--sm` | 28px 最小高度；用于工具条、紧凑卡片 |
| 图标按钮 | `.desktop-icon-button` | 36px 正方形；默认透明底，也可与 `.btn` 的语义变体组合 |
| 紧凑图标按钮 | `.desktop-icon-button--toolbar` | 与基础图标类组合，28px；不在页面写独立宽高覆盖 |
| 文本、数字、密码输入 | `.settings-field__input` | 正常高度 36px，统一字号、边框和焦点；宽度由表单布局决定 |
| 多行输入 | `textarea.settings-field__input` | 用 `rows` 表达所需内容行数；长文本可加 `--multiline` 变体，允许纵向调整 |
| 字段结构 | `.settings-field` / `__label` / `__hint` / `__error` | 标签、输入、提示、错误分工固定，错误使用 `aria-invalid`，不能只改颜色 |
| 选择器 | `UiSelect` | 正常 36px，`size="compact"` 32px；大量选项使用 `searchable`；图标位置、列表与键盘交互统一 |
| 开关 | `.settings-switch-input` 或 `.settings-toggle input[type="checkbox"]` | 38×22px；用于单项开启/关闭 |
| 多项勾选 | `.form-checkbox` | 16px；批量选择、技能白名单等保留勾选语义，不替换为开关 |
| 折叠分区 | `SettingsDisclosure` | 52px 最小标题行、右侧箭头；说明放展开内容顶部；`initiallyOpen` 或 `v-model:expanded`；内容保留挂载，不丢草稿 |
| 右键菜单 | `UiContextMenu` | 共用浮层颜色、阴影和选项状态；保留既有焦点、键盘及视口边界处理 |
| 弹窗 | `.ui-dialog` + `__header` / `__body` / `__actions` | 统一表面、圆角、阴影；容器负责宽度与滚动，调用组件继续负责焦点管理 |
| 弹窗遮罩 | `.ui-dialog-backdrop` | 统一中性半透明背景；不在页面另加模糊或更深的遮罩，定位与层级由弹窗决定 |
| 其他浮层 | `.ui-popover` | 统一表面，不替代选择器或菜单的键盘行为 |

## 允许与禁止的页面样式

允许页面声明 `display`、`grid-template-columns`、`gap`、`width`、`max-width`、`margin`、滚动及响应式布局。例如，Linux 凭据弹窗的设置项使用共享单行布局，资源工具条可使用紧凑图标按钮。

不允许在页面中写 `.some-page .btn { border-radius: ... }`、`:deep(.ui-select__trigger) { padding: ... }` 或重新定义普通输入框的背景、字号、边框、焦点。发现新的通用尺寸需求时，在控件层增加有明确用途、实际消费者的变体，禁止只为一个页面创建近似复制品。

业务状态可以专门表达：审批风险、运行中状态、文件选中态、代码语法色等。但不要借业务状态选择器改变普通按钮的尺寸与字体。无业务差异的裸按钮、输入和弹窗应接入现有入口，不复制 CSS。

## 设置页布局与信息层级

- 设置页按固定顺序组织：页面标题与简短说明、语义分组、各配置行、统一保存栏。页面标题说明页面整体用途；分组标题和说明只描述该组配置。
- 每项配置占一行。行左侧放名称及该项说明，右侧放输入控件；说明必须紧邻所属项，不得跨项汇总或拼接成摘要。
- 布尔项用开关表达当前状态。删除与开关重复的“已启用 / 已关闭”摘要；只读或环境变量托管状态作为该项内容中的说明呈现。
- 低频但相关的参数可以放在同一个 `SettingsDisclosure` 中。折叠标题使用简短的内容名，不使用“高级：”分类前缀、命令句或末尾冒号；技术格式名如 JSON 可以保留。标题右侧只显示箭头；用途说明和配置状态放在展开内容顶部或具体配置行内，不把长说明、状态放在标题右侧形成第二套信息布局。
- 设置分组、配置行、字段、标题和折叠区使用共享布局类与统一间距；组件负责语义结构，页面只负责分组顺序和业务内容。输入项在窄窗口可从右侧移到说明下方，开关仍与左侧说明保持同一行。
- 配置行使用 `.settings-row`；左侧 `.settings-row__copy` 中放 `.settings-row__title` 或 `.settings-field__label`，以及可选的 `.settings-row__description`；右侧直接放输入、开关或 `.settings-row__control` 控件容器。名称字体由 `controls.css` 统一定义，行排列由 `workbench.css` 负责。
- 当窗口宽度不超过 900px 时，标量输入改到名称和说明下方；布尔开关保留在同一行右侧。该断点为侧栏和页面留白预留空间。

```html
<label class="settings-field settings-row settings-row--field">
  <span class="settings-row__copy">
    <span class="settings-field__label">空闲时长（秒）</span>
    <span class="settings-row__description">设为 0 可关闭。</span>
  </span>
  <input class="settings-field__input" type="number" min="0">
</label>

<label class="settings-row">
  <span class="settings-row__copy">
    <span class="settings-row__title">当天日期提示</span>
    <span class="settings-row__description">将当前日期加入对话提示。</span>
  </span>
  <input class="settings-switch-input" type="checkbox" aria-label="当天日期提示">
</label>
```
- 校验错误显示在所属字段附近并使用 `aria-invalid`，页面级保存错误进入统一保存栏；禁用、只读及环境控制原因必须可见，不能只靠颜色或隐藏字段表达。
- 本规范适用于桌面端所有设置页，包括独立设置页、嵌入式配置面板、智能体详情设置和首次配置设置。迁移时保留列表、编辑器、终端等确有独立语义的结构，不扩展到聊天与资源浏览控件。

## 本轮迁移范围

- 消除 main、DesktopApp 与 workbench 的重复样式导入，以及公共控件在 workbench、desktop-ui、UiSelect、UiContextMenu 中的多处外观定义。
- 设置表单覆盖智能体、上下文、安全防护、共享服务、MCP、Linux 通道、模型配置和反馈等高频页面；设置类折叠区使用 `SettingsDisclosure`。
- 文件资源、浏览器资源、自主总览、工作组、审批和终端对话框中的普通操作控件接入公共样式。
- 保留终端、编辑器、文件列表行、消息卡片和窗口页签等专用结构；它们不属于普通表单控件，不应机械替换为 `.btn`。

## 验证与维护

新增/修改控件至少检查：正常、悬浮、键盘焦点、禁用、报错，以及长文本和最小支持窗口宽度。开关与多选框要保持语义区别；折叠区关闭时不可聚焦到隐藏内容，只读设置仍允许展开查看。

`src/ui/styles/controls.test.js` 检查全局入口与控件样式归属；交互组件的测试覆盖选择器、菜单和折叠区行为。前端验收命令：

```powershell
npm run test:frontend --prefix desktop/tray-tauri
npm run test:ui --prefix desktop/tray-tauri
npm run lint --prefix desktop/tray-tauri
npm run build --prefix desktop/tray-tauri
git diff --check
```

样式或结构改动须使用生产组件和同一 CSS 入口进行浏览器视觉验证。浏览器预览只验证前端呈现与交互，不替代 Windows 原生窗口、托盘或真实服务工作流的验收。

本轮浏览器验收使用生产组件和合成数据，覆盖公共控件图库、共享服务设置、智能体表单、终端选择弹窗。已检查输入框与选择器等高、按钮普通/紧凑尺寸、禁用与错误状态、搜索与长选项、菜单键盘导航、折叠内容的草稿保留，以及 800px 窗口边界。没有连接真实终端或修改生产配置。
