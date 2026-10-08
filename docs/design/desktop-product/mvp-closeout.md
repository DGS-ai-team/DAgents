# Desktop MVP 收束与验收记录

日期：2026-10-08。对象是 `desktop/tray-tauri` 中的真实 Windows Tauri 窗口及本机 Node，设计原型仅作布局参考。

## 本轮交付边界

这是可连接本机 Node 的开发版 Desktop 工作台。它完成了已有智能体的对话、工具审批、终端资源和配置页面的首条实际使用链路；它尚未替代 Node WebUI，也不代表 Desktop / Node 分离技术方案全部完成。

- Desktop 主窗口使用合并菜单/窗口控制栏。设置位于 DAgents 菜单，侧栏底部只显示连接状态。
- 侧栏保留智能体、工作组、自主智能体三组；可折叠到图标导航，空分组和未接入能力有明确提示。
- 对话与终端资源分开展示。终端页右下角有消息坞，可发送消息、展开最近消息/工具和审批、返回完整对话；悬浮坞避让终端命令栏。
- Agent 切换时清除上一位的流式文本，并保留各自的输入草稿。发送失败会恢复或保留待恢复文本，避免覆盖用户继续输入的内容。
- execute_tool 审批展示工具名、审批理由和实际参数，可批准或拒绝。其他 HITL 类型目前提示到原 Node 控制台处理。
- 设置页复用现有 Node 的通用、模型与连接、MCP、Linux 通道、能力、技能、智能体与详情、定时任务、输出防护、上下文、关于、反馈组件。配置事实仍只保存在 Node。
- 设置页与外围使用统一底色；进入/离开设置时侧栏和顶栏位置平滑过渡，支持减少动画偏好。导航溢出用边缘渐隐提示，保留滚轮和键盘滚动。

## 当前实现职责

| 部分 | 入口 | 职责 |
| --- | --- | --- |
| 工作台 | `desktop/ui/DesktopApp.vue`、`desktop-ui.css` | 布局、Agent 选择、对话/资源呈现、草稿与审批交互 |
| 可测试的局部状态 | `desktop/ui/workbench-state.js` | 路由映射、参数展示、草稿切换与失败恢复 |
| 终端文本呈现 | `desktop/ui/terminal-output.js` | 跨块 UTF-8 解码及 CSI/OSC 过滤；不是完整终端模拟器 |
| 设置承载 | `desktop/tray-tauri/src/settings-router.js`、`src/main.js` | 复用配置页面，通过 Tauri transport 调用 Node API；同步当前 Agent 到诊断组件 |
| 宿主与传输 | `desktop/tray-tauri/src-tauri/src/lib.rs`、`nodeclient.rs`、`terminal.rs`、`events.rs` | 窗口/托盘、REST/SSE、PTY WebSocket 中继与命令校验 |
| 图标 | `desktop/ui/assets/icons/` | 应用自包含资产副本；来源和许可见设计包 `icons/manifest.json` |
| 设计参考 | 本目录的 `prototype/`、`icons/`、`visuals/` | 模拟交互、图标画板、历史探索与当前布局参考，不能当作已接入的功能 |

目前只接受 loopback Node，原始认证凭据由 Rust 宿主持有，UI 通过 Tauri commands 调用固定 Node。Node 地址可作为连接信息显示；UI 不自行直连 Node。终端会话归属于 Agent：切换 Agent/关窗脱离连接并保留进程，明确关闭资源则关闭会话。原托盘 Console 与 Node `/ui/` 保留用于首配和未迁移功能。

## 验收证据与限度

