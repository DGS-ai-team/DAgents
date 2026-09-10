# DAgents 色彩系统

`dagents-color-system.svg` 是 Node 与 Manage 共用的色彩 token 设计样稿，不改业务 UI。目标是让两个产品面使用同一个品牌和语义系统：Node 采用深色表面，Manage 采用浅色表面，明度随环境切换，色相和状态含义保持不变。

## 设计结论

- **80% 中性表面**：页面底、侧栏、卡片、浮层和边框用中性蓝灰层级区分。内容密度由表面层级处理，普通 Agent 与工作组不各自染色。
- **15% 品牌冷色**：品牌雪花衍生冰蓝/青色，承担品牌标识、主按钮、选中态、焦点环和可执行操作。Node 和 Manage 共享相同色相，只使用深浅强度保证对比度。
- **5% 语义强调**：只用于成功、警告、危险、提示、处理中等状态；不能把语义色当作装饰大面积铺满界面。
- **Auto 使用受控靛紫**：靛紫只标识自主运行、唤醒、Dreaming 和 Auto 总览，作为品牌冷色的辅助色。普通 Agent 与工作组沿用中性表面和品牌冷色，避免分组各自变成彩色标签。

## Token 表

| token | Node dark | Manage light | 作用 |
| --- | --- | --- | --- |
| `surface-0` | `#0B1220` | `#F8FAFC` | 页面底、应用背景 |
| `surface-1` | `#111C2D` | `#F1F5F9` | 侧栏、导航、次级区域 |
| `surface-2` | `#16263B` | `#FFFFFF` | 卡片、输入框、浮层 |
| `border-subtle` | `#334962` | `#D4DEE9` | 分隔线、边框、非激活轮廓 |
| `text-primary` | `#F6FAFF` | `#172337` | 标题和正文 |
| `text-muted` | `#A7BAD0` | `#617289` | 辅助说明、元数据 |
| `brand-300` | `#A7F3FF` | `#1496B3` | 深色图标 / 浅色文本或按钮 |
| `brand-500` | `#55D9EE` | `#2BBFD9` | 主按钮、hover、活动提示 |
| `brand-600` | `#2BBFD9` | `#1496B3` | active、focus ring、已选中 |
| `auto-500` | `#8A83F5` | `#5D59C4` | Auto 持续运行和自主性辅助色 |
| `success` | `#35B77A` | `#16824F` | 成功、已完成 |
| `warning` | `#D99B2B` | `#9B6700` | 警告、需要关注 |
| `danger` | `#E56369` | `#B4232F` | 失败、删除、危险操作 |
| `info` | `#3E9BD6` | `#176A9A` | 提示、帮助、信息 |
| `pending` | `#6B8DAA` | `#466B89` | 排队、连接中、处理中 |

浅色列采用更深的文字和语义色，深色列采用更亮的文字和语义色。实现时应将这些 token 放入 Node 与 Manage 各自的主题变量中，而不是在组件中复制一套近似的蓝色或青色。

## 组件使用规则

| 场景 | 表面 | 前景 / 边框 | 说明 |
| --- | --- | --- | --- |
| icon normal | `surface-1` | `text-muted` | 普通 Agent、工作组和导航默认状态 |
| icon hover | `surface-2` | `brand-500` | 提升表面并出现冷色轮廓 |
| icon active | `surface-2` 或选中层 | `brand-600` + focus ring | 选中项要有轮廓或表面变化，不能只依赖颜色 |
| icon disabled | 原表面降低不透明度 | `text-muted` 降低强度 | 保持可辨认，避免整块消失 |
| Auto active | Auto surface + `auto-500` | 靛紫 | 仅 Auto 使用，体现持续唤醒，不改变普通对话的品牌色 |
| success / warning / danger / info / pending | 中性表面 | 对应统一语义色 | 同时配合图标、文字或状态标签 |

图标本身继续遵循 [dagents-icon-system.svg](./dagents-icon-system.svg) 的 24×24、1.6px、`currentColor` 规则。按钮负责 `aria-label` 和焦点环，SVG 保持 `aria-hidden="true"`。

## 对比度与可访问性

- 正文与其背景以 **4.5:1** 为目标；大字号标题和图形以 **3:1** 为最低目标。深色和浅色主题分别使用表中的强度，不直接把 Node 的亮色复制到 Manage 的白色背景。
- 状态不能只用颜色传达。成功使用勾选图标和“已完成”，危险使用错误图标和动作文案，处理中使用等待图标和状态文字。
- disabled 可以降低强调度，但需要保留对象轮廓和足够的文字识别度；如果操作不可用，还应提供原生 `disabled` 或等效可访问状态。
- focus ring 使用 `brand-600`，并与邻近边框保持可见间隔；不要用 `outline: none` 消除键盘焦点。
- Auto 靛紫只作辅助标识，不能用来表示错误、成功或权限。语义色永远优先于产品模式色。

## 第三方品牌色例外

DeepSeek、OpenAI、Mimo、企业微信及未来的 GitHub/GitLab 等连接器色彩表示外部身份或数据来源，应保留供应商徽标和其品牌色。系统 token 只描述“模型”“连接器”“工具”这一层语义，不用通用图标或 DAgents 语义色伪造第三方品牌。品牌色也不参与 success、danger、pending 等系统状态判断。

## 落地顺序

1. 在 Node 和 Manage 的主题入口分别建立同名 CSS 变量，保留上表的 token 名称。
2. 先迁移全局背景、卡片、边框、正文和辅助文字，再迁移图标、按钮和状态标签。
3. 将侧栏 Agent、工作组、Auto 的颜色收敛到“中性 + 品牌冷色 + Auto 靛紫”规则，删除组件内部的近似色值。
4. 对深色 Node、浅色 Manage、hover/active/disabled 和五类状态做截图回归，并用对比度工具检查正文、焦点环与状态卡片。
