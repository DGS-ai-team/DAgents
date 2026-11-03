# v1.0.5 功能减负与实现收敛验收

本轮以 Windows Desktop、本机 Node、可选 Manage 协作为产品边界。保留智能体日常聊天、自主激活、技能与工具执行、资源工作台和工作组，不把低频调优选项放在日常操作路径中。

## 完成清单

| 项目 | 最终行为 | 主要代码 |
| --- | --- | --- |
| 工具停止入口 | 工具气泡不再提供单工具终止；输入栏停止本轮统一取消任务 | `desktop/tray-tauri/src/ui/components/ToolSummaryRow.vue`、`stores/toolJobs.js` |
| 状态动效 | 工具组保留文字扫光；同工具仅参数垂直切换，不同工具整体切换；输入栏状态保留垂直切换与扫光 | `components/ToolGroupRow.vue`、`components/ComposerToolbar.vue`、`desktop-ui.css` |
| 实时工具记录 | hydrate 包含正在执行的工具调用，等待结果时仍显示执行中；终态优先于旧的运行提示 | `node/internal/session/hydrate_view.go`、`node/internal/turn/coordinator.go` |
| 模型故障恢复 | 临时错误有界重试并展示进度，失败明确提示原因、释放操作；响应有空闲超时，部分正文持久保存，产生输出后不自动重放 | `node/internal/llm/provider_error.go`、`openai.go`、`node/internal/turn/model_attempt_runner.go`、`node/internal/session/turn_state_view.go`、`DesktopApp.vue`、`stores/turnState.js` |
| 清空对话边界 | 持久化清空前的运行代次，隐藏旧工具显示投影而保留恢复日志；清空与边界一次写入，失败返回错误，新一轮工具正常显示 | `node/internal/session/runtime.go`、`runtime_persistence.go`、`hydrate_view.go` |
| 旧聊天实现 | 删除未使用的 transcript、toolStream、streamReveal 状态链；Agent store 仅保留当前 Agent 身份 | `desktop/tray-tauri/src/ui/stores/agent.js`、`DesktopApp.vue` |
| 内部 CLI 冗余接口 | client 保留 probe/update/version 所需接口，删除未消费的聊天、SSE、hydrate、上下文和技能客户端包装 | `client/internal/api/client.go` |
| 子智能体进度 | 现有 SSE 与 hydrate.child_agents 合并到同一状态；切换父 Agent 清空作用域，空快照清理残留，拒绝旧请求覆盖新事件 | `stores/remoteWorkers.js`、`composables/useStream.js`、`DesktopApp.vue` |
| 模型配置权威 | 模型档案只保存在 llm_configs.db；node_settings 仅保留活动引用；无持久库时返回错误，不再假保存到 YAML | `node/internal/store/llm_sync.go`、`node_settings.go`、`node/internal/api/setup_api.go`、`llm_api.go` |
| 启动与迁移 | 先校验监听等引导配置，加载持久设置后再校验业务配置；已有模型库优先，旧副本成功迁移后才精简 | `shared/config/config.go`、`node/internal/store/node_settings_bootstrap.go`、`node/internal/api/server.go` |
| 凭据保护 | 保留已有 .llm_secret_key；旧 APIKeyEnv 缺失时拒绝部分迁移并暂停设置写入，保留旧数据并提示带原环境变量重试 | `node/internal/store/llm_sync.go`、`node_settings_test.go`、`v011_restore_test.go`、`node/internal/api/llm_migration_readiness_test.go` |
| 临时岗位授权 | 移除 UI、策略存储和路由；权限继续通过智能体工具策略配置 | `components/PolicyPanel.vue`、`node/internal/policy`、`node/internal/api/agent_policy_api.go` |
| 命令与扩展包 | 移除命令帮助、/thinking、Desktop/Node 上传发布链路；保留 /clear 与菜单中的上下文压缩；Manage 仓库保留 | `slash-commands.js`、`DesktopApp.vue`、`node/internal/api/server_routes.go` |
| 模型思考参数 | 根据 provider/model 显示支持的开关、强度和预算，切模型时清理不适用参数；旧档案按允许值和范围归一化，避免旧 Qwen effort、OpenAI max 或越界预算阻止无关设置保存 | `components/LlmProfileModal.vue`、`node/internal/llm/runtime_settings.go`、`node/internal/store/llm_sync.go` |
| 高级设置 | 上下文、输出防护、共享服务的低频参数折叠；上下文页仍显示当前自动压缩和空闲归档策略 | `components/ContextConfigPanel.vue`、`SecurityConfigPanel.vue`、`CapabilitiesConfigPanel.vue` |
| 设置一致性 | 共用标题、保存栏、选择器；智能体详情子页切换保留父框架；常用界面使用“本地服务”等用户可理解措辞 | `components/SettingsPageHeader.vue`、`SettingsSectionHeader.vue`、`SettingsSaveBar.vue`、`UiSelect.vue`、`views/settings/AgentDetailSettings.vue` |
| 资源配置闭环 | MCP/Linux 空状态可跳转新增，保存后回到原智能体；新增全局连接不会自动授予该智能体权限 | `components/McpAgentPanel.vue`、`LinuxAgentPanel.vue`、`views/settings/McpSettings.vue`、`LinuxChannelsSettings.vue` |
| 工作区引用发送 | 完整聊天与悬浮输入栏共用引用组装和提交；返回聊天仍显示引用，允许只发送引用，失败保留，成功仅清理本次引用 | `DesktopApp.vue`、`workspace-references.js`、`components/WorkspaceReferenceAttachments.vue` |
| 工作组可发现性 | 已启用协作、有工作组或当前位于工作组时显示侧栏组；菜单保留首次配置入口 | `DesktopApp.vue`、`workbench-state.js` |
| Manage 角色界面 | 发布操作仅管理员可见；普通用户只浏览目录，空状态不再指向隐藏按钮；高级 ACL 收起 | `manage/console/frontend/src/components/MarketplaceView.vue`、`SkillsView.vue`、`PluginsView.vue`、`ExternalToolsView.vue`、`WorkgroupView.vue` |
| 长对话渲染 | 持久历史与流式尾项分离；逐块文本更新不重建全部历史，保留同长度内容变更和工具状态更新 | `components/MainChatPanel.vue`、`DesktopApp.vue` |
| 上下文锁占用 | 锁内复制可变消息数据，锁外估算 token | `node/internal/session/context_view.go` |
| 冷会话状态 | 容量 32 的只读投影缓存，每次按持久事件 ID 和数量校验；完整恢复路径保持日志重放 | `node/internal/session/lifecycle_projection.go`、`node/internal/store/turn_events.go` |
| 文档边界 | README、开发文档、Desktop 说明与实际本机连接、可导航浏览器能力一致 | `README.md`、`desktop/tray-tauri/src/ui/README.md`、`docs/development.md` |

