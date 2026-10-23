# Desktop 接入范围与 Node 交互收束

日期：2026-10-20。本文记录产品决定：Desktop 只连接同机 Node；远程 Node 与 SSH tunnel 连接模式不进入产品主线。Linux 仍可通过 Node 的 SSH/远程终端工具操作，不代表 Desktop 连接 Linux 上的远程 Node。

## 已接入的日常功能

| 用户任务 | Desktop 入口 | 权威状态与传输 |
| --- | --- | --- |
| Agent 对话、切换模型、工具审批、Auto 配置与总览 | 工作台与共享 Node 设置组件 | Agent、配置、消息和回合状态由 Node 保存；普通 JSON 经 Tauri Rust Node transport；消息增量走 SSE，最终消息以 hydrate 为准。 |
| 附件、历史图片、工作区文件浏览/编辑/预览 | 对话附件与文件资源页 | 文件路径及内容由 Node 工作区 API 处理；Tauri Rust 代理有界二进制，Desktop 仅保存草稿和 UI 状态。 |
| PowerShell、Linux/SSH 终端 | 终端资源页 | PTY 与运行历史归 Node；Tauri Rust 负责 WebSocket 中继。关闭资源页只脱离中继，明确结束才请求 Node 销毁会话。 |
| 浏览器资源 | Agent 浏览器只读画面 | Node 管理 Playwright 会话并启动本机 Browser sidecar；Desktop 拉取同一会话快照。当前没有用户接管、地址栏或点击控制。 |
| 工作组、Node 设置及首次配置 | 工作组入口、设置页 | Node 保存 Agent/运行时设置；Manage 承担工作组协调与后台配置。Desktop 偏好单独保存通知和更新检查选项。 |

## 本次收束后的传输

```text
Desktop Vue
  ├─ 一般 Node JSON ── Tauri IPC: desktop_node_request ── Rust Node Client ── 127.0.0.1:18765
  ├─ 文件/媒体二进制 ─ Tauri IPC 有界命令 ────────────────┘
  ├─ 对话事件 ───────── Node SSE ── Rust subscriber ── Tauri event ── Vue
  └─ 终端 WebSocket ─── Rust relay ─────────────────── Node PTY session

Node ── 本地 sidecar API: 127.0.0.1:18766 ── dagents-browser
```

Node transcript 与 turn state 是对话事实来源。SSE delta 用于及时展示；`turn_finished`、错误、审批及断线恢复会触发 hydrate 对账。新实现让 Desktop 在首次连接时订阅 live stream，重连后携带全局 cursor；Node 重启使游标超前时返回 `resync_required` 并将游标夹回当前进程序号。Desktop 对工具事件做短 debounce，避免每个事件重新走选 Agent 和模型信息读取；每 30 秒无 SSE 活动且 UI 仍显示生成时，watchdog 通过 hydrate 修复卡住的状态。

Desktop 固定访问 Node `127.0.0.1:18765`，Node 固定访问 Browser sidecar `127.0.0.1:18766`。Desktop JSON 不再保存 `node_endpoint` 或 `connection_mode`；旧字段被忽略并在下一次保存时移除。Node Browser 设置 API 不再暴露 endpoint/CDP 地址，旧 Browser 地址由默认配置收束为固定本机 sidecar。

## 仍未接入或需要独立验收的能力

下面按“为了独立日常使用是否必须补齐”排序。这里把“功能未接入”和“已经接入但缺少完整实测证据”分开，避免把已存在的页面重复列为开发缺口。

