# Windows Desktop Shell — 功能清单与架构决策

**状态（2026-07）**：规划中（`desktop/tray/` 为最小范例，尚未接入安装包）。  
**范围**：**仅 Windows**（不含 Linux/macOS 桌面壳）。  
**读者**：产品 / 实现 / 安装包；与 [local-assistant.md](../architecture/local-assistant.md)、[client-packaging.md](../architecture/client-packaging.md) 配套。

---

## 1. 背景与问题

当前仓库已有：

| 组件 | 路径 | 职责 |
|------|------|------|
| Agent Node | `node/` | 运行时、HITL、SSE、`/ui/` 静态资源 |
| Web UI | `node/webui/` | 复杂人机交互（Vue 3） |
| 终端 Client | `client/`、`app/cli/` | TUI / REPL，订阅 SSE |
| 托盘范例 | `desktop/tray/` | 启停 Node、health 探活 |

**缺口 A（Shell）**：用户 **未打开 Web UI** 时，没有常驻组件接收 Node 的 **HITL** 等事件，也无法提供 **系统通知**、**文件路径粘贴** 等 Windows 集成。

**缺口 B（全 Client，阻塞 Shell 闭环）**：当前 **Web UI / Go TUI / Python TUI** 均 **不能恢复会话历史与未完成 HITL**：

| 现象 | 原因（现状） |
|------|----------------|
| 切换 session / 刷新页面后主聊天区空白 | Web UI `switchSession` 会 `clearTranscript()`；仅依赖 **`live=1` SSE** 增量，不做历史回放 |
| 从 Shell 通知点进 UI 看不到待审批 | `clearHitl()` + 无 API 还原 `PendingHITL`；HITL 仅在有 **`hitl_required` SSE 实时事件** 时入队 |
| `/context` 面板能看到最近 10 条摘要 | `GET /v1/sessions/{id}/context` 的 `recent_messages` **未** 灌入主 transcript；且无 HITL 展示载荷 |
| Node 已持久化 messages + pending | SQLite `RuntimeState.Pending` 在 Node 重启后可恢复 **turn**，但 **Client UI 未 hydrate** |

因此：**Session 历史 + Pending HITL 恢复** 是跨端能力，**Web UI 为 P0**（Windows 默认 Client）；Shell 通知 → 打开 UI 的处理链 **依赖** 该能力。

**目标**：增加 **Desktop Shell** + 补齐 **Client Hydrate**；Windows 上 **Shell + Web UI** 为默认 Client。

---

## 2. 已达成的重要决策

以下决策已对齐，**后续实现默认遵循**，变更需更新本文并注明原因。

### 2.1 平台与进程

| # | 决策 | 说明 |
|---|------|------|
| D1 | **仅 Windows** | 桌面壳不对 Linux/macOS 做同等规划。 |
| D2 | **Shell 与 Node 分离进程，Shell 管 Node 生命周期** | `dagents-node.exe` 管 turn/工具/持久化；**Shell 是桌面侧 supervisor**（启停、自启、退出收尾）。 |
| D29 | **退出 Shell 即停止 Node** | 用户退出 Shell（托盘「退出」/ 注销）时 **graceful stop Node**（`nodectl.Stop`）；**不再**保留「退 Shell 留 Node」。 |
| D30 | **Shell 常驻，Node 随 Shell 会话** | 桌面产品路径下：**Shell 生命周期 ⊇ Node**；Node 在 Shell 自启后拉起，随 Shell 退出而停。Web UI 仍短于二者。 |
| D3 | **Shell 跑在用户交互会话** | 托盘、Toast 须在用户桌面会话；**不由 Node 直接弹通知**。 |
| D4 | **单实例 Shell** | 二次启动（协议、CLI）转发到已运行实例。 |
| D20 | **单实例 Node** | 全机（或安装根下）仅一个 `dagents-node`；**仅 Shell 负责拉起/停止**，不重复拉起。 |
| D21 | **开机自启 Shell → 自动起 Node** | Shell 随用户登录自启；启动后 **立即 ensure Node**（非「按需才启」）。长期任务（trigger、inbox 等）由 **Node 执行、Shell 监护与通知**。 |

### 2.2 架构分层

| # | 决策 | 说明 |
|---|------|------|
| D5 | **`desktop/tray` 演进为 Shell** | 扩展为 `dagents-shell.exe`（目录可 `desktop/shell/`）。 |
| D6 | **Node 为唯一运行时真相源** | Turn、HITL、SSE、持久化均在 Node；Shell **不复制** turn 状态机。 |
| D7 | **复杂 UI 以 Web UI 为主** | 聊天、完整审批、A2A relay 等 **不在 Shell 内重做**。 |
| D8 | **UI 未打开时的媒介 = Shell** | Shell 常驻订阅 Node 事件 → Toast → 点击打开对应 session 的 UI。 |
| D22 | **Windows 默认 Client = Shell + Web UI** | 文档与安装包默认推荐 Shell + 浏览器 `/ui/`；TUI 降为 SSH/脚本兜底。 |

### 2.3 通知与 HITL（产品确认 2026-07）

| # | 决策 | 说明 |
|---|------|------|
| D17 | **每 session 一条 HITL 通知** | 同一 session 内多个 HITL 项（`hitl_required` 多 item、或连续事件）**合并为一条 Toast**；不逐条刷屏。 |
| D18 | **点击通知 → 打开该 session 的 UI** | 深链携带 `session_id`；hydrate 恢复 transcript + pending HITL |
| D23 | **Shell 仅通知，不在 Toast 内审批** | Phase 1 不做 Toast 批准/拒绝按钮；复杂 HITL 一律进 Web UI。 |
| D39 | **有待办时托盘图标须有明显态** | 任意 session 存在待办（HITL 或 **未读回复**，见 D40）时，托盘图标切换为 **特殊效果**（角标/高亮/备用 icon 等，实现待定）；与菜单「待办」、Toast 一致。 |
| D40 | **待办含「未读 assistant 回复」** | Node 当轮 **正常结束**（`done` + `finish_reason: stop` 等，非 HITL 暂停）且末条 assistant **尚未被 Client 消费** 时，该 session **也算一条待办**；与 HITL 共用 session 聚合表（F-E3）。 |

### 2.4 文件与输入（产品确认 2026-07）

| # | 决策 | 说明 |
|---|------|------|
| D19 | **复制/拖入文件 → 输入框路径** | 用户在 Web UI **消息输入框**粘贴或拖入文件时，转为 **绝对路径字符串** 写入输入框（非「双击文件关联打开 App」）。 |
| — | **浏览器限制** | 纯 Web 无法从剪贴板读 Windows `CF_HDROP` 完整路径；需 Shell 提供 **localhost 辅助 API** 或由 Shell 写入剪贴板为路径文本（实现见 §8、F-P*）。 |

### 2.5 技术取向

