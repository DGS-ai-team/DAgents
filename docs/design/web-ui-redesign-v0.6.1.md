# Web UI 产品化重构（v0.6.1）

**状态（2026-07）**：**已实现**（待 Smoke 与 tag `v0.6.1`）。  
**版本**：Git tag **`v0.6.1`**（与 Media API、`show_image` 同 tag 交付）。  
**读者**：Web UI / Node 前端实现；与 [node-ui-media-display.md](./node-ui-media-display.md)、[windows-desktop-shell.md](./windows-desktop-shell.md) 配套。

---

## 1. 背景与目标

### 1.1 问题

v0.6.0 通过 Shell + Hydrate 解决了「桌面用户离线也能收到 HITL」，但 **Web UI 仍是 power-user 控制台**：

- 常驻展示 SSE、API URL、token、session id 等 **运维信息**；
- Skills / Policy / Triggers / Update 等 **几乎只能靠 slash 命令** 发现；
- 工具执行、压缩、子 Agent 等 **开发者向日志** 打断阅读流；
- Node 旁路 SSE（trigger/A2A deferred）在 UI **未订阅**，功能缺口；
- browser/read_image 截图 **不可见**（→ Media 轨道另文）。

### 1.2 目标 persona（已确认）

| 项 | 决策 |
|----|------|
| **Primary** | **Windows 普通桌面用户**（经 Shell 安装、Toast 拉进 UI，非 Agent 开发者） |
| **Secondary** | Power user / 运维仍可用 **slash 命令** 与 **可折叠诊断**，但不占主界面 |

### 1.3 v0.6.1 交付定义

**「能看图的产品化 UI 1.0」** = Media 能力 + 布局/信息架构重构 + 关键 bugfix + Triggers 基础管理。

用户可感知价值：

1. 像 **聊天产品** 一样对话、审批、切换会话；  
2. Agent 截图/读图 **在气泡里直接看到**；  
3. F5 / 切换 session 后 **历史与图片仍在**；  
4. 定时任务可在 **设置里改、开、关**；  
5. 需要排障时 **展开诊断**，默认不打扰。

### 1.4 非目标（v0.6.1）

- Shell 代理 Media / 一键应用更新（→ v0.6.2）；  
- Triggers **手动 fire**、执行 history 浏览（→ v0.7 或更晚）；  
- Policy **新建规则向导**（仅保留现有编辑能力 + 设置入口）；  
- 暗色主题完整落地（→ v0.6.2 可选 F-UI15）；  
- TUI 对齐（→ v0.7.0）。

---

## 2. 产品原则

| # | 原则 | 说明 |
|---|------|------|
| P1 | **用户语言** | 界面用「助手正在处理…」「需要你确认一步操作」；隐藏 SSE、ctx、tool loop |
| P2 | **聊天优先** | ≥75% 视口给消息流 + 输入框；设置/诊断为二级 |
| P3 | **渐进披露** | 工具默认 **一行摘要**；点击展开详情；reasoning/compression 默认不可见 |
| P4 | **可见即可用** | 顶栏/侧栏图标进设置；slash 保留为高级快捷方式 |
| P5 | **与 Shell 一致** | 会话 **未读/待审批** badge 与 Shell 待办语义对齐（F-E13） |
| P6 | **破坏性可接受** | 0.x 允许改 URL 结构、目录重组、移除常驻 Runtime 面板 |

---

## 3. 信息架构与布局

### 3.1 路由（vue-router，已确认接受）

| 路径 | 视图 | 说明 |
|------|------|------|
| `/` | redirect → `/chat` 或上次 session | |
| `/chat` | `ChatView` | 无 session 时 ensure 或空态 |
| `/chat/:sessionId` | `ChatView` | 主聊天；`:sessionId` 与 `?session=` **并存**，router 优先 |
| `/settings` | `SettingsLayout` | 设置壳层 |
| `/settings/general` | 模型、思考模式（原 `/thinking`） | |
| `/settings/skills` | Skills 加载/卸载 | 原 SkillsPanel |
| `/settings/triggers` | 触发器列表 + 编辑/启停 | 见 §5.4 |
| `/settings/security` | 工具/Shell 策略 | 原 PolicyPanel，简化文案 |
| `/settings/about` | 版本、更新检查 | 原 UpdatePanel + Status 摘要 |

