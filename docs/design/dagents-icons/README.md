# DAgents 独立图标资产

本目录是 DAgents 的离线 SVG 图标源文件。每个语义 key 都有一个同名文件，便于 Node、Manage、文档和原生壳按需复用；它不改变现有业务 UI。

## 资产规则

- 每个文件都是独立的 `24×24` SVG，带自己的 `viewBox="0 0 24 24"`、标题和描述。
- 功能图标只使用 `currentColor`，线宽为 `1.6px`，统一 round cap / round join；文件内没有主题色、外部字体、网络资源、`defs` 或 `use` 依赖。
- `brand-snowflake.svg` 是现有 `shared/branding/brand-icon.png` 的六臂宽圆角外轮廓，只有一个平滑 contour，不包含眼睛、笑脸、中心圆或其他内部装饰。品牌轮廓提供 `brand-snowflake-16.svg` 小尺寸变体，两个文件都使用 `currentColor`。
- 页面或组件可以直接使用 `<img>`，也可以用 CSS `mask-image` 让 `currentColor` 继承组件语义色。离线 HTML 画板采用后者展示 Node dark 与 Manage light 两套表面。

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

上一版画板按视觉卡片统计为 60 个，但 `expand` 与 `collapse` 现在是两个独立 key，所以可交付目录包含 61 个语义文件，另提供一个 16px 品牌变体。

### 品牌与导航

`brand-snowflake`、`nav-agents`、`nav-workgroups`、`nav-auto`、`nav-node`、`nav-manage`、`nav-settings`、`nav-feedback`、`create`、`search`、`menu`、`more`

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
| `nav-auto.svg` | `auto` | nav variant | 自主智能体分组导航使用轨道 / 脉冲轮廓 |
| `tool-skills.svg` | `skills` | alias | 工具能力面板中的技能入口 |
| `tool-memory.svg` | `memory` | alias | 工具能力面板中的记忆入口 |
| `brand-snowflake-16.svg` | `brand-snowflake` | size variant | 16px 独立轮廓，适合 favicon 与紧凑品牌锚点 |

`expand` 与 `collapse` 是相反方向的操作 key，不再合并成一个含糊的 `expand-collapse` 文件。旧画板中可能出现的 `dashboard`、`check`、`close` 等是画板内部辅助符号或历史候选，不属于本轮 61 个交付 key；产品中如要使用，应继续复用现有组件语义。

## 颜色 token

图标文件不写死颜色。Node 与 Manage 应从同一套语义 token 继承颜色：中性文字使用 `text-muted`，普通品牌入口使用 `brand-500` / `brand-600`，Auto 相关 key 使用受控的 `auto-500` 靛紫，工作组和普通 Agent 保持品牌冷色，不各自染色。状态 key 映射为 `success`、`warning`、`danger`、`info`、`pending`；pending 使用统一蓝灰色。建议表面比例为约 80% 中性、15% 品牌冷色、5% 语义强调。

第三方 provider、连接器和数据来源（例如 OpenAI、DeepSeek、Mimo、企业微信）保留各自的官方品牌资源，不用本图标集替代品牌标识。完整颜色 token、Node dark / Manage light 对比度示例见 `../dagents-color-system.md`；可视化预览见 `../dagents-icon-gallery.html`。