| # | 决策 | 说明 |
|---|------|------|
| D9 | **Shell 实现语言：Go** | 与 `nodectl`、`shared/config` 同栈。 |
| D10 | **不用悬浮球作为默认方案** | 托盘 + 通知 + 按需打开 UI。 |
| D11 | **Shell 核心不依赖 WebView2** | Toast、SSE、托盘用 Go + Win32；Web UI 仍用系统浏览器。 |
| D12 | **Server 2012 仍纳入支持目标** | 浏览器 + Go Shell；不绑 WebView2。 |
| D13 | **HITL 展开逻辑应共享** | Shell / Web / TUI 共用 expand 逻辑（包路径待定）。 |

### 2.6 数据流（产品确认 + 架构评审 2026-07）

| # | 决策 | 说明 |
|---|------|------|
| D24 | **混合数据流（推荐，见 §8）** | **事件/通知**：`Node → Shell`（必须）。**UI 运行时 SSE/API**：`Node → Web UI` 直连（推荐）。**桌面能力**（路径粘贴等）：`Web UI → Shell` localhost 辅助 API。 |
| — | **不全量改为 Node→Shell→UI 代理** | 避免 Shell 成为全部 HTTP/SSE 网关（复杂度高、Dev 链路过长、故障面扩大）；若未来要强统一再评估 Shell 反向代理。 |

### 2.7 与现有 API 的衔接

| # | 决策 | 说明 |
|---|------|------|
| D14 | **Shell 事件通道：SSE** | `GET /v1/streams?live=1`（全局订阅），Shell 内按 session 聚合。 |
| D15 | **断线恢复：轮询兜底** | SSE 重连 + `GET /v1/sessions`（`run_turn_phase`）。 |
| D16 | **HITL 续跑** | Web UI 经 Node `POST /v1/messages`（`resume`）；Shell 不代提交（Phase 1）。 |

### 2.8 Session 历史与 Pending HITL 恢复（产品确认 2026-07）

| # | 决策 | 说明 |
|---|------|------|
| D25 | **全 Client 缺口，Web UI 先补** | 历史消息 + 未完成审批的 **展示恢复** 为平台能力；TUI 可后续对齐；**Shell 通知闭环依赖 Web UI hydrate**。 |
| D26 | **Hydrate 走 Node API，非 Shell 缓存** | 打开/切换 session、刷新页面、深链进入时，Client 向 Node 拉快照；Shell **不**存 transcript。 |
| D27 | **Pending HITL 以 Node 真相为准** | 从 Node 内存/`PendingHITL` 持久化状态生成与 `hitl_required` **同构** 的载荷，供 Client `expandHitlRequired` 入队。 |
| D28 | **历史 transcript 从 messages 映射** | 不能仅靠 SSE hub 回放（缓冲上限 ~256 条事件且为 SSE 形态）；需 API 提供 **可渲染 transcript** 或足够完整的 `messages[]` + Client 映射规则。 |

### 2.9 不活跃 Session 统一维护：压缩 + 卸内存（产品确认 2026-07）

| # | 决策 | 说明 |
|---|------|------|
| D31 | **压缩与 evict 绑定为同一扫描器** | 扩展现有 **`idle_auto_compress` 扫描循环**（或重命名为 **idle session maintenance**）：一次 tick 内对 eligible session 顺序执行 **可选压缩 → persist → Release 卸内存**；**不**再开第二个 poll。 |
| D32 | **evict 前须 persist** | 卸内存前 `runtime.persist()`；messages + `PendingHITL` 落盘；evict 后 **`POST /v1/sessions` + hydrate** 恢复。 |
| D33 | **忙碌 session 跳过整轮** | turn 进行中 / 队列非空 / 非 idle：**不压缩、不 evict**；**仅有 pending HITL 且 idle** 的 session **可** evict（待办在 DB）。 |
| D34 | **统一 idle 阈值** | 默认 **共用** `compression.idle_auto_compress_seconds`（及 `poll_seconds`）作为「无动作」判定；`min_tokens` 仍 **仅** 约束是否执行压缩，**不** 阻止 evict。 |
| D35 | **DB-only session 不再压完常驻内存** | 现状 `tryPersistedIdleAutoCompress` 会 `ensureRuntime` 后 **留在内存**；合并后：**临时加载 → 压缩（若满足 min_tokens 且未标记）→ Release**，避免「只为压缩而泄漏内存」。 |

### 2.10 版本检查与升级：Shell 为安装态 orchestrator（产品确认 2026-07）

| # | 决策 | 说明 |
|---|------|------|
| D36 | **Windows 桌面：Shell 负责安装态更新** | **查版本、提醒用户、停 Node、换文件、再起 Node** 由 Shell orchestrate；Node **不再**作为 Release Hub 的 poll 主体（`UpdateChecker` / `GET /v1/agent/update` 在 Windows+Shell 路径 **逐步下线**）。 |
| D37 | **Apply 前须 Node 空闲** | Shell 执行 apply 前向 Node 查询 **无 active turn / 无队列**；忙碌时 **仅通知、不静默升级**（与 [release-update-hub.md](./release-update-hub.md)「非静默升级」一致）。 |
| D38 | **共享 update 逻辑** | Manage check URL、manifest 解析、下载校验等抽到 **`shared/update`**（或 `desktop/shell/internal/update` 复用 Node 现有实现），避免 Shell/Node 双份维护。 |

### 2.11 明确不做（当前阶段）

| # | 非目标 |
|---|--------|
| N1 | Shell 内完整聊天 Client |
| N2 | Electron/Wails 常驻窗口替代浏览器 |
| N3 | Linux/macOS 同等 Shell |
| N4 | Explorer 双击文件扩展名关联（非当前需求） |
| N5 | Server Core 上的 GUI Shell |
| N6 | Toast 内一键批准/拒绝（Phase 1） |
| N7 | Phase 1 一次性还原与 SSE 完全一致的 streaming 动画（hydrate 为静态快照即可） |

---

## 3. 功能清单

**图例**：`P0` 首版必须 · `P1` 紧随其后 · `P2` 增强

### 3.1 核心 — Node 生命周期

| ID | 优先级 | 功能 | 现状 | 备注 |
|----|--------|------|------|------|
| F-L1 | P0 | 启动 Node（后台、无控制台） | ✅ `nodectl.Start` | |
| F-L2 | P0 | 停止 Node | ✅ `nodectl.Stop` | |
| F-L3 | P0 | 重启 Node | ✅ `nodectl.Restart` | |
| F-L4 | P0 | health 探活 | ✅ 3s `Probe` | |
| F-L5 | P0 | 托盘启停与状态 | ✅ 基础 | 待办数 ✅；**图标特殊效果** → F-N10 |
| F-L8 | P0 | **Node 单实例**：已运行时不再拉起 | ❌ | D20；Mutex / pid+health |
| F-L9 | P0 | Shell 启动时 **立即 ensure Node running** | ❌ | D21 |
| F-L10 | P0 | **退出 Shell 时 stop Node**（graceful，超时可强制） | ❌ | D29；替代现 tray「不停止 Node」 |
| F-L11 | P1 | 退出前确认：「将停止 Agent 与后台任务」 | ❌ | 可选二次确认 |
| F-L12 | P0 | **Shell 单实例**：全局 Mutex + 二次启动 **转发**（打开 UI / focus session） | ❌ | D4 |
| F-L13 | P0 | **Node 异常退出监护**：health 连续失败 → 托盘告警 + **自动重启**（可配置次数/间隔） | ❌ | D2/D30；supervisor 职责 |
| F-L15 | P0 | **`desktop/tray` → `dagents-shell.exe`** | ❌ | D5；目录可 `desktop/shell/` 或保留目录仅改二进制名 |
| F-L6 | P1 | 与 `dagents.cmd node` PID 回退对齐 | ❌ | |
| F-L7 | P2 | Windows 服务安装 Node | ❌ | 非当前路径；Shell 自启 + 拉起 Node |

