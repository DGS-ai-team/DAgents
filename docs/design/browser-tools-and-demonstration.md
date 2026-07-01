# 内置 Browser Tools 与人类操作演示（Demonstration）

本文记录 **Go Agent Node** 侧「录制人在任意网站的操作 → LLM 模仿复现」的产品与技术方案。

**状态（2026-07）**：

| 阶段 | 状态 | 说明 |
|------|------|------|
| **Phase 1 / 1.5** | **已落地** | `browser_*` + **模式 A**（Go RemoteDriver + Python **browser-use** 薄服务） |
| Phase 2–4 | 规划中 | 录制 / 回放 / WebUI |

**架构决策（2026-07，已认可）**：

1. 内网机器 **均已安装 Chrome**（含 Server 2012/2016/2019）；**不**使用 Playwright 自带 Chromium。
2. **模式 A（已定稿）**：Go `BrowserManager` → `RemoteDriver` → 本机 **`dagents-browser`**（Python **browser-use** + CDP）；Go 内不嵌入 rod/CDP。
3. 演示轨迹 **`demonstration/v1`** 与引擎无关；步骤可来自 CDP 录制或 Chrome 扩展导出。

**相关索引**：[go-node-compatibility.md](../architecture/go-node-compatibility.md)、[go-node-internals.md](../architecture/go-node-internals.md)、[04-能力与策略.md](../handbook/04-能力与策略.md)。

---

## 1. 目标与约束

### 1.1 用户目标

1. **录制**：人在 **本机 Chrome** 中操作任意网站，生成结构化 **操作轨迹**。
2. **模仿**：LLM 读取轨迹，在 **同一台机器** 上分步复现（经 `browser_*`，非 `bash_run` 黑盒脚本）。
3. **对齐**：失败时用 **截图 + vision**（`read_image` / `multimodal.enabled`）修正 selector。

### 1.2 已确认约束

| 项 | 决策 |
|----|------|
| 浏览器 | **内网机器已安装 Chrome**（版本由内网统一管控，项目需验收矩阵） |
| 录制场景 | 任意网站（非仅 `/ui/`） |
| 执行环境 | 与录制 **同一台机器**、**同一 Chrome 生态**（CDP） |
| Node 定位 | **Win Server 2012 R2+** 可跑 Go Node；browser 能力 **不** 以 Playwright 官方 OS 列表为准 |
| 主输入 | 结构化 `demonstration/v1` JSON；video **不** 进 LLM |
| 栈归属 | Go `node/internal/browser/`、`node/internal/tools/` |

### 1.3 非目标

- 跨机器远程回放（A2A 远程 browser 网关列为后续可选，非 MVP）。
- 替代 Playwright Test / 全量 E2E 框架。
- 静默录制未授权标签页。
- 纯 IE 页面（无 Chrome 时另议 UIA / PS COM 兜底，非主路径）。

---

## 2. 总体架构

```text
┌──────────────────────────────────────────────────────────────────┐
│                        Go Agent Node                              │
│  browser_* tools ──► BrowserManager ──► RemoteDriver (HTTP)      │
│       │                      │              │                     │
│  demonstration_*             │              ▼                     │
│       │                      │     dagents-browser (Python)       │
│       └──────────────────────┘     browser-use + 本机 Chrome CDP  │
└──────────────────────────────────────────────────────────────────┘
```

**职责分离**：

| 层 | 职责 |
|----|------|
| **`browser_*`（Go）** | Agent 执行面、Policy/HITL、URL 校验 |
| **`dagents-browser`（Python）** | browser-use DOM 序列化 + CDP 动作 |
| **录制（Phase 2）** | CDP 监听或扩展；写 `demonstration/v1` |
| **`demonstration_*`** | 读 manifest → 映射 `browser_*` 分步回放 |

详见 [browser-remote-service-mode-a.md](./browser-remote-service-mode-a.md)。

---

## 3. Chrome 连接方式

| | **attach（`cdp_url` 非空）** | **launch（默认）** |
|--|---------------------------|-------------------|
| 浏览器来源 | **已有 Chrome 实例** | Agent 按 session 启动 Chrome |
| profile | 用户已有 profile | `fs_root/browser/profiles/{session}` |
| stop 行为 | 仅关闭 tab，**不**关闭浏览器 | 关闭浏览器并清理 launcher |
| SSO / 内网登录 | **可复用已登录会话** | 独立 profile，需重新登录 |