| 项目 | 结果与证据 |
| --- | --- |
| 真模型对话 | 在隔离 Node 中复用已有的非 Mock `mimo-v2.5-pro` 配置，从真实 Tauri 输入并发送连通性测试。实际回复“Desktop 实测通过”，Node hydrate 中回合已完成且消息持久化。 |
| 真模型工具审批 | 在临时 Agent 工作目录创建 `hello.txt`，将 `read_file` 设置为每次审批。实际模型发出 read_file 请求；在 Tauri 中批准后工具成功执行，模型返回 `DESKTOP_APPROVAL_ACCEPTED`。未在日常 Agent 会话中写入验收消息。 |
| 终端链路 | 开启 `DAGENTS_TERMINAL_SMOKE` 后，Rust 测试实际连接 Node PTY，验证输入、脱离、恢复、历史回放和关闭。原生窗口另核对连接状态、关闭资源菜单、消息坞和命令栏布局。未通过 UI 自动化输入系统命令。 |
| Node 流与回合 | 开启 `DAGENTS_MVP_SMOKE`，验证真实本机 Node REST/SSE/hydrate 链路；该 transport smoke 使用 Mock 模型，与上面真模型测试分开记录。 |
| 配置 | 原生窗口验证通用配置保存成功，并检查设置页面导航、实际数据和空状态。页面迁移不等于所有表单、凭据、SSH/MCP 实际服务、升级和反馈投递均已端到端实测。 |
| 原生视觉 | 检查合并顶栏、对话输入、设置底色/过渡/导航渐隐、终端与消息坞；修复提示行截断、审批参数挤在一行、终端 OSC 乱码和消息坞遮挡命令栏。 |
| Desktop 检查 | `npm run build`、`npm run test:ui`（9 项）、`cargo fmt --check`、`cargo test --lib`（28 项，包括两个启用的 live smoke）通过。 |
| 干净依赖构建 | 临时移开 Node WebUI 的 `node_modules` 后 Desktop 仍可构建，随后恢复。Desktop 显式声明并解析 Markdown 依赖，不依赖 Node 前端已安装的包。 |
| 共享 WebUI 回归 | Node 前端构建成功；67 个测试文件、369 项测试通过。 |
| 当前模拟原型 | `prototype/` 构建成功，`test:sites` 4 项通过。它的模拟浏览器/文件不算生产能力。 |

上述检查在 Windows 本机开发环境完成。没有据此验证 macOS/Linux 原生窗口、发行安装包、签名、更新回滚、网络长时间断线或多 DPI 完整矩阵。隔离真实模型测试目录中的凭据副本应在验收后清除；运行状态、私钥、数据库、依赖和构建输出均不入库。

本次清理状态：临时真实模型 Node 已停止，常规开发窗口恢复连接 `18765` 的 Mock 测试 Node。自动审批拒绝了删除本地临时文件的操作（只返回 `blocked by policy`）；`.runtime-test-desktop-real/.runtime/llm_configs.db` 与 `.llm_secret_key` 两个测试副本仍需由用户清理。该目录已忽略，不在提交中；主工作区原有模型配置未修改。

## 下一阶段仍需完成

按产品闭环优先排序，不在本次基础工作台收束中扩展：

1. **日常对话完整性**：新建 Agent/对话、停止与恢复、模型切换、消息 Markdown/附件和完整工具气泡、更多 HITL 类型；补齐空 Node 的直接首配，并完成 Auto 激活/审批的原生窗口长时间运行测试。
2. **资源能力**：将简单文本终端推进到可支持交互程序和尺寸变化的终端组件；接入实际文件打开/预览/上传下载、内置浏览器观看与接管，明确 Node 文件与 Desktop 文件边界。
3. **设置深度验收**：覆盖全部修改/保存/重载与错误分支，使用真实 MCP/SSH/Manage 验证服务和反馈闭环；修正继承页面中的 WebUI 专属说明与快捷键。
4. **连接与发布**：远程 Node、连接握手/版本能力协商、凭据与缓存隔离、CSP 和权限收紧、三平台构建/性能/DPI 验收、安装升级回滚。
5. **最终职责分离**：抽出共享设置组件/API 契约，消除 Desktop 对 `node/webui/frontend/src` 的源码依赖。功能覆盖并稳定后再移除 Node UI 与旧 Shell 轨道。

工作组、浏览器和文件入口在当前实现中明确不可用。以上缺口使本轮适合作为本地开发 MVP 验收点，尚不足以直接宣布完整 Desktop 产品迁移或正式发布。