### 3.2 核心 — 事件订阅与 session 级聚合

| ID | 优先级 | 功能 | 现状 | 备注 |
|----|--------|------|------|------|
| F-E1 | P0 | 常驻 SSE（`live=1`，全局） | ❌ | |
| F-E2 | P0 | 解析 HITL 事件并入 **session 待办表** | ❌ | D17 |
| F-E3 | P0 | 同 session 多条 HITL **合并为一条通知态** | ❌ | 更新计数/摘要，不重复 Toast |
| F-E4 | P0 | SSE 断线重连 | ❌ | |
| F-E5 | P1 | `GET /v1/sessions` 轮询兜底 | ❌ | |
| F-E9 | P1 | UI 打开某 session 时 Shell **抑制该 session 新 Toast** | ✅ | `POST /v1/desktop/ui/focus` + TTL 心跳 |
| F-E10 | P0 | **待办消除**：该 session 在 Node 侧无 pending HITL 时清除 Shell 状态 | ❌ | 见 §8.5；**不依赖**从通知打开 UI |
| F-E11 | P0 | 订阅 **A2A relay** 事件：`approval_required` / `user_information_required` | ❌ | §8.5；与 `hitl_required` 一并入 session 待办表 |
| F-E12 | P0 | Shell 访问 Node **SSE/REST 鉴权**（API key header，与 Web UI 对齐） | ❌ | 共用 `config.yaml` / `DAGENTS_HOME` |
| F-E13 | P0 | **未读回复待办（IM cursor）**：Node 持久化 `notify_seq`/`ack_seq`；`has_unread = notify_seq > ack_seq`；Client `POST /ack` 消费 | ✅ | D40；§8.5.1；Shell 仅同步 Node，不本地推断 |
| F-E7 | P2 | 可选订阅 `error` / 子 Agent 事件 | ❌ | |

### 3.3 核心 — 通知

| ID | 优先级 | 功能 | 现状 | 备注 |
|----|--------|------|------|------|
| F-N1 | P0 | 每 session 一条 Toast（有待办 HITL） | ❌ | D17 |
| F-N2 | P0 | 点击 Toast → 打开该 session 的 `/ui/` | ❌ | D18 |
| F-N3 | P0 | 托盘：待办 session 数 / 列表入口 | ❌ | |
| F-N10 | P0 | **有待办时托盘图标特殊效果**（角标/高亮/备用 icon 等） | ❌ | D39；与 F-N3 待办表联动 |
| F-N4 | P1 | `dagents://session/<id>` 协议与 Toast 激活 | ❌ | |
| F-N8 | P1 | 通知文案：session 摘要（如「3 项待处理」） | ❌ | 同 session 合并 |
| F-N9 | P1 | **新版本 Toast / 托盘入口**（Manage 有 upgrade 时） | ✅ | D36；非静默，点击进升级确认 |
| F-N6 | P2 | tooltip 待办摘要 | ❌ | |

### 3.4 核心 — 打开 Web UI

| ID | 优先级 | 功能 | 现状 | 备注 |
|----|--------|------|------|------|
| F-U1 | P0 | 托盘「打开控制台」→ 浏览器 `/ui/` | ❌ | |
| F-U2 | P0 | Node 未运行时先 ensure 再打开 | ❌ | |
| F-U3 | P0 | Web UI URL：`?session=<id>` | ❌ | F-X1 |
| F-U4 | P1 | 打开后通知 Shell「已聚焦 session」 | ✅ | `POST /v1/desktop/ui/focus`（F-X5） |
| F-U5 | P1 | Shell **轮询 Manage**（`manage.update` 配置）缓存 `UpdateStatus` | ✅ | D36/D38；**不依赖 Node 在跑** |
| F-U6 | P1 | Shell **执行 apply**：确认 → stop Node → 下载/解压/覆盖 `bin/*` → start Node | ✅ | D37；复用现 `dagents.cmd update` 文件布局逻辑 |

### 3.5 文件路径与输入框（非扩展名关联）

| ID | 优先级 | 功能 | 现状 | 备注 |
|----|--------|------|------|------|
| F-P1 | P1 | Web UI：paste/drop 文件时插入 **绝对路径** 到输入框 | ✅ | D19；`MainChatPanel` Composer |
| F-P2 | P1 | Shell：`GET /v1/desktop/clipboard/files`（或等价）返回路径列表 | ✅ | 读 CF_HDROP；仅 localhost |
| F-P3 | P1 | Web UI paste 时若 `clipboardData.files` 无路径则 **调 Shell API** | ✅ | |
| F-P4 | P2 | 拖入多个文件 → 多行路径或约定分隔符 | ❌ | 产品可再定格式 |
| F-P5 | P2 | 路径是否在 `fs_root` 内校验提示 | ❌ | 可选，Node 侧仍会拦 |

### 3.6 安装与运维

| ID | 优先级 | 功能 | 现状 | 备注 |
|----|--------|------|------|------|
| F-I1 | P0 | `bin/dagents-shell.exe` + `dagents-node.exe` 布局 | ✅ | assemble + Inno `bin/*` |
| F-I3 | P0 | **当前用户开机自启 Shell** | ✅ | HKCU Run + `dagents shell --background` |
| F-I2 | P1 | Inno Setup 组件 | ❌ | |
| F-I4 | P1 | 共用 `-config` / `DAGENTS_HOME` | ✅ | |
| F-I5 | P2 | `.runtime/logs/shell.log` | ❌ | |
| F-I7 | P1 | 文档：Windows 默认 Client 改为 Shell + Web UI | ❌ | D22 |
| F-I8 | P0 | **CI / Release workflow** 构建 `dagents-shell.exe` | ✅ | `build_dagents_shell.sh` + assemble 必含 |
| F-I9 | P0 | **`dagents.cmd shell`** 子命令（start / status / stop） | ✅ | `--background` / `--foreground` |
| F-I10 | P0 | **卸载 / 升级** 时清理 Shell **开机自启**注册表项 | ✅ | Inno `RemoveShellAutostart` |
| F-I11 | P1 | **`shared/update`**（或等价）抽取 Manage check + 下载校验 | ✅ | D38 |
| F-I12 | P1 | **`dagents.cmd update`** 委托 Shell（Shell 未运行时回退现 client 路径） | ✅ | 与 F-U6 同一 orchestrator |

### 3.7 Web UI / Node 配合项

