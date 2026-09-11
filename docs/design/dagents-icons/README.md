# DAgents 独立图标资产

本目录是 DAgents 的离线功能 SVG 图标源文件。每个功能语义 key 都有一个同名文件，便于 Node、Manage、文档和原生壳按需复用；它不改变现有业务 UI。品牌雪花不属于这套功能 SVG 资产。

## 资产规则

- 每个文件都是独立的 `24×24` SVG，带自己的 `viewBox="0 0 24 24"`、标题和描述。
- 功能图标只使用 `currentColor`，线宽为 `2px`，统一 round cap / round join；文件内没有主题色、外部字体、网络资源、`defs` 或 `use` 依赖。
- 品牌雪花只复用现有 `shared/branding/brand-icon.png`，用于程序图标、托盘图标、启动页、favicon 和其他品牌展示；不生成、不维护 `brand-snowflake.svg` 或 `brand-snowflake-16.svg`。
- 页面或组件可以直接使用 `<img>`，也可以用 CSS `mask-image` 让 `currentColor` 继承组件语义色。离线 HTML 画板采用后者展示 Node dark 与 Manage light 两套表面。

## Lucide 复用映射

`../dagents-icon-gallery.html` 在每张卡片上标注 Lucide 名称和来源策略；direct 卡片实际加载本地 `lucide/` vendor SVG，composed/custom 使用 DAgents fallback。画板不从网络加载 Lucide，也不新增 npm 依赖。`direct Lucide reuse` 表示语义和几何可直接采用官方图标，`composed` 表示用官方原语组合产品关系，`custom` 只保留 DAgents 专属形态。

direct 候选的官方 SVG 已复制到 `lucide/` vendor 目录，Gallery 会在 direct 卡片中实际加载这些本地文件；内联 DAgents fallback 保障 `file://` 或 CSS mask 不可用时仍可预览。来源、抓取日期、ISC License 和当前 main 分支说明见 `lucide/README.md`。

- Auto 复用普通 Agent 的 `Bot` 主体，右上角叠加小型 `Sparkles`，表达轻量自主循环；`wake-trigger` 继续使用 `AlarmClock` 表达唤醒。
- `child-agent` 使用 `GitBranch + Bot`，表达父子委派关系；`auto-overview` 直接使用 Lucide `ReceiptText` 表达任务记录与回执；`Network` 保留给工作组语义。
- 公共入口、工具和状态优先映射到 `Bot`、`Network`、`UsersRound`、`Server`、`LayoutDashboard`、`Settings`、`MessageCircle`、`CirclePlus`、`Search`、`Menu`、`Ellipsis`、`MoonStar`、`BookOpen`、`ListChecks`、`SlidersHorizontal`、`Cpu`、`Sparkles`、`Wrench`、`Brain`、`AlarmClock`、`ShieldCheck`、`KeyRound`、`Megaphone`、`ArrowLeftRight`、`Inbox`、`IdCard`、`ScrollText`、`UserPlus`、`FolderTree`、`FolderOpen`、`File`、`PanelTop`、`Terminal`、`SquareTerminal`、`Plug`、`Clipboard`、`Upload`、`CircleCheck`、`TriangleAlert`、`CircleX`、`Info`、`LoaderCircle`、`Play`、`Square`、`RefreshCw`、`Save`、`Pencil`、`Trash`、`ChevronDown`、`ChevronUp` 等候选。

品牌展示统一直接使用 `shared/branding/brand-icon.png`；它不进入功能图标的 key、alias 或 SVG 映射。`nav-node` 优先采用 Lucide `Server`，现有 DAgents 节点 SVG 仅作为离线 fallback，不改变产品入口语义。原候选 `Trash2` 在官方当前 `main/icons` 中不存在，删除语义改用官方 `Trash`，详情见 vendor README。

```html
<span class="icon-mask" style="--icon-url: url('../design/dagents-icons/auto.svg')" aria-hidden="true"></span>
```

```css
.icon-mask {
  width: 24px;
  height: 24px;
  display: inline-block;
  color: var(--brand-600);
  background: currentColor;
  -webkit-mask: var(--icon-url) center / contain no-repeat;
  mask: var(--icon-url) center / contain no-repeat;
}
```