表内 Desktop 简写路径以 `desktop/tray-tauri/src/ui/` 为根。

## 验证记录

- Desktop：84 个测试文件、570 项 Vitest 测试，另有 7 项图标契约和 106 项 UI 状态检查通过；工作区引用单测扩充后 9 项通过。后续重试短文案定向测试 29 项与 lint 通过。lint、构建和 Tauri Windows 原生构建通过。
- Manage：控制台 lint/build、Ruff、Pyright、212 项 Python unittest 通过。使用独立测试数据库分别登录管理员与节点身份，确认发布按钮和空状态文案按角色一致。
- Go：Go 1.26.10 下五个模块的 test、vet、staticcheck 通过；Node 与 client 构建通过。完整脚本发现的两项 staticcheck 遗留已修正并补跑。
- Rust：格式检查与 77 项测试通过。品牌资源契约、API/fixture 契约、OpenAPI 路由覆盖和 git diff 检查通过。
- PR 升级回归：缺环境变量时，无关设置保存与模型切换都不会删除旧模型副本；带原变量重启后两档模型及密钥恢复。旧 Qwen effort 不再回显，合法预算与密钥保留，新提交的非法 effort 仍被拒绝。
- 第二轮审查回归：旧 OpenAI o3 的 max 归一化后可保存其他设置；取消工具后清空对话，当前与重启后的 hydrate 均为空，恢复后新轮次调用仍显示；SQLite 故障注入证明清空快照不发生部分写入且错误能返回调用方。工作区组件测试覆盖引用文件、切换聊天、仅引用发送、失败保留及重试成功清理。
- 原生联调：使用独立运行目录和本地 OpenAI SSE fixture，执行真实 PowerShell 固定睡眠命令；界面显示“执行中”，气泡无终止按钮。点击输入栏“停止本轮”后，服务返回 `cancelled_by_user`、工具状态 `cancelled`、`has_active_turn=false`、运行工具数 0。fixture、临时 Agent 和临时模型档案已清理。
- 工具组的参数独立切换、工具名称整体切换、快速更新与展开态保持由组件测试覆盖；单工具行本来不使用工具组标题动画。降低动态效果禁用扫光与过渡；强制颜色模式禁用渐变扫光。
- 模型故障联调：两次 503 后第三次成功；持续 503 在三次请求后失败；401 不重试；退避期间取消不再发送请求；超过 30 秒的 Retry-After 不自动重试；输出部分正文后断流仅请求一次。六类场景的 SSE 和 hydrate 均符合终态，之后新消息可成功并清空对话；部分正文带 `partial` 标记持久保存，并进入下一次请求的上下文。
- 模型故障原生验收：聊天显示安全原因及已重试次数，发送和模型选择恢复；悬浮输入区也显示失败。退避状态保留输入栏停止和文字动效，短文案避免次数被截断。此次使用本地故障服务，不代表真实服务商故障恢复率。