| ID | 优先级 | 功能 | 负责层 | 备注 |
|----|--------|------|--------|------|
| F-X1 | P0 | URL 深链 + 聚焦 HITL | Web UI | 依赖 F-H8 |
| F-X6 | P0 | **Session hydrate 编排**：`ensureSession` 后拉快照并恢复 UI | Web UI | 见 §3.9 |
| F-X4 | P1 | 输入框 paste/drop 文件 → 路径 | ✅ | Web UI + Shell；F-P* |
| F-X5 | P1 | 加载时 `POST` Shell「ui.focus」`（session_id） | ✅ | hydrate + 30s 心跳 |
| F-X8 | P1 | Web UI **Update 面板**改读 Shell `GET /v1/desktop/update`（非 Node `/v1/agent/update`） | ✅ | localhost；Apply 经 Shell 确认 |
| F-X3 | P2 | `ui.enabled: false` 时 Shell 提示 | Shell | |

### 3.9 Session 历史与 Pending HITL 恢复（跨端，Web UI P0）

**现状依据**：`node/webui/frontend/src/App.vue` 的 `switchSession` 会 `clearTranscript()` + `clearHitl()`；`GET /context` 仅有 `recent_messages`（10 条、截断）与 `pending_tool_calls_count`（**无** HITL 展示载荷）。

#### Node API

| ID | 优先级 | 功能 | 现状 | 备注 |
|----|--------|------|------|------|
| F-H1 | P0 | **`GET /v1/sessions/{id}/pending-hitl`**（或并入 hydrate） | ❌ | 返回与 `hitl_required` 同构的 `{ items[], hitl_id, … }`；由 `PendingHITL` + `buildHITLRequiredPayload` 生成 |
| F-H2 | P0 | **Transcript 快照 API** | ❌ | **(a)** `GET …/transcript` 返回可渲染 entries；**(b)** 扩展 `/context` 全量 `messages` + 映射规则 |
| F-H14 | P0 | Node 侧 **`MessagesToTranscriptEntries`**（或等价）组装 hydrate `transcript` | ❌ | F-H2 实现子项；映射规则与 Web UI SSE 渲染对齐 |
| F-H3 | P1 | Hydrate 响应含 `run_turn_phase` / `has_active_turn` | ⚠️ 部分已有 | `/context` 已有字段 |
| F-H4 | P1 | 子 Agent / A2A relay pending 的 hydrate | ❌ | 与 relay 字段对齐 |
| F-H5 | P2 | TUI hydrate（Go full TUI） | ❌ | D25 后续 |
| F-H6 | P2 | Python TUI hydrate | ❌ | 后续 |

#### Web UI

| ID | 优先级 | 功能 | 现状 | 备注 |
|----|--------|------|------|------|
| F-H7 | P0 | 进入 session（加载 / 切换 / 深链）时 **hydrate transcript** | ❌ | 替换「只清不灌」 |
| F-H8 | P0 | hydrate 后若有 pending → **`expandHitlRequired` 入队** 并聚焦 | ❌ | 通知点进 UI 必达 |
| F-H9 | P0 | hydrate 与 **`live=1` SSE** 衔接（seq fence / 去重） | ❌ | 避免重复条目 |
| F-H10 | P1 | 浏览器刷新（F5）后自动 hydrate | ❌ | 同 F-H7 |
| F-H11 | P1 | Context 面板与主 transcript 分工一致 | ❌ | 避免两套真相 |

#### 与 Shell 的衔接

| ID | 优先级 | 功能 | 备注 |
|----|--------|------|------|
| F-H12 | P0 | Shell 深链 `?session=` 触发 hydrate（F-H7–H8） | 通知闭环 |
| F-H17 | P0 | **evicted session 端到端**：`ensureSession`（`POST /v1/sessions` 带 id）→ hydrate → 可 resume | 与 F-NM7 联调；`active=false` 时必走 Create |
| F-H13 | P1 | hydrate 完成后可选上报 Shell `ui.focus` | ✅ | `hydrateSession` → F-X5 |

#### 建议 API 形态（草案）

```http
GET /v1/sessions/{session_id}/hydrate
```

```json
{
  "session_id": "…",
  "run_turn_phase": "awaiting_hitl",
  "transcript": [ { "kind": "assistant", "text": "…" } ],
  "pending_hitl": { "hitl_id": "…", "items": [ ] },
  "sse_seq_hint": 42
}
```

- `pending_hitl` 为空表示无待办；非空则 Web UI 直接 `expandHitlRequired`。
- `transcript` 为 **静态快照**（不要求 streaming 态）；与 SSE 增量去重见 F-H9。

#### 与 Hub 256 / evict 的关系

- **Hydrate 读 SQLite/最后一次 persist**，不依赖 SSE hub 环形缓冲（见 §8.6）。
- Session **被 evict 后**不在内存：`GET /hydrate` / `GetContextView` 仍可从 **DB** 组装；Client 访问前 **`POST /v1/sessions`（带 id）** 重新 `Create` 激活 consumer（Web UI `ensureSession` 已如此）。

### 3.10 Node — 不活跃 Session 统一维护（D31–D35）

**现状（2026-07）**：`scanIdleSessionMaintenance` 在同一循环内 **压缩 + Release**；DB-only 路径临时 `ensureRuntime` 后 **必 Release**（D35）。

```text
scanIdleSessionMaintenance()   // 原 scanIdleAutoCompress，已扩展
  对每个 eligible session（updated_at 超阈值 + idle）:
    1. 若在 DB-only → ensureRuntime（临时加载）
    2. 若满足 min_tokens 且未 idle_auto_compress_applied → ForceBlocking 压缩 → 打标
    3. persist()
    4. Release()  // stop consumer，移出 m.sessions，保留 SQLite
