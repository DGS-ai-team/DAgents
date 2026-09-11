# 内置 Browser Tools 与人类操作演示（Demonstration）

本文记录 **Go Agent Node** 侧「录制人在任意网站的操作 → LLM 模仿复现」的产品与技术方案。

**状态（2026-08）**：

| 阶段 | 状态 | 说明 |
|------|------|------|
| **Phase 1 / 1.5** | **已落地** | **伴生 Agent** + 任务级工具 + **模式 A**（RemoteDriver → `dagents-browser` / browser-use） |
| Phase 2–4 | 规划中 | 录制 / 回放 / WebUI（仍可能复用 CDP；产品回放不再依赖已退役的细粒度 `browser_*`） |

**架构决策（2026-07，已认可；2026-08 产品面收窄）**：

1. 内网机器 **均已安装 Chrome**（含 Server 2012/2016/2019）；**不**使用 Playwright 自带 Chromium。
2. **模式 A（已定稿）**：Go `BrowserManager` → `RemoteDriver` → 本机 **`dagents-browser`**（Python **browser-use** + CDP）；Go 内不嵌入 rod/CDP。
3. **主 Agent 仅任务级工具**（`browser_run_task` / `browser_task_status` / `browser_task_cancel`）；细粒度 DOM 动作在 sidecar `browser_use.Agent` 内闭环。
4. 演示轨迹 **`demonstration/v1`** 与引擎无关；步骤可来自 CDP 录制或 Chrome 扩展导出（Phase 2+）。

**相关索引**：[架构](../architecture.md)、[go-node-internals.md](../architecture/go-node-internals.md)、[04-能力与策略.md](../handbook/04-能力与策略.md)。

---

## 1. 目标与约束

### 1.1 用户目标

1. **录制**（Phase 2）：人在 **本机 Chrome** 中操作任意网站，生成结构化 **操作轨迹**。
2. **日常自动化（已落地）**：主 Agent 用自然语言任务派发给伴生 sidecar（`browser_run_task`）。
3. **模仿回放**（Phase 3）：LLM / Skill 读取轨迹，在 **同一台机器** 上复现（优先任务级或内部 CDP；非 `bash_run` 黑盒脚本）。
4. **对齐**：失败时用任务归档截图 + vision（`read_image` / `multimodal.enabled`）辅助修正。

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
│  browser_run_task* ──► BrowserManager ──► RemoteDriver (HTTP)    │
│       │                      │              │                     │
│  demonstration_*（规划）      │              ▼                     │
│       │                      │     dagents-browser (Python)       │
│       └──────────────────────┘     browser_use.Agent + Chrome CDP │
└──────────────────────────────────────────────────────────────────┘
```

**职责分离**：

| 层 | 职责 |
|----|------|
| **任务级 `browser_*`（Go）** | 主 Agent 执行面、伴生会话、Policy/HITL |
| **`dagents-browser`（Python）** | session + `browser_use.Agent` 闭环（内部 DOM/CDP） |
| **录制（Phase 2）** | CDP 监听或扩展；写 `demonstration/v1` |
| **`demonstration_*`（规划）** | 读 manifest → 回放（不再依赖已退役细粒度 LLM 工具） |

详见 [browser-remote-service-mode-a.md](./browser-remote-service-mode-a.md)。

---

## 3. Chrome 连接方式

| | **attach（`cdp_url` 非空）** | **launch（默认）** |
|--|---------------------------|-------------------|
| 浏览器来源 | **已有 Chrome 实例** | Agent 按 session 启动 Chrome |
| profile | 用户已有 profile | `<runtime_root>/browser/profiles/{session}` |
| stop 行为 | 仅关闭 tab，**不**关闭浏览器 | 关闭浏览器并清理 launcher |
| SSO / 内网登录 | **可复用已登录会话** | 独立 profile，需重新登录 |

---

## 4. 支持矩阵（对外声明）

### 4.1 dagents-node 核心（不变）

核心 Node 的构建与部署见 [运维与发布](../user/operations.md)；旧 OS 验收矩阵仅作历史参考。

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
| **Phase 1 / 1.5** | 伴生任务 + 模式 A | ✅ `browser_run_task*`、RemoteDriver、`browser-service`（browser-use Agent） |
| **Phase 2** | 录制 | Go CDP 事件 → `demonstration/v1`；可选 Chrome 扩展导出 |
| **Phase 3** | 回放 | `demonstration_*` + 任务/内部 CDP + Skill |
| **Phase 4** | WebUI + HTTP API | record start/stop、demo 列表 |

---

## 6. 已实现：模式 A + browser-use

### 6.1 包布局

| 路径 | 说明 |
|------|------|
| `node/internal/browser/remote_driver.go` | Go → HTTP |
| `node/internal/browser/manager.go` / `manager_task.go` | session + 任务派发 |
| `browser-service/dagents_browser/` | browser-use Agent 驱动 Chrome |
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
  max_sessions: 8
  allowed_url_schemes: [https, http]
  ignore_https_errors: false
```

### 6.3 依赖

- **Go Node**：无 rod；仅 HTTP 客户端
- **dagents-browser**：Python 3.11+、`browser-use`、本机 Chrome
- **不需要**：Playwright 自带 Chromium、Node sidecar

---

## 7. 工具清单（主 Agent）

| 工具 | 说明 |
|------|------|
| `browser_run_task` | 向伴生 session 派发自然语言任务（默认 wait=true，返回 summary） |
| `browser_task_status` | 查询任务状态 / 扁平结果（summary、urls、screenshot_paths 等） |
| `browser_task_cancel` | 取消运行中任务 |