**深链**：Shell Toast → `/ui/chat/:id` 或 `/ui/?session=`（兼容 v0.6.0）；实现时 router 统一 normalize。

### 3.2 目标布局

```text
┌──────────────────────────────────────────────────────────────────┐
│  DAgents          ● 在线                    ⚙ 设置   ? 帮助      │
├───┬──────────────────────────────────────────────────────────┬───┤
│ 会 │                                                          │ ▶ │
│ 话 │              聊天消息流                                   │ 诊 │
│  │ │              (assistant / 图片 / 审批 / 一行工具摘要)      │ 断 │
│ 列 │                                                          │   │
│ 表 │  ┌─ 待审批 sticky（有 HITL 时）────────────────────┐    │ 可 │
│   │  └──────────────────────────────────────────────────┘    │ 折 │
│ + │  ┌──────────────────────────────────────────────────┐    │ 叠 │
│   │  │ 输入消息…                          📎  发送       │    │   │
│   │  └──────────────────────────────────────────────────┘    │   │
└───┴──────────────────────────────────────────────────────────┴───┘
 ~240px                        flex 1                         ~320px
```

- **左侧**：会话列表（标题、相对时间、**未读点**、**待审批 badge**、「进行中」）；底部「+ 新对话」。  
- **中间**：仅聊天 + Composer；无 token/ctx 常驻条。  
- **右侧**：**默认可折叠**；展开后为 **「诊断」** 面板（非产品主路径）。  
- **顶栏**：连接状态点（绿/黄/红）；**不**常驻 `agent_id` / model 字符串（model 进设置 › 通用）。

### 3.3 可折叠「诊断」面板（已确认）

替代 v0.6.0 常驻 `RuntimeStatusPanel` + 部分 `StatusPanel` 内容。

| 区块 | 默认 | 内容 |
|------|------|------|
| **连接** | 折叠为一行摘要 | 「实时连接正常」/「连接中断，正在重连…」 |
| **展开后** | | SSE 状态、API 地址（可复制）、当前 session id、模型名 |
| **活动** | 可选 | 子 Agent 数量、HITL 队列长度（链到聊天内 sticky） |
| **高级** | 折叠 | 「打开上下文详情」→ 原 ContextPanel 精简版或链 `/settings` 开发者小节 |

**持久化**：折叠状态 `localStorage` key `dagents.ui.diagnosticsExpanded`（默认 `false`）。

**错误处理**：`sessionStore.error` 以 **聊天区顶部横幅** 展示，不仅藏在诊断里。

### 3.4 移除 / 降级展示

| 原位置 | v0.6.1 处理 |
|--------|-------------|
| 顶栏 `agent_id · model` | 移除；model → 设置 › 通用 |
| Composer 右条 `thinking · tokens · ctx` | 移除；thinking → 输入框旁小 toggle |
| Composer 左条 `HITL 队列 N` | 改为聊天内 **sticky 审批条** + 会话 badge |
| System `[compression]…` | 收入 **活动记录** 折叠组，或仅诊断可见 |
| Reasoning 流 | 默认隐藏；设置 › 通用 ›「显示思考过程（高级）」 |
| Overlay `/sessions` | **删除**；与会话侧栏合并 |
| 各 Panel JSON 切换 | 保留在 **诊断 › 高级** 或设置 › 开发者，主路径不可见 |

---

## 4. 聊天流体验

### 4.1 工具消息：一行摘要 + 可展开（已确认）

**默认（Tier 1）** — 单行、低视觉权重：