```

| ID | 优先级 | 功能 | 现状 | 备注 |
|----|--------|------|------|------|
| F-NM1 | P1 | `Manager.Release(sessionID)` | ✅ | persist → stop → 移出 map；不 `store.Delete` |
| F-NM2 | P1 | 合并进 `scanIdleSessionMaintenance`（原 idle compress loop） | ✅ | D31；**不单开** evict scanner |
| F-NM3 | P1 | 压缩步：复用 `tryRuntimeIdleAutoCompress` 逻辑 | ✅ | 无 pending、非 child |
| F-NM4 | P1 | evict 步：凡通过 idle 判定 **且非 busy** 均 Release | ✅ | 含「已压缩跳过」与「token 不足仅 evict」 |
| F-NM5 | P1 | DB-only 路径：压缩后 **必须** Release（D35） | ✅ | 修现 `ensureRuntime` 泄漏 |
| F-NM6 | P2 | 配置：沿用 `compression.idle_auto_compress_*`；文档说明 **含 evict** | ⚠️ | 可选 rename 为 `idle_session_*` |
| F-NM7 | P1 | evict 后 `EnqueueMessage` 须先 `Create` | ✅ | Web UI `ensureSession` + hydrate |
| F-NM8 | P2 | log/metric：`compressed` / `evicted` / `skipped_busy` | ❌ | |

**eligible 摘要**（与现 `eligibleForIdleAutoCompress` 对齐并扩展）：

| 条件 | 压缩 | evict |
|------|------|-------|
| `updated_at` 早于阈值 | 前提 | 前提 |
| busy（非 idle / 队列 / 在途 turn） | ❌ | ❌ |
| `pending HITL` | ❌ | ✅（persist 后 evict，不压缩） |
| 已 `idle_auto_compress_applied` | 跳过 | ✅ 仍 evict |
| token &lt; min_tokens | 跳过 | ✅ 仍 evict |
| child session | ❌ | ❌ |

**与 Shell / hydrate**：evict 后会话仅在 SQLite；Shell 已收过的 HITL 通知不受影响；UI **`ensureSession` + `/hydrate`** 恢复。

**实现落点**：`node/internal/session/idle_auto_compress.go` → 重命名/扩展为 `idle_session_maintenance.go`（或同文件演进）。

### 3.11 版本检查与升级（Release Hub · D36–D38）

**现状**：[`release-update-hub.md`](./release-update-hub.md) 规定 Client 经 **`GET /v1/agent/update`**（Node `UpdateChecker` poll Manage）；Windows `dagents.cmd update` 经 client 下载后 **shutdown_node → 覆盖 bin → restart_node**。

**目标（Windows + Shell）**：安装态更新 **与 Node 运行时解耦**；Shell 为 orchestrator，Node 仅回答 **能否现在升**。

```text
Manage /v1/releases/check
    ↑ poll（manage.update 配置 + agent token）
Shell UpdateChecker（F-U5）
    ├── 托盘 / Toast（F-N9）
    ├── GET localhost /v1/desktop/update（F-X8）
    └── apply（F-U6）：问 Node 空闲 → stop → 下载/解压/覆盖 → start

Node（运行时）
    ├── GET /v1/agent/upgrade-readiness（或 health 扩展）→ busy / idle
    └── GET /v1/agent/update → Windows+Shell：**deprecated**，返回 delegate 提示（F-ND2）
```

| ID | 优先级 | 功能 | 现状 | 备注 |
|----|--------|------|------|------|
| F-U5 | P1 | Shell poll Manage + 缓存 `UpdateStatus` | ✅ | 见 §3.4；可读 `VERSION` + 各 bin 版本 |
| F-U6 | P1 | Shell apply orchestration | ✅ | 见 §3.4；D37 |
| F-N9 | P1 | 新版本 Toast / 托盘菜单 | ✅ | 见 §3.3 |
| F-I11 | P1 | 共享 update 库（check + download + sha256） | ✅ | D38；自 `node/internal/manage/update_checker.go` 等抽取 |
| F-I12 | P1 | `dagents.cmd update` → Shell | ✅ | Shell absent 时回退 `dagents-client update` |
| F-X8 | P1 | Web UI UpdatePanel → Shell localhost API | ✅ | 见 §3.7 |
| F-ND1 | P1 | Node **`GET /v1/agent/upgrade-readiness`**：`has_active_turn` / 可升级 | ✅ | Apply 前 Shell 必查；无 turn 才允许 F-U6 |
| F-ND2 | P2 | Windows+Shell：`/v1/agent/update` **deprecated** | ⚠️ 现由 Node 提供 | 返回 `{"delegate":"shell",…}` 或文档说明；Linux/headless **保留** Node 路径 |

**与 v0.6.0**：§4.1 中 **架构决策 D36–D38 写入**；**实现**（F-U5/U6、F-ND*、F-X8）标 **v0.6.x**；v0.6.0 可先让 **`dagents update` 的 stop/start 走 Shell**（与 F-L10 一致），Node `UpdateChecker` **暂保留**。

**详见**：[release-update-hub.md §10](./release-update-hub.md#10-windows-shell-路径例外)。

### 3.8 降级 / 可选（原「系统入口」，非 P0）

| ID | 优先级 | 功能 | 说明 |
|----|--------|------|------|
| F-S4 | P2 | `dagents://` 协议 | 与 F-N4 合并 |
| F-S5 | P3 | 拖文件到托盘 | 非 D19 主路径 |
| F-S7 | P3 | Explorer COM 右键 | 远期 |

---

## 4. 建议分期

| 阶段 | 目标 | 功能 ID |
|------|------|---------|
| **Phase 1** | 通知闭环 + **Shell 监护 Node** + 单/双实例 + 自启 + 可安装 | F-L1–5, **8–10,12–13,15**,9, F-E1–E4, **E10–E13**, **F-N1–3, N10**, F-U1–3, F-X1, F-X6, **F-H1–H2, H7–H9, H12, H14, H17**, F-I1, **I3, I8–I10** |
| **Phase 2** | 路径粘贴 + hydrate 完善 + **idle 维护（压+卸）** + 安装包 + **Shell 更新 orchestrator** | F-P1–3, F-E9, F-U4, **F-U5–U6**, F-X4–5, **F-X8**, F-N4, F-N8, **F-N9**, F-H10–H11, F-H13, **F-NM1–NM5, NM7**, **F-I11–I12, F-ND1**, F-I2, F-I7, F-L11 |
| **Phase 3** | 体验、TUI hydrate、运维、Node update 下线 | F-E5, F-E7, F-P4–5, F-N6, F-H5–H6, F-I5, **F-ND2**, F-S*, F-L6 |

### 4.1 v0.6.0 发布范围

> **完整 v0.6.0 – v0.7.0 开发路径**（含 `show_image`、Shell 自更新、TUI hydrate）见 **[v0.6-v0.7-roadmap.md](./v0.6-v0.7-roadmap.md)**。

**版本定位**：在 Windows 上交付 **默认 Client = Shell + Web UI** 的首个完整闭环；Git tag **`v0.6.0`**。

**一句话**：自启 **Shell** 监护 **单实例 Node**，UI 未打开时 **每 session 一条 Toast** 提醒 HITL；点击或手工打开 Web UI 均可经 **Hydrate** 恢复历史与待办；长期不活跃 session 经 **idle 维护（压缩 + 卸内存）** 降占用；安装包默认含 **Shell + Node + Web UI**。

#### 必达（Release blockers）

| 主题 | 功能 ID |
|------|---------|
| Shell 演进与生命周期 | F-L1–5, **F-L8–L10, L12–L13, L15** |
| 事件与待办 | F-E1–E4, **F-E10–E13** |
| 通知与打开 UI | **F-N1–N3, N10**, F-U1–U3 |
| Client Hydrate | **F-H1–H2, H7–H9, H12, H14, H17**, F-X1, F-X6 |
| 不活跃 Session 维护 | **F-NM1–NM5, NM7** |
| 安装与发布 | F-I1, F-I3, **F-I8–I10**；**F-I2**（Inno 含 Shell 组件） |

#### 建议纳入但可标为 P1（不挡 tag，挡「默认推荐安装路径」）

| 功能 ID | 说明 |
|---------|------|
| F-I7 | handbook / README：Windows 默认 Client 改为 Shell + Web UI |
| F-H10 | 浏览器 F5 后自动 hydrate（与 F-H7 同逻辑，验收单独列） |
| F-L11 | 退出 Shell 二次确认（产品可选） |