| 优先级 | 能力 | 当前状态 | 建议 |
| --- | --- | --- | --- |
| P0 | **Desktop / Node 版本与 API 兼容性握手** | Desktop 只显示 Node 版本；没有最低 API 版本或能力协商。旧 Node 占用固定端口时，可能被误认为可用 Node，随后才在具体页面报错。 | 发版前补一条轻量 `/v1/meta` 契约：Node 版本、API 主版本、能力集合；Desktop 启动时校验并给出“版本不匹配 / 如何修复”，不尝试连远程 Node。避免按每个按钮维护一份手写兼容矩阵。 |
| P0 | **断线、Node 重启和长任务恢复的端到端验收** | SSE cursor、resync 和 hydrate 已补齐单测；自动激活、Dreaming、审批变体和长时间运行依赖 Node 状态与真实模型，单测不能证明完整 UI 闭环。 | 在隔离 Node 上验收“运行中断线、Node 重启、最终回复已写入 transcript、Desktop 恢复后状态一致”四种路径，并用 Mock 覆盖重复回归；真模型只验证一次无工具消息和一次审批链路。 |
| P1 | **浏览器人工接管** | 当前是 Agent 浏览器会话的只读画面；没有用户登录后导航、点击/键盘输入、接管/交还 Agent 等流程。 | 若产品要达到 Codex 式内置浏览器，这是关键差异化能力，应作为独立项目设计：Node 持有浏览器会话，Desktop 发受限控制指令，明确“用户接管 / Agent 执行”状态并保留审批边界。当前只读查看可继续日常使用，不应为了补齐菜单而匆忙加一组点击按钮。 |
| P1 | **常见文档的只读预览覆盖** | PDF、DOCX、XLSX、图片、HTML/SVG 与常见代码已有预览；旧版 DOC/XLS、PPT/PPTX 和超过有界传输限制的文件只能下载或提示不支持。 | 先收集真实使用频率；优先补 PPTX 只读预览，再决定旧版 Office 转换。不做完整 Office 编辑器。所有预览继续使用 Node 有界二进制传输，禁止把工作区文件路径交给 Desktop 本机直接读取。 |
| P2 | **跨平台 Desktop 发布** | 当前实际支持与主要验收目标为 Windows x64；macOS/Linux 原生包、签名、升级/回滚、多 DPI 尚未完成发布验收。 | 按目标用户与维护成本分阶段推进；在 Windows 安装、升级和回滚验收通过前，不把跨平台当作当前 Desktop 接入缺口。 |
| 外部验收 | **MCP、SSH、Manage、通知、更新、Auto 与审批分支** | 入口及 Node/Manage API 已接入，但结果依赖外部服务、凭证、Node 工具配置和特定审批状态。 | 按每日使用的部署配置做一份端到端验收矩阵。Manage 管理员意见查看、部署级注册/管理等管理职责继续留在 Manage；不为了“Desktop 独立”复制一套管理后台。 |

**明确不列为待接入**：远程 Node / SSH tunnel、Node 多用户与用户级 runtime、完整 Office 编辑、Manage 管理后台。这些要么已从产品方向中移除，要么属于另一个产品边界；Linux 日常操作继续通过当前 Node 的 SSH/远程终端工具完成。

## 降低复杂度的边界

当前保留四种必要数据通道：普通 JSON、受限二进制、SSE、终端 WebSocket。它们对应不同的数据语义，不应为减少行数而合并成一个无类型的大接口。可继续收束的是重复的 UI refresh 行为、过期连接设置、未使用的 Tauri command 和过多 UI 轮询。本次已移除无调用方的 Node connection 查询与消息发送 command；模型消息仍由现有 Node API helper 统一走 Tauri JSON transport。

暂不建议把所有 Node API 全部改成 Desktop 专用命令或复制一份 DTO：那会扩大接口维护面。更合适的边界是 Node 保持领域 API 的唯一事实源，Desktop Rust 提供固定 endpoint、认证/权限边界、媒体大小限制与特殊流传输，Vue 复用现有 API helper 和设置组件。

交互上建议维持这条简单规则：**Node 管业务与会话事实，Desktop 管呈现与本机窗口能力，Manage 管组织级管理**。断线恢复只保留一条路径——SSE 增量、重连后按 cursor 补播，缺口或 Node 进程变化时 hydrate；不要再让每类事件各自重新拉 Agent、模型、历史和工具状态。一般 API 只走一个有路径校验的 JSON bridge；文件继续走有大小上限的二进制 bridge；终端保留 WebSocket relay；浏览器 sidecar 只由 Node 管理。固定 localhost 后不要再保留“连接模式”“自定义 Node URL”“隧道启动/停止”状态分支。