```text
✓  已读取  report.pdf
✓  已打开网页并截图
⏳ 正在执行  run_shell…
```

规则：

- 不展示 tool source badge（agent/child/a2a）于 Tier 1；展开后可见。  
- `read_file` / **`show_image`** / 带 `media[]` 的工具：**摘要行 + 内联缩略图**（Media 轨道 F-M3）。  
- 失败/拒绝：红色摘要 + 可选展开错误详情。

**展开（Tier 2）** — 点击摘要行：

- 工具名、参数摘要、耗时、来源 badge；  
- `read_file` → `ReadFileResultPreview`；  
- 其它 → 格式化文本或 code preview（原 verbose 内容）。  
- **不再**使用居中宽条 `tool-exec-bubble` 作为默认态。

**全局**：设置 › 高级 ›「始终展开工具详情」映射原 `/tools verbose`（默认 off）。

### 4.2 HITL

- **审批**：保持 inline `ApprovalBubble`；会话列表 + 顶栏 sticky「N 项待你确认」。  
- **UserInfo**：表单化选项；**修复** `App.vue` 未监听 `@user-info-selected`（F-UI0b）。  
- **A2A relay**：摘要文案用户向化（「另一台 Agent 请求执行…」），技术 id 仅展开可见。

### 4.3 输入框

| 项 | v0.6.1 |
|----|--------|
| 发送 | **Enter** 发送；**Shift+Enter** 换行（IM 习惯，已确认） |
| 附件 | 📎 图片（multimodal 开启时）；v0.6.2 路径粘贴 |
| Thinking | 输入框左侧 toggle（原 ComposerToolbar） |
| Placeholder | 「输入消息，或向助手提问…」 |

Slash 仍可用；输入 `/` 时可选显示 **命令提示下拉**（P2，不挡 v0.6.1 tag）。

### 4.4 活动记录（替代 system 消息洪流）

聊天流底部或「⋯ 活动记录」折叠块：

- 压缩完成、子 Agent 创建/结束、旁路消息入库 — **一行中文**，默认折叠。  
- 不插入主消息流中间（减少打断）。

---

## 5. 设置与能力暴露

### 5.1 设置导航

```text
设置
├── 通用      模型、思考模式、显示思考过程
├── 技能      已加载 / 可用 skills
├── 定时任务  触发器列表（编辑、启用、禁用）
├── 安全      工具与命令审批策略
└── 关于      版本、检查更新、复制升级命令
```

### 5.2 Triggers（已确认）

| 能力 | v0.6.1 |
|------|--------|
| 列表 | 名称、调度摘要、启用/禁用状态、下次运行（只读） |
| **编辑** | PATCH：名称、调度、cron/interval、任务模板（表单或 JSON 编辑器） |
| **启用/禁用** | Toggle → `PATCH` 或专用字段 |
| **新建** | POST 创建（基础表单：名称 + 调度 + prompt 模板） |
| **删除** | 确认对话框 |
| **手动 fire** | ❌ 不做 |
| **history** | ❌ v0.6.1 不做 |

API：`GET/POST/PATCH/DELETE /v1/triggers`（已有）；Web `api/node.js` 补封装。

### 5.3 子 Agent

- 设置 › 通用 或 诊断 › 活动：**运行中的临时 Agent** 列表；  
- **取消**按钮 → `POST …/child-agents/{id}/cancel`（F-UI11）；  
- 聊天内仍 **不** 刷屏子 Agent SSE（保持 v0.6.0 过滤策略）。

### 5.4 Skills / Policy / Update

- 从 overlay 迁入 **设置** 路由；交互基本保留，**文案中文化**。  
- Policy：不新增「创建规则向导」；表格 + 四档策略按钮保留。  
- Update：仍引导 CLI / v0.6.2 Shell 一键升级；关于页展示 Release notes。

---

## 6. Bugfix 与 Node 对齐（Phase 0）