#### 明确不在 v0.6.0

| 功能 ID | 推迟至 |
|---------|--------|
| F-P1–P5 | 0.6.x / 0.7（路径粘贴 + Shell localhost API） |
| F-E9, F-X5, F-H13 | 0.6.x（UI focus 抑制 Toast；§8.5 非必须） |
| F-N4, F-N8 | 0.6.x（`dagents://`、通知文案增强） |
| F-H4–H6 | 0.7+（A2A relay hydrate 完善、TUI hydrate） |
| F-L6–L7, F-I5 | 远期 |
| **F-U5–U6, F-N9, F-I11–I12, F-X8, F-ND1–ND2** | **v0.6.2**（Shell 更新 orchestrator） |
| **F-M0–M8**（`show_image` + Media API） | **v0.6.1 / v0.7.0**（见 [node-ui-media-display.md](./node-ui-media-display.md)、[roadmap](./v0.6-v0.7-roadmap.md)） |

#### 实现顺序（建议）

```text
① F-L15/L8/I8          Shell 二进制 + Node 单实例 + CI
② F-L9–L10/L12–L13     监护生命周期 + Shell 单实例 + crash 恢复
③ F-H1/H2/H14          Hydrate API + transcript 映射
④ F-H7–H9/H17/X6       Web UI hydrate + evicted 路径
⑤ F-E1–E4/E10–E13     Shell SSE、待办表（含未读回复）、鉴权
⑥ F-N1–N3/N10/U1–U3  Toast + 托盘图标态 + 深链
⑦ F-NM1–NM5/NM7        idle 维护（压 + 卸）
⑧ F-I1–I3/I2/I9–I10    安装包 + 自启 + cmd + 卸载清理
```

#### 验收要点（Smoke）

完整检查表：[`v0.6.0-smoke-checklist.md`](./v0.6.0-smoke-checklist.md)

1. 安装后登录 → Shell 自启 → Node health 正常。  
2. Web UI 关闭时触发 HITL → **一条** Toast → 点击 → 深链 session → **transcript + pending** 可见并可 resume。  
3. Web UI 关闭时 Agent **正常回复完成**（末条 assistant 未读）→ Shell 待办 + **托盘图标特殊效果**（F-E13 / F-N10）。  
4. 手工打开 `/ui/` 处理 HITL 或 **阅读回复** → Shell 待办 **自动消除**（F-E10 / F-E13）。  
5. 退出 Shell → Node **停止**；二次启动 Shell → 仅 **一个** Shell 实例（F-L12）。  
6. 长期 idle session → 压缩（若满足）→ **卸内存**；再打开该 session → Create + hydrate 正常（F-NM*, F-H17）。

---

## 5. 待定方案（实现细节）

| 主题 | 决定 |
|------|------|
| 目录/二进制名 | 倾向 `desktop/shell`、`dagents-shell.exe` — **待定** |
| 共享 HITL 包 | `shared/hitl` vs 导出 `client/internal/hitl` — **待定** |
| Toast 库 | go-toast / WinRT — **待定** |
| **托盘 icon 待办态** | 角标 overlay / 双色 icon / 轻动画 — **待定**（F-N10） |
| **未读回复判定** | **已决**：Node `runtime_state_json` 存 `notify_seq`/`ack_seq`；`has_unread = notify_seq > ack_seq`（F-E13 / §8.5.1） |
| Shell↔UI localhost 端口 | 固定端口 vs 命名管道 vs 写 config — **待定** |
| Node 单实例锁 | 全局 Mutex 名 vs 仅 `.runtime/node.pid` — **待定** |
| Hydrate API 形态 | 独立 `/hydrate` vs 扩展 `/context` | 倾向独立或 `/context?hydrate=1` — **待定** |
| Transcript 映射 | Node 侧组装 vs Web UI 从 `messages[]` 映射 | 倾向 Node 组装稳定 entries — **待定** |
| Session evict 阈值 | ~~单独配置~~ | **与 idle 压缩共用** `idle_auto_compress_seconds`（D34） | **已决** |
| 同 session 通知更新策略 | 首次 Toast + 后续仅更新托盘计数 vs 替换同 tag Toast | — **待定** |
| Windows apply 载体 | tar/zip 覆盖 `bin/*` vs Inno 半静默升级 | v0.6.x 倾向沿用 tar/zip；Inno 远期 — **待定** |
| Shell update localhost 路径 | `GET /v1/desktop/update` vs 并入 desktop API 前缀 | 与 F-P2 共用端口 — **待定** |

---

## 6. 已确认的产品决策（2026-07）

| # | 问题 | 结论 |
|---|------|------|
| 1 | 多 session / 多 HITL 如何通知 | **每 session 一条通知**；同 session 内多项合并 |
| 2 | 「文件关联」含义 | **复制/拖入文件到 UI 输入框 → 绝对路径**；非扩展名双击关联 |
| 3 | Node 实例 | **单实例** |
| 4 | 开机自启 | **是**（Shell 自启 → **立即**起 Node） |
| 5 | Windows 默认 Client | **是**，Shell + Web UI 取代 TUI 推荐位 |
| 6 | 历史消息 + 未完成 HITL | **必须恢复**；Web UI Phase 1（D25–D28） |
| 7 | 退出 Shell | **停止 Node**；Shell 为常驻 supervisor（D29–D30） |
| 8 | 不活跃 session | **idle 扫描：压缩 + 卸内存绑定**（D31–D35）；共用 idle 阈值 |
| 9 | 版本与升级 | **Shell 为 Windows 安装态 orchestrator**（D36–D38）；Node 仅 upgrade-readiness |

**仍可选确认**：A2A relay 通知文案；RDS 多用户；退出 Shell 是否二次确认（F-L11）；Inno vs tar apply。

---

## 7. 生命周期与架构图

### 7.1 进程生命周期（D29–D30）

```text
用户登录 ──► Shell 自启 ──► ensure Node ──►（可选）打开 Web UI tab
                │                │
                │                ├── trigger / inbox / turn（长期任务在 Node）
                │                └── Shell：SSE、Toast、启停监护、**Release 更新**
                │
用户退出 Shell ──► stop Node ──► Shell 进程结束
关浏览器 tab  ──► 仅 UI 结束（Node + Shell 继续）
```

| 关系 | 结论 |
|------|------|
| **Shell vs UI** | Shell **长于** UI |
| **Shell vs Node** | 桌面路径下 **Shell ⊇ Node**（同启同停） |
| **长期任务** | 跑在 **Node**；**Shell 监护**（通知、健康、退出时收尾） |

### 7.2 混合数据流