---

## 4. 支持矩阵（对外声明）

### 4.1 dagents-node 核心（不变）

见 [go-node-compatibility.md](../architecture/go-node-compatibility.md)：**Win Server 2012 R2+**（Go 静态二进制）。

### 4.2 `browser_*` / 演示录制

| 环境 | 支持级别 |
|------|----------|
| **Win Server 2012 R2 / 2016 / 2019** + **已装 Chrome** + **桌面/RDS 会话** | **正式支持目标**（须在内网 Chrome 版本上 **项目验收**） |
| 无 GUI（Server Core） | **不支持**「录人操作」；headless CDP 回放可 POC |
| 无 Chrome | 不支持 |

**内网须确认**：2012/2016 上 Chrome **主版本号**；manifest 记录 `chrome_version` 便于回放校验。

---

## 5. 实施阶段

| 阶段 | 内容 | 交付物 |
|------|------|--------|
| **Phase 1 / 1.5** | Browser Tools + 模式 A | ✅ `browser_*`、RemoteDriver、`browser-service`（browser-use） |
| **Phase 2** | 录制 | Go CDP 事件 → `demonstration/v1`；可选 Chrome 扩展导出 |
| **Phase 3** | 回放 | `demonstration_*` + `browser_*` + Skill |
| **Phase 4** | WebUI + HTTP API | record start/stop、demo 列表 |

---

## 6. 已实现：模式 A + browser-use

### 6.1 包布局

| 路径 | 说明 |
|------|------|
| `node/internal/browser/remote_driver.go` | Go → HTTP |
| `node/internal/browser/manager.go` | session 管理、URL 校验 |
| `browser-service/dagents_browser/` | browser-use 驱动 Chrome |
| `shared/config/browser.go` | `service_url`、`chrome_path`、`cdp_url` 等 |

### 6.2 配置

```yaml
browser:
  enabled: true
  service_url: http://127.0.0.1:18766
  headed: true
  chrome_path: ""
  cdp_url: ""
  debug_port: 9222
  default_timeout_ms: 30000
  output_dir: browser
  max_sessions: 1
  allowed_url_schemes: [https, http]
  ignore_https_errors: false
```

### 6.3 依赖

- **Go Node**：无 rod；仅 HTTP 客户端
- **dagents-browser**：Python 3.11+、`browser-use`、本机 Chrome
- **不需要**：Playwright 自带 Chromium、Node sidecar

---

## 7. 工具清单

`browser_start` / `browser_stop` / `browser_navigate` / `browser_click` / `browser_fill` / `browser_press` / `browser_screenshot` / `browser_wait` / `browser_snapshot`。

### 7.1 交互模式（随 `multimodal.enabled` 自动切换）

| `multimodal.enabled` | 模式 | 主路径 |
|----------------------|------|--------|
| `false`（默认） | **非视觉** | `browser_snapshot` → `llm_representation` + **index** → `browser_click` / `browser_fill` |
| `true` | **视觉** | `browser_snapshot`（**自动截图 + 注入 vision**）→ `browser_click_coordinate(x,y)`；index/selector 作 fallback |

```text
# 非视觉（multimodal.enabled: false）
browser_start → browser_navigate → browser_snapshot → browser_click(index=N)

# 视觉（multimodal.enabled: true，须 vision 模型）
browser_start → browser_navigate → browser_snapshot  # 截图自动注入 LLM
  → browser_click_coordinate(x, y) / browser_fill(index=…)
browser_screenshot  # 亦自动注入，无需 read_image
```

| 工具 | 非视觉 | 视觉 |
|------|--------|------|
| `browser_snapshot` | `llm_representation` + index 列表 | 同上 + **PNG 截图自动 vision 注入** |
| `browser_click` | **index** 主路径 | fallback |
| `browser_click_coordinate` | 不可用 | **坐标主路径** |
| `browser_screenshot` | 返回路径，需 `read_image` | **自动 vision 注入** |

**扩展工具（对齐 browser-use 原生 action）**：