| ID | 内容 | 文件 |
|----|------|------|
| **F-UI0a** | SSE 订阅 `user_message_deferred`、`side_effect_turn_start`、`side_effect_applied`、`side_effects_cleared` | `sse/stream.js` |
| **F-UI0b** | UserInfo 选项：`@user-info-selected` → `hitlSelected` | `App.vue` / `ChatView` |
| **F-UI0c** | 会话列表 `has_unread`、pending HITL badge | `SessionSidebar.vue` |
| **F-UI0d** | deferred 用户消息 + 旁路状态在 Tier 1 摘要中可见 | `transcript` + 活动记录 |

---

## 7. 前端工程结构（破坏性重组）

### 7.1 目录（目标）

```text
node/webui/frontend/src/
├── main.js
├── App.vue                 # 壳：router-view + 全局 SSE 生命周期
├── router/
│   └── index.js
├── layouts/
│   ├── ChatLayout.vue      # 顶栏 + 三栏
│   └── SettingsLayout.vue
├── views/
│   ├── ChatView.vue        # 原 App.vue 聊天逻辑下沉
│   └── settings/
│       ├── GeneralSettings.vue
│       ├── SkillsSettings.vue
│       ├── TriggersSettings.vue
│       ├── SecuritySettings.vue
│       └── AboutSettings.vue
├── components/
│   ├── chat/               # MessageBubble, Composer, ToolSummaryRow, …
│   ├── sidebar/            # SessionSidebar, DiagnosticsPanel
│   ├── hitl/               # ApprovalBubble, UserInfoBubble
│   └── settings/           # 设置页子组件
├── stores/                 # 保留，按域拆分可选
├── api/node.js
├── sse/stream.js
└── styles/
    ├── tokens.css
    ├── layout.css
    └── components/         # 自 workbench.css 拆分
```

### 7.2 迁移策略

1. **①** 引入 vue-router，`App.vue` 瘦身为 layout + SSE hub；  
2. **②** 复制现有组件到新路径，ChatView 接管事件分发；  
3. **③** 新布局 + DiagnosticsPanel；删除 RuntimeStatusPanel 常驻；  
4. **④** ToolSummaryRow 替换默认 ToolExecBubble 展示；  
5. **⑤** 设置页迁移，删除 overlay panel 路由；  
6. **⑥** 删 dead code（`ChildAgentsPanel.vue`、`StatusLineBubble.vue`、未用 store 字段）；  
7. **⑦** 拆分 `workbench.css`（可渐进，tag 前至少 tokens + layout）。

### 7.3 依赖

- 新增 `vue-router@4`（与 Vue 3.5 配套）；  
- 无新 UI 框架（仍纯 CSS + 设计 token）。

---

## 8. 功能 ID 总表（v0.6.1）

### 8.1 Media 轨道（不变，见 node-ui-media-display.md）

| ID | 内容 |
|----|------|
| F-M0 | `show_image` 工具 |
| F-M1 | MediaRegistry + GET media |
| F-M2 | browser/read_image/show_image 注册 + SSE `media[]` |
| F-M3 | ImageResultPreview + ToolExecBubble / MessageBubble |
| F-M4 | Hydrate transcript 含 `media` |
| F-M5 | 用户发图落盘 + Hydrate 回放 |
| F-H10 | F5 hydrate |
| F-H11 | Context 与 transcript 分工（Context 迁入诊断/高级） |

### 8.2 UI 重构轨道（本文件）