启用 `tools.enabled_groups: [browser]` 且 `browser.enabled: true` 时，Node 为每个主 Agent 创建持久伴生 `{agent_id}-browser`；Chrome `session_key` 为伴生 id。

```text
主 Agent: browser_run_task(task="打开 example.com 并提取标题")
        → Manager.RunTask* → sidecar op=run_task
        → browser_use.Agent 内部 navigate/click/extract…
        → browser_task_status / 同步 wait 回灌 summary + 截图引用
```

细粒度 `browser_navigate` / `browser_click` / `browser_snapshot` 等 **已从 LLM 工具面与 Go Manager / HTTP op 面退役**；仅存于历史文档与 Phase 2+ 规划语境。

配置示例：

```yaml
browser:
  enabled: true
  service_url: http://127.0.0.1:18766
tools:
  enabled_groups: [browser]
```

详见 `node/internal/tools/browser_tool.go`、`browser_tool_task.go` 与 `node/internal/browser/README.md`。

---

## 8. Phase 2：录制（Demonstration Record）

### 8.1 存储

```text
{runtime_root}/demonstrations/{demo_id}/
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

`demonstration_load` → attach 同 profile → `demonstration_replay_step` / 任务级复述 → 失败 vision 对齐。

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
- `browser_run_task` 默认 HITL（种子 `always`，可按 Agent 策略调整）。  
- 内网 URL 可选 host 白名单（P2）。  
- manifest 密码字段 `redacted: true`。  
- 演示数据默认不出 `runtime_root`。

---

## 13. 验收标准

### 13.1 2026-08-25 回归结果

本轮使用真实 LLM、Node `18765` 与本机 browser sidecar 完成以下验证：

| 场景 | 结果 | 证据/结论 |
|---|---|---|
| `wait=true` 只读任务 | ✅ | example.com 成功返回标题、正文、URL、步骤摘要与截图路径 |
| `wait=false` + `task_status` | ✅ | 先返回 `queued/task_id`，后续查询返回 `completed/success=true` |
| `wait=false` 自动回灌 | ✅ | Node 轮询 sidecar 终态并通过 `async_tool_result` 自动启动回灌回合；真实 UI 收到 Example Domain |
| 排队态立即取消 | ✅ | 稳定返回 `cancelled`，不再暴露 browser-use 的 `_task_start_time` 异常 |
| 运行态取消 | ✅ | 先观察到 `running`，取消后返回 `cancelled` |
| 多 session 并发 | ✅ | example.com / example.org 使用不同 debug port、task_id 与 URL，未串线 |
| 同 session 连续任务 | ✅ | 第二个任务能基于前一任务留下的页面继续导航，结果正确 |
| 失败后恢复 | ✅ | 失败任务 `status=completed, success=false` 且保留 errors；后续任务成功 |
| HITL UI 实时审批恢复 | ✅ | `tool_waiting` 丢失 `hitl_required` 时自动 hydrate；无需刷新即可显示审批卡片 |
| HITL UI 终态收敛 | ✅ | 后端 `pending_hitl=null` 后，审批卡片不再残留 |
| 真实多模态图片输入 | ✅ | Node 使用 `mimo-v2.5` 接收上传图片，经 `read_image` 视觉 follow-up 准确识别界面、工具状态和关键数值 |
| 已归档任务查询 | ✅ | sidecar 重启后可从 `tasks/{task_id}.json` 恢复已完成任务状态；运行中的任务仍不能跨重启续接 |
| URL scheme 硬拦截 | ✅ | sidecar 导航边界拒绝 `file:` / `javascript:` 等未允许 scheme；默认只允许 `http/https` |

当前已确认的产品缺口：

1. sidecar 重启后只能恢复已归档的终态任务；重启前仍在运行的任务无法恢复执行或取消，需要后续引入 Node 侧任务索引与恢复协议。
2. 当前已对 URL scheme 做运行时硬拦截，但还没有 host/domain allowlist；生产部署仍应增加按 Agent 或 Node 配置的域名白名单。
3. 取消采用协作停止，必要时再强制取消，响应可能等待数秒；UI 已展示 `正在取消`，但 sidecar 仍可进一步提供更细的取消进度。
4. browser sidecar 是独立进程；Node 的 LLM profile 变更不会替已运行的 sidecar 热切换模型，切换 browser 视觉模型后需要重启/重载 sidecar 配置。

**Phase 1 / 1.5（伴生 + CDP）**

1. Win Server **2016/2019** 内网 Chrome 上：`browser_run_task` 可完成或失败可读；归档 / 截图引用可见。  
2. **2012 R2** 在内网 Chrome 验收矩阵内通过同等 smoke。  
3. 无 Node.js 进程；`go test` 绿；sidecar 仅接受任务级 `op`。  
4. SSO 场景：attach 已登录 Chrome（`cdp_url`）可执行登录后任务（POC）。

**Phase 2–3**：同初稿，engine=`cdp-chrome`。

---

## 14. 后续扩展

- `browser_connect`：显式 attach 用户已打开的 Chrome（仅 CDP URL）。  
- Demonstration 录制 / 回放（Phase 2–4）。  
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
| 2026-08 | **伴生任务模型**：主 Agent 仅 `browser_run_task*`；退役细粒度 LLM 工具与 Manager/HTTP op |