| 工具 | browser-use | 说明 |
|------|-------------|------|
| `browser_search` | `search` | 搜索引擎查询 |
| `browser_go_back` | `go_back` | 后退 |
| `browser_scroll` | `scroll` | 滚动页面/元素 |
| `browser_find_text` | `find_text` | 滚动到文本 |
| `browser_switch_tab` | `switch` | 切换标签（`tab_id` 见 snapshot `detail.tabs`） |
| `browser_close_tab` | `close` | 关闭标签 |
| `browser_extract` | `extract` | 页面 LLM 提取（需 config `llm.model`） |
| `browser_evaluate` | `evaluate` | 执行 JS |
| `browser_find_elements` | `find_elements` | CSS 查元素 |
| `browser_search_page` | `search_page` | 页面内 grep |
| `browser_upload_file` | `upload_file` | 上传 FS_ROOT 内文件 |
| `browser_dropdown_options` | `dropdown_options` | 下拉选项列表 |
| `browser_select_dropdown` | `select_dropdown` | 选择下拉项 |

配置示例：

```yaml
multimodal:
  enabled: true   # 开启后 browser_* 自动切视觉模式
browser:
  enabled: true
  service_url: http://127.0.0.1:18766
tools:
  enabled_groups: [browser]
```

详见 `node/internal/tools/browser_tool.go` 与 `node/internal/browser/README.md`。

---

## 8. Phase 2：录制（Demonstration Record）

### 8.1 存储

```text
{fs_root}/demonstrations/{demo_id}/
  manifest.json
  steps/*.png
```

### 8.2 `demonstration/v1`（引擎无关）

```json
{
  "version": 1,
  "demo_id": "demo-20250701-143022",
  "engine": "cdp-chrome",
  "chrome_version": "109.0.5414.120",
  "recorder": "cdp-record-v1",
  "viewport": { "width": 1280, "height": 720 },
  "start_url": "https://intranet.example/form",
  "steps": [ "..."]
}
```

**`engine` 枚举**：`cdp-chrome` | `chrome-extension`（扩展导出）。

**采集（主）**：CDP 订阅 `Input`、`Page` 等 → selector + 坐标 + 关键帧 `Page.captureScreenshot`。

**采集（辅）**：Chrome 扩展录 DOM 步骤 → 导入 manifest（适合 SSO 场景人工操作）。

### 8.3 流程

1. `record start` → 确保 Chrome 带 CDP（attach 或 Agent 拉起）。  
2. 用户在本机 Chrome 操作。  
3. `record stop` → 写 manifest。  

---

## 9. Phase 3：模仿回放

`demonstration_load` → `browser_start`（attach 同 profile）→ `demonstration_replay_step` / 直接 `browser_*` → 失败 vision 对齐。

Skill 禁止默认 `bash_run` Playwright 脚本。

---

## 10. Phase 4：WebUI + API

录制控制面调用 CDP attach。

---

## 11. 内网 / 离线发布

| 组件 | 要求 |
|------|------|
| dagents-node | 必装 |
| 本机 Chrome | **必装（前提）** |
| Node.js + Playwright + Chromium | **不需要** |

---

## 12. 安全与合规

- CDP 端口 **127.0.0.1**；禁止暴露到内网网卡。  
- `browser_start` / `browser_navigate` 默认 HITL。  
- 内网 URL 可选 host 白名单（P2）。  
- manifest 密码字段 `redacted: true`。  
- 演示数据默认不出 `fs_root`。

---

## 13. 验收标准

**Phase 1 / 1.5（CDP）**

1. Win Server **2016/2019** 内网 Chrome 上：`browser_start` → `navigate` → `click` → `screenshot` 成功。  
2. **2012 R2** 在内网 Chrome 验收矩阵内通过同等 smoke。  
3. 无 Node.js 进程；`go test` 绿。  
4. SSO 场景：attach 已登录 Chrome 可回放登录后路径（POC）。

**Phase 2–3**：同初稿，engine=`cdp-chrome`。

---

## 14. 后续扩展

- `browser_connect`：显式 attach 用户已打开的 Chrome（仅 CDP URL）。  
- `browser_snapshot`：DOM / a11y 摘要。  
- 远程 browser 执行器（老 Node 无 Chrome 时的 A2A 跳板，非 MVP）。  
- UIA 兜底（无 Chrome / 纯 IE 控件，极少数）。

---

## 15. 变更记录

| 日期 | 说明 |
|------|------|
| 2026-07-01 | 初稿：Playwright launch；先做 web tools |
| 2026-07-01 | Phase 1 过渡 sidecar（已废弃） |
| 2026-07-01 | **架构修订**：CDP attach + Go 为主路径 |
| 2026-07-01 | **模式 A**：RemoteDriver + `browser-service`（browser-use）；删除 Go rod/CDP 与 interim Go 薄服务 |