## key 清单

当前画板包含 60 个功能视觉卡片；`expand` 与 `collapse` 是两个独立 key，因此可交付目录包含 60 个功能语义 SVG 文件。品牌雪花使用现有 PNG，不计入功能图标清单。画板每张卡同时显示 24px 主图和 16px smoke test，重点检查 `file`、`audit-log`、`release-update`、`mcp`、`pending`、`dreaming` 等细节密集图标在小尺寸下的可辨识度。

### 品牌与导航

`nav-agents`、`nav-workgroups`、`nav-auto`、`nav-node`、`nav-manage`、`nav-settings`、`nav-feedback`、`create`、`search`、`menu`、`more`

### 智能体与 Auto

`agent`、`auto`、`auto-overview`、`wake-trigger`、`dreaming`、`experience-handbook`、`todo-list`、`autonomy`、`model`、`skills`、`tools`、`memory`

### 工作组与管理

`workgroup`、`workgroup-members`、`manage-dashboard`、`admin-access`、`publish`、`sync`、`feedback-inbox`、`release-update`、`agent-registry`、`audit-log`、`invite`、`permissions`

### 工具与工作区

`filesystem`、`folder-open`、`file`、`terminal`、`browser`、`mcp`、`child-agent`、`shell`、`clipboard`、`upload`、`tool-skills`、`tool-memory`

### 状态与操作

`success`、`warning`、`error`、`info`、`pending`、`run`、`stop`、`refresh`、`save`、`edit`、`delete`、`expand`、`collapse`

## alias 与 variant

| 文件 | 基础语义 | 类型 | 说明 |
| --- | --- | --- | --- |
| `nav-agents.svg` | `agent` | nav variant | 智能体分组导航使用相同人形轮廓 |
| `nav-workgroups.svg` | `workgroup` | nav variant | 工作组分组导航使用三节点轮廓 |
| `nav-auto.svg` | `auto` | nav variant | 自主智能体分组导航复用 Bot + Sparkles，16px 保留主体与识别点 |
| `tool-skills.svg` | `skills` | alias | 工具能力面板中的技能入口 |
| `tool-memory.svg` | `memory` | alias | 工具能力面板中的记忆入口 |

Auto 图标以普通 Bot 主体 + 右上角小型 Sparkles 表达自主循环，不使用眼睛或行星式中心；画板中的 16px variant 保留 Bot 轮廓与 Sparkles 识别点，唤醒语义由独立的 AlarmClock 图标承担。Auto 总览直接采用 Lucide `ReceiptText`，在 16px 下保留纸张轮廓与文字线条，避免与工作组 `Network` 混淆。`workgroup` 使用三个同等节点；`child-agent` 使用更大的父节点、两个更小的子节点和方向箭头；`model` 使用芯片和层级引脚；`pending` 使用进度弧与三点，避免像 `refresh` 一样出现成对循环箭头。

`expand` 与 `collapse` 是相反方向的操作 key，不再合并成一个含糊的 `expand-collapse` 文件。旧画板中可能出现的 `dashboard`、`check`、`close` 等是画板内部辅助符号或历史候选，不属于本轮 60 个交付 key；产品中如要使用，应继续复用现有组件语义。

## 颜色 token

图标文件不写死颜色。Node 与 Manage 应从同一套语义 token 继承颜色：中性文字使用 `text-muted`，普通品牌入口使用 `brand-500` / `brand-600`，Auto 相关 key 使用受控的 `auto-500` 靛紫，工作组和普通 Agent 保持品牌冷色，不各自染色。状态 key 映射为 `success`、`warning`、`danger`、`info`、`pending`；pending 使用统一蓝灰色。建议表面比例为约 80% 中性、15% 品牌冷色、5% 语义强调。

第三方 provider、连接器和数据来源（例如 OpenAI、DeepSeek、Mimo、企业微信）保留各自的官方品牌资源，不用本图标集替代品牌标识。完整颜色 token、Node dark / Manage light 对比度示例见 `../dagents-color-system.md`；可视化预览见 `../dagents-icon-gallery.html`。