```text
                    ┌──────────────────────────────────────┐
                    │  dagents-shell.exe（自启、单实例）       │
                    │  · SSE 订阅 · session 待办表 · Toast    │
                    │  · localhost 桌面 API（路径、**update**）│
                    │  · Manage Release 检查 / apply 编排    │
                    └───────────────┬──────────────────────┘
                                    │
          SSE 事件 / health         │  ensure start
          ┌─────────────────────────┼─────────────────────────┐
          │                         │                         │
          ▼                         ▼                         │
┌─────────────────────┐   paste 路径 API                      │
│  dagents-node.exe   │◄──────────────────────────────────────┘
│  单实例 · /v1/*     │         ▲
│  · /ui/ 静态资源    │         │ SSE + REST（运行时直连）
└─────────┬───────────┘         │
          │                     │
          └─────────────────────┘
                    │
                    ▼
          ┌─────────────────────┐
          │  系统浏览器 /ui/     │
          │  Web UI（默认 Client）│
          └─────────────────────┘
```

---

## 8. 数据流评估：是否改为 Node → Shell → UI？

### 8.1 三种模型

| 模型 | 描述 |
|------|------|
| **A. 并行（推荐）** | 事件：`Node → Shell`；UI 打开时：`Node ↔ Web UI` 直连 SSE/API；桌面能力：`Web UI → Shell` |
| **B. 全代理** | 所有 SSE/API 经 Shell 反向代理到 Node |
| **C. 仅 Shell** | UI 只连 Shell，Shell 缓存/转发一切 |

### 8.2 对比（结合已确认需求）

| 维度 | A 并行 | B/C 全代理 |
|------|--------|------------|
| HITL 每 session 一条通知 | ✅ Shell 聚合 SSE 即可 | ✅ 同样可以 |
| 点击通知打开 session UI | ✅ Shell 拼 URL | ✅ |
| Node 单实例 | ✅ 与模型无关 | ✅ |
| Web UI 开发（Vite → Node） | ✅ **不变** | ❌ 需经 Shell 或双配置 |
| 延迟 | ✅ UI 直连 Node | ⚠️ 多一跳 |
| Shell 崩溃 | ⚠️ 无通知；UI 仍可用 | ❌ UI 也断 |
| 路径粘贴 | ✅ Shell 辅助 API | ✅ 自然同进程 |
| 实现量 | ✅ 小 | ❌ Shell 成完整网关 |

### 8.3 结论（D24）

**不建议**在 Phase 1–2 把**全部**数据流改为 `Node → Shell → UI`。

更合适的划分：

```text
① 通知/待办流（UI 关着）     Node ──SSE──► Shell ──► Toast
② 交互流（UI 开着）         Node ◄──SSE/API──► Web UI
③ 桌面能力流               Web UI ──localhost──► Shell（路径、focus、**update 状态**）
④ 安装态更新（Windows）     Shell ──poll──► Manage；apply 时 Shell stop/start Node
```

理由简述：

1. **HITL 需求只在 Shell 做「session 级通知 + 跳转」**，不要求 Shell 代理聊天 SSE。  
2. **Node 单实例** 已保证后端唯一；UI 直连不会破坏该约束。  
3. **路径粘贴、更新检查** 需要 Shell，但这是 **窄 API**，不是全量 SSE 代理。  
4. **Web UI 已内嵌在 Node**，强制走 Shell 代理意味着改 base URL、CORS、Vite 代理链，成本高。  
5. **UI 已打开时**：应用 **F-E9**（UI 上报 focus session，Shell 停止对该 session 弹 Toast），而不是让 Shell 转发所有事件。

### 8.4 何时再考虑全代理（B/C）

- 需要 **离线缓存** 或 **UI 与 Node 解耦部署**；  
- 需要 **统一鉴权/审计** 所有 Client 流量；  
- 改用 **Wails 单窗口** 且 UI 只认 Shell origin。

当前 **浏览器 + 自启 Shell** 路径下，**混合模型（A）更合适**。

### 8.5 通知何时自动消除（含「手工打开 UI」）

**结论**：用户 **不必** 从 Toast 点进 UI；在浏览器里手动打开 `/ui/` 并完成 HITL 后，Shell **仍应自动消除** 该 session 的待办与 Toast——依据 **Node 的 SSE（与 UI 同源）**，而非 UI 是否经 Shell 打开。

#### 数据从哪来

Shell 与 Web UI **并行** 订阅同一 Node：

```text
Node ──SSE──► Shell（全局 live=1）     → 维护 session 待办表、Toast
Node ◄─SSE/API─► Web UI（用户手工打开） → 用户点 resume
```

用户在 UI 里 `POST /v1/messages`（resume）后，Node 继续 turn 并 **再发 SSE**；Shell 在同一连接上都能收到，**与 UI 从哪打开无关**。

#### Shell 侧 session 待办状态机（F-E10 / F-E13）

待办 **类型**（同 session 可并存，对外 **合并为一条** 通知态，F-E3 / D17）：

| 类型 | Node 真相源 | Shell 行为 |
|------|-------------|------------|
| **HITL** | `pending_hitl` 非空 或 `run_turn_phase == awaiting_hitl` | `GET /v1/sessions` 同步 `has_pending_hitl` / `pending_hitl_items` |
| **未读回复** | `has_unread`（`notify_seq > ack_seq`） | 同上；**不**在 Shell 本地解析 `done` 推断 |

Shell **不维护** 本地 `consumed` / `MarkUnread`；SSE 仅 **触发** `GET /v1/sessions` 刷新；60s 轮询兜底（F-E5）。

#### 8.5.1 IM cursor（F-E13 / D40）

**持久化**（SQLite `runtime_state_json`）：

```json
{
  "notify_seq": 128,
  "ack_seq": 120
}
```

| 字段 | 含义 | 更新时机 |
|------|------|----------|
| `notify_seq` | 最后需要 Client 关注的事件 SSE `seq` | `hitl_required` / A2A HITL 事件；`done` 且 turn 正常结束（非 HITL 暂停，见 `ShouldBumpNotifySeq`） |
| `ack_seq` | 各 Client 已确认看到的最大 SSE `seq` | `POST /v1/sessions/{id}/ack` `{ "sse_seq": N }`，取 `max(ack_seq, N)` |
| `has_unread` | 派生：`notify_seq > ack_seq` | hydrate / list / ack 响应中返回 |

**Node API**

| 方法 | 路径 | 请求 | 响应字段（相关） |
|------|------|------|------------------|
| GET | `/v1/sessions/{id}/hydrate` | — | `notify_seq`, `ack_seq`, `has_unread`, `sse_seq_hint`, `pending_hitl`, `run_turn_phase` |
| POST | `/v1/sessions/{id}/ack` | `{ "sse_seq": 128 }` | `session_id`, `notify_seq`, `ack_seq`, `has_unread` |
| GET | `/v1/sessions` | — | 每项：`notify_seq`, `ack_seq`, `has_unread`, `has_pending_hitl`, `pending_hitl_items`, `run_turn_phase` |

**Web UI（Client）**

| 时机 | 动作 |
|------|------|
| hydrate 灌入 transcript + `applyHydrateSeqHint` 后 | `POST /ack` with `lastAppliedSeq` |
| SSE 事件 `markEventApplied(seq)` 后 | `POST /ack` with `max(lastAppliedSeq, seq)` |

**Shell（Client）**