| ID | 优先级 | 内容 |
|----|--------|------|
| F-UI0a–d | P0 | §6 Bugfix + 旁路可见 |
| F-UI1 | P0 | vue-router + URL `/chat/:sessionId` |
| F-UI2 | P0 | 三栏布局：会话 / 聊天 / 可折叠诊断 |
| F-UI3 | P0 | 产品化顶栏（连接点、设置、帮助） |
| F-UI4 | P0 | 设置路由与导航（§5.1） |
| F-UI5 | P0 | Composer 简化 + Enter/Shift+Enter |
| F-UI6 | P0 | 工具 Tier1 摘要 + Tier2 展开 |
| F-UI7 | P1 | 活动记录折叠；system 消息降级 |
| F-UI8 | P0 | HITL sticky + 会话 badge |
| F-UI9 | P1 | 图片 lightbox（自 v0.7 F-M7 提前） |
| F-UI10 | P1 | Triggers 编辑/启停/新建/删除（无 fire） |
| F-UI11 | P1 | 子 Agent 列表 + cancel |
| F-UI12 | P2 | 文案中文化 + 移除主路径 JSON |
| F-UI13 | P2 | CSS 拆分（tokens/layout/components） |

### 8.3 明确不含

- F-M6 thumbnail、F-M8 TUI → v0.7.0；  
- F-UI15 暗色主题 → v0.6.2 可选；  
- Triggers fire/history、Policy 新建向导、Manage 上传表单 → 更晚版本。

---

## 9. 实现顺序

```text
Phase 0   F-UI0*              SSE 旁路 + UserInfo + 未读 badge
Phase 1   F-M1                MediaRegistry + GET API（Node）
Phase 2   F-UI1 + F-UI2 + F-UI3   router + 布局 + 顶栏 + 诊断折叠
Phase 3   F-M0 + F-M2 + F-M3 + F-UI6   Media 工具 + 摘要行 + 图片预览
Phase 4   F-UI4 + F-UI5 + F-UI8   设置壳 + Composer + HITL sticky
Phase 5   F-M4 + F-M5 + F-H10    hydrate media + 用户图落盘
Phase 6   F-UI10 + F-UI11      Triggers + 子 Agent cancel
Phase 7   F-UI7 + F-UI9 + F-UI12 + F-UI13   活动记录、lightbox、文案、CSS
```

**并行**：Phase 1（Node Media）与 Phase 2（前端骨架）可双人并行；Phase 3 依赖 Phase 1 API。

---

## 10. 验收（Smoke 扩展，tag v0.6.1）

完整检查表（含记录模板）：[`v0.6.1-smoke-checklist.md`](./v0.6.1-smoke-checklist.md)

摘要：

1. **布局**：两栏（会话 + 聊天）；顶栏连接点 + 设置；**无** SSE/API 常驻诊断栏。  
2. **对话**：Enter 发送；工具 **一行摘要** + 展开；hydrate 后 tool 行不重复。  
3. **图片**：`show_image` / browser 截图 + lightbox；F5 后仍在。  
4. **未读 / HITL**：会话 badge；inline 审批；UserInfo 选项正确。  
5. **旁路**：trigger deferred 消息可见。  
6. **Triggers**：设置 › 定时任务 — 编辑/启停/删除；**无** fire。  
7. **深链**：`?session=` 与 `/chat/:id` 可用。  
8. **设置**：显示思考过程、子 Agent 取消、上下文页。  
9. **Slash**：`/help`、`/reasoning` 等 power user 路径仍可用。

---

## 11. 相关文档

| 文档 | 关系 |
|------|------|
| [v0.6-v0.7-roadmap.md §3](./v0.6-v0.7-roadmap.md) | 版本功能 ID 与顺序 |
| [node-ui-media-display.md](./node-ui-media-display.md) | Media API 与 `show_image` |
| [windows-desktop-shell.md](./windows-desktop-shell.md) | Shell 深链、F-E13 未读 |

---

## 12. 变更记录

| 日期 | 变更 |
|------|------|
| 2026-07 | 初稿：产品决策确认（Windows 普通用户、可折叠诊断、工具摘要+展开、Triggers 编辑启停无 fire、vue-router） |
| 2026-07 | **实现完成**：两栏布局、设置导航、Media、F-UI7–13；Smoke 见 [`v0.6.1-smoke-checklist.md`](./v0.6.1-smoke-checklist.md) |