- 提交 922cfaf3 的完整 CI（run 41619773131）与 CodeQL 均通过；机器人复审完成且无发现，PR #383 已合入 dev。
- 使用原 v1.0.4 CI 二进制与 922cfaf3 候选构建完成隔离安装、升级及卸载。对话历史、自定义工作区、Agent/全局记忆、已有工具输出和加密模型凭据保留，正式安装设置的校验值未变。安装器前后数据库校验值与启动迁移后的卸载基线分别保存；业务字段和迁移结构已核验，没有迁移前完整 JSON 快照，不能声称全部字段逐项等价。工具输出为已有文件 fixture，此升级测试没有外部模型调用。
## 性能与边界

Windows i5-13490F 的合成基准（单次采样，仅用于前后比较）：

| 场景 | 未缓存 | 命中缓存 |
| --- | --- | --- |
| 1,000 条事件 | 5.111 ms / 3.11 MB 分配 | 0.281 ms / 1.04 KB |
| 10,000 条事件 | 146.773 ms / 37.73 MB 分配 | 2.957 ms / 1.95 KB |

一万条合成聊天历史构建及分组约 6.394 ms；100 次流式尾项更新未触发任何全量构建。冷缓存仍查询 `MAX(event_id), COUNT(*)`，版本校验成本随会话事件数增长，不是常数时间。

本轮未验证外部真实付费模型、真实 SSH 主机和跨机器工作组环境；本地 SSE fixture 不等同于真实 LLM 验收。Windows 本机 race 检查受到 ThreadSanitizer/toolchain 环境问题阻碍，CI 的 Linux race 检查已通过。此次清单完成不代表整个仓库已无任何死代码或性能改进空间。

升级前备份完整 `.runtime`（包括 `.llm_secret_key`）；保留旧环境变量凭据。删除的旧命令/上传/临时授权接口不再承诺兼容，已安装技能、工具和 Manage 仓库继续可用。