| 时机 | 动作 |
|------|------|
| 启动 / SSE 重连 / 相关 SSE 事件 / 60s 轮询 | `GET /v1/sessions` → 重建待办表 |
| 打开 `/ui/?session=` 深链 | **不**本地 ack；由 Web UI hydrate/SSE 后 `POST /ack` |

**待办表条目**（Shell 内存，由 Node 同步）：

```text
active(session) := has_pending_hitl || has_unread
item_count      := pending_hitl_items + (has_unread ? 1 : 0)
```

#### 8.5.2 HITL 消除（F-E10）

| 事件 | Shell 行为 |
|------|------------|
| HITL 类 SSE | 触发 `GET /v1/sessions` 同步（Node 已 bump `notify_seq`） |
| `done(awaiting_hitl)` | 同步后仍 `has_pending_hitl` → 保持 HITL 待办 |
| HITL 全部处理完 | Node 清 `pending` → 同步后 `has_pending_hitl=false` |
| SSE 断线重连 | `GET /v1/sessions` 核对 `run_turn_phase` / `has_pending_hitl`（F-E5） |

要点：**以 Node 运行时 pending HITL 与 notify cursor 为准**，Shell 不解析 `done` 语义。

#### 8.5.3 同 session 多条 HITL（D17）

- 通知 **一条**，内部计数随 `hitl_required` 的 item 数或队列深度 **递增**。
- 用户在 UI 中 **逐条 resume**，在全部处理完之前 Node 仍会发 `done(awaiting_hitl)` → Shell **不消除**。
- **全部处理完** 后出现 `done(stop)`（或等价非 HITL 暂停）→ Shell **消除**。

#### Toast 与托盘 UI

- **托盘菜单/角标**：随 Shell 内存待办表即时更新（可靠）。
- **托盘图标（F-N10 / D39）**：有待办时切换 **特殊效果**（如角标、高亮、备用 `.ico`）；无待办恢复默认 icon。
- **系统 Toast**：Windows 对「程序化撤销」支持有限；实现上可用 **固定 Toast Group/Tag（ per session）** 在消除待办时 **Replace/Remove** 同 tag 通知；若 API 不支持撤销，则允许 Toast 自然过期，以 **托盘状态为准**（实现细节见 §5）。

#### 可选加速（F-X5，非必须）

Web UI 在 `hitlStore.queue` 变空且 session 无 pending 时，可 **额外** `POST` Shell `ui.hitl_idle`（`session_id`）：

- **优点**：Toast 消除略快、不依赖再等服务端 `done`。
- **缺点**：与 Node 真相可能短暂不一致；**必须以 SSE/`/v1/sessions` 为准**，UI 上报仅作 hint。

**Phase 1 推荐**：只做 **SSE + 轮询兜底（F-E10）**，不做 UI→Shell 消除回调也可闭环。

#### 与 F-E9 的关系

| 机制 | 作用 |
|------|------|
| **F-E9**（UI focus 上报） | 用户 **已打开** 某 session 时，**少弹/不弹新 Toast**（避免人已在看还提醒） |
| **F-E10**（SSE 消除） | 用户 **处理完 HITL** 后，**清待办**（无论 UI 是通知打开还是手工打开） |

两者独立：手工打开 UI → F-E9 可抑制 **新** 通知；处理完毕 → F-E10 消除 **已有** 待办。

### 8.6 Hub 256、Hydrate 与 Session Evict

| 机制 | 作用 | 是否丢 SQLite 历史 |
|------|------|-------------------|
| SSE Hub（256 条） | 新订阅者短回放 | 否（与 messages 无关） |
| SQLite `messages_json` | LLM history 真相源 | 否（压缩会摘要，属产品行为） |
| **Session evict** | 卸内存、减占用 | **否**（persist 后仍在 DB） |
| **GET /hydrate** | UI 从 DB/内存 组装 transcript + pending | 否 |

Evict 后：`ListSessions` 中该 session **`active=false`**；用户/S Web UI **`POST /v1/sessions` + id** 重新激活 → hydrate → 继续。

### 8.7 版本升级与 Shell orchestrator（D36–D38）

| 角色 | 职责 |
|------|------|
| **Manage** | 托管安装包；`GET /v1/releases/check` |
| **Shell（Windows 桌面）** | poll Manage；Toast/托盘提醒；用户确认后 stop Node → 换 `bin/*` → start Node |
| **Node** | **`upgrade-readiness`**（有无 active turn）；**不**再作为 Windows 默认安装的 update 查询入口 |
| **Linux / headless / SSH** | 仍 **Node `UpdateChecker` + `dagents update`**（无 Shell 时不变） |

Apply 流程（Windows）：

```text
用户确认 / dagents.cmd update
  → Shell 问 Node：upgrade-readiness OK?
  → stop Node（F-L2 / nodectl）
  → 下载 latest 包 → 校验 → 覆盖 bin/*、VERSION
  → start Node（F-L1）
  → Shell 刷新 UpdateStatus
```

与 HITL 通知并列：**安装态**事件走 Shell，**运行时**事件仍 Node SSE → Shell。

---

## 9. 相关文档与代码

| 资源 | 路径 |
|------|------|
| 托盘范例 README | `desktop/tray/README.md` |
| Node SSE | `node/internal/api/server.go` |
| Web SSE | `node/webui/frontend/src/sse/stream.js` |
| Web HITL | `node/webui/frontend/src/stores/hitl.js` |
| Go HITL | `client/internal/hitl/` |
| Windows 安装包 | `packaging/windows/dagents-installer.iss` |
| Release Hub / 更新 | `docs/design/release-update-hub.md` |
| Node UpdateChecker | `node/internal/manage/update_checker.go` |
| Client update | `client/internal/update/update.go` |
| 本地助手架构 | `docs/architecture/local-assistant.md` |

---

## 10. 变更记录

| 日期 | 变更 |
|------|------|
| 2026-07 | 初稿：汇总 tray 范例、Shell 演进、功能 Backlog |
| 2026-07 | 产品确认：每 session 一条通知、路径粘贴、Node 单实例、自启、Shell 为默认 Client；§8 数据流评估；D17–D24 |
| 2026-07 | §8.5：HITL 通知消除语义（手工打开 UI + SSE）；F-E10 |
| 2026-07 | D34–D35：idle 压缩与内存 evict 绑定为同一扫描器（§3.10） |
| 2026-07 | 补项 F-L12/13/15、F-E11/12、F-H14/17、F-I8–I10；§4.1 v0.6.0 发布范围 |
| 2026-07 | D36–D38、§3.11、§8.7：Shell 为 Windows 安装态更新 orchestrator；联动 release-update-hub §10 |
| 2026-07 | §4.1 链至 [v0.6-v0.7-roadmap.md](./v0.6-v0.7-roadmap.md) |
| 2026-07 | D39–D40、F-E13、F-N10：托盘图标待办态 + 未读 assistant 回复纳入待办 |
| 2026-07 | F-E13 IM cursor：`notify_seq`/`ack_seq`/`POST /ack`；Shell 从 Node 同步，§8.5.1 |
