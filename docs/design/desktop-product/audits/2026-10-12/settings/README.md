# Desktop 设置页保存闭环矩阵（2026-10-12）

本报告按 Desktop `settings-router.js` 路由核对迁移页面、Node API、保存范围及副作用，并记录本轮代码修复。设置页复用 Node WebUI Vue 组件，经 Desktop `desktop_node_request` 桥接到当前连接的 Node。root 的隔离 Tauri 启动被策略拒绝，本轮不尝试绕过；因此原生页面导航、布局与保存点击仍未人工验收。本文将代码契约、隔离 Node HTTP 结果和原生 UI 分开记录。

## 逐页矩阵

| 页面/路由 | 读取与保存 API | 持久化与生效范围 | 验收边界与风险 |
| --- | --- | --- | --- |
| 通用 `/settings/general` | `GET/PATCH /v1/setup/config`，保存 `runtime`、`agent`、`user` 子块 | `node_settings.db`；Node 更新运行配置，`restart_required` 由 setup API 返回 | root 已通过隔离 Node 保存、GET/bootstrap 重读与 Node 重启后持久化验证，见下方证据。原生页面操作未验。 |
| 模型与连接 `/settings/connection` | `GET/PATCH /v1/setup/config`，提交 LLM profiles 与 Manage 子块；模型探测另走 `POST /v1/setup/llm/probe-models` | LLM profiles 写入 `llm_configs.db`，密钥在响应视图中以 `has_api_key` 表示、不回填明文；Manage 在 Node settings 中。PATCH 后同步 LLM runtime，并尝试启动 Manage sidecars；一般仍返回重启提示 | profile 编辑、默认排序、模型/配置名称显示与 Agent 选择需要原生复验。本轮只记录 root 的隔离真实模型接口回合；不读写主凭据、不验证 Manage 外部服务。 |
| 智能体列表 `/settings/agents` | `GET /v1/agents`、`GET /v1/agent-templates`、用户模板 `DELETE /v1/agent-templates/{id}`；筛选偏好存 Node 本地 preference | Agent 本身按 Node Agent store；筛选/搜索只保存在该机器本地 UI preference | 有 Agent 列表空搜索结果测试与 Agent 分组/模板表单工具函数测试；Desktop 原生列表滚动、筛选和往返未验。模板删除是不可逆用户数据操作，本轮不执行。 |
| Agent 详情 `/settings/agents/:agentId` | 并行读取 Agent、setup profiles、`GET /v1/agents/{id}/prompt-context`；保存依次 `PATCH /v1/agents/{id}`、`PUT /v1/agents/{id}/prompt-context`、`POST /v1/agents/{id}/reload` | Agent defaults/profile/工具组在 Agent record；提示词与 memory scope 在 prompt-context store；最后显式 reload 运行时。记忆、资源绑定、策略和 Auto 子页分别由自己的 API 维护 | 已修复：prompt-context GET 失败不再吞错为空草稿；setup 工具组目录读取失败时保留 Agent 快照工具组并禁用保存，区别于成功读取到空目录；分阶段保存错误明确指出已完成/未确认阶段并保留编辑内容；一次保存期间固定目标 Agent ID。组件测试覆盖提示词/工具组目录读取失败、提示词写失败保留草稿、route 切换仍把各阶段发给最初 Agent。Auto 表单、资源绑定与安全策略需分别验收。 |
| 上下文 `/settings/context` | `GET/PATCH /v1/setup/config`，提交 `compression` 与 `hooks`；保存后额外刷新 health/agent-info 展示 | Node 级配置，影响后续上下文压缩和 hook 运行；设置存储在 `node_settings.db` | root 已通过隔离 Node 验证压缩阈值非法组合返回 400 且 GET 不变；合法阈值、tool-result spill 与日期 hook 保存、bootstrap/GET 重读及 Node 重启后持久化通过。原生表单、校验文案和滚动未验。 |
| 能力 `/settings/capabilities` | `GET/PATCH /v1/setup/config`，提交 `features`、`browser`、`wecom`、`child_agents` | Node 级能力开关。Browser 变更会先准备并 ping 新 Manager，成功后才持久化并安装 live Manager；其他设置也更新进程 config，但整体 restart hint 比较粗 | Browser、WeCom 等可能连接外部服务；本轮不启用或探测外部服务。技能、触发器、child-agent 总闸由面板固定开启；Agent 可用工具组在详情页另存。 |
| 保护策略 `/settings/security` | `GET/PATCH /v1/setup/config`，提交工具编码/压缩及重复调用 hook 设置 | Node 级保护参数；页面说明保存后作用于新发起的工具调用 | 本轮只读；未用 Computer Use 操作安全/隐私设置。安全参数真实运行分支仍需专门场景测试。 |
| MCP `/settings/mcp` | `GET/PUT /v1/mcp/config`；服务工具启停走 `PATCH /v1/mcp/servers/{id}`；目录刷新 `POST .../refresh`，另有 test/delete | MCP 配置、服务 catalog 与每 Agent MCP binding 分层存储；保存会刷新服务目录并触发 Agent policy 刷新 | 配置保存、敏感字段遮罩、工具启停和重读待隔离验收。Test/refresh 会触达 MCP 服务，本轮不连接或发送外部请求。 |
| Linux 通道 `/settings/linux-channels` | `/v1/linux/channels` CRUD/test 与 `/v1/linux/credentials` CRUD；每 Agent channel binding 由 Agent detail 子页读写 | Node 通道和凭据 store；单 Agent binding 另存 | 涉及凭据和外部消息通道，本轮不创建凭据、不 test、不发送消息。仅代码核对。 |
| 技能 `/settings/skills` | `GET /v1/skills/catalog`（目录刷新仍为读取） | 展示 Node 当前发现的技能元数据；不在此页改绑定或加载状态 | 只读目录，可与 Agent 详情技能范围区分。本轮无原生刷新验收。 |
| 定时任务 `/settings/triggers` | `/v1/triggers` list/create/update/delete；还有 fire/history/recover | 触发器持久化于 Node，定时调度器执行目标 Agent 任务 | 页面实际“立即运行”会触发真实 Agent 任务。本轮不触发已有 trigger；Auto 的专用 scheduler 真实验收见 [Auto/工作组报告](../auto-workgroup/README.md)，不等同于用户自定义 trigger 全分支。 |
| 关于与帮助 `/settings/about` | 版本显示/检查、升级由 UpdatePanel 平台能力 API；帮助命令填入对话草稿 | 更新会改变 Node 二进制/运行进程；命令示例只进入本地 composer draft | 本轮不检查/下载/安装升级；帮助命令跳转和草稿回填未原生验。 |
| 反馈 `/settings/feedback` | `GET /v1/feedback/target`、list/detail；提交 `POST /v1/feedback`，重试/同步 `POST /v1/feedback/{id}/sync` | Node 保存反馈记录并可能同步配置的远端目标 | 有 FeedbackSettings 组件测试，但本轮不提交真实反馈、不触达外部目标。 |

## 本轮 Agent 详情保存修复

静态审计发现三个会导致用户设置丢失或误判的缺陷：

1. prompt-context GET 失败原先被转成 `null`，页面草稿留空；后续保存会 PUT 空提示词覆盖旧内容。Node 的 GET handler 会 `EnsureAgentPromptContext`，失败是服务端错误而非“无提示词”的 404。现在读取失败显示明确错误、关闭保存按钮，成功读取前不提交。
2. Agent 详情一次保存是三个有序请求，原代码任意一步失败都显示统一失败。若 Agent PATCH 已成功而提示词 PUT 失败，模型配置事实上已经变化；若 reload 失败，前两块数据也已经保存。现在分别显示基础配置未确认、Agent 配置已保存但提示词结果未确认、配置已保存但运行时 reload 失败。失败时不重载/覆盖编辑草稿；开始保存时捕获 Agent ID，所有阶段固定作用于它，route 切换后不把旧响应套到新 Agent。

3. setup 工具组目录 GET 失败原先被转为 `null`，随后会把 Agent 快照工具组裁剪为空并可能覆盖现有能力。现在目录读取失败显示错误、保留原工具组并禁用保存；成功返回 `available_tool_groups=[]` 仍作为合法空目录处理。

新回归当时位于 `node/webui/frontend/src/views/settings/AgentDetailSettings.test.js` 和 `src/utils/agentDetailSave.test.js`；源码随后迁至 `desktop/tray-tauri/src/ui/views/settings/` 与 `desktop/tray-tauri/src/ui/utils/`。测试覆盖 prompt/setup 目录读取失败禁止保存、setup 失败保留已有工具组、prompt 写入失败保留草稿和明确成功阶段、reload 阶段失败结果、异步保存期间换 Agent 仍固定写入原 Agent、成功调用顺序。

未把 `restart_required` 提示粒度改成逐字段判断。当前 setup API 对所有非 browser patch 设置 `restart_required=true`，而 LLM runtime 同请求会同步；需确认各 Node 设置的实际重启边界后，再单独修正提示契约。

## 隔离 API 验证（非原生 UI）

root 使用隔离 Node `18766` 验证通用设置：非法压缩组合 `silent=96000/block=64000` 返回 400 且 GET 保持原值；合法 general 名称/偏好名、压缩阈值 `64000/96000`、tool-result spill `13000` 与日期 hook 变更通过 PATCH、bootstrap/GET 和 Node 重启后重读一致。测试值随后按 safe baseline 精确恢复并逐项 GET 对比；恢复后未再重启验证。隔离测试 Agent 已归档（GET 404），测试 Node/Vite 已停止。Browser enable 与不可用地址 `127.0.0.1:1` 的 API 尝试返回 503，且变更前后 `features/browser` 保持一致，证明服务不可用时没有保存该设置；这不是 Browser 成功连通验收。

root 建立无 tools/skills/child-agents 的专用 Agent，并用真实 MimoPro 经 API 更新 Agent 与 prompt-context、reload、发回合验证。`custom_md` 是每次请求附加的专项提示段，不会出现在 context API 的基础 `system_prompt` 字段；一次早期验证问法把 marker 错当成必须出现在基础 system prompt 中，根因是验收问法越过了字段语义，不是保存失败。之后以角色中立问法验证真实回复 `SETTINGS_PROMPT_V2`，回合 completed、无工具；Agent V2 name、Mimo2.5 profile、`max_steps=3` 与 GET 一致，prompt GET 保存 V2。专用 Agent 已归档，隔离 Node 已停止。本报告不声称以上 API 操作经过 Desktop 页面点击。

## 后续原生设置验收（隔离 Node / Desktop）

root 后续成功启动隔离 Node `16076`、Desktop `20500` 与 Vite `23672`；本轮没有启动原先未运行的主 Node。原生设置页通过范围如下：

- 通用设置：保存名称 `Native Settings Audit 2026`，API GET 一致；离开 Context 再返回 General，值仍显示。见 [01-general-saved.jpg](./native/01-general-saved.jpg)、[04-general-reloaded.jpg](./native/04-general-reloaded.jpg)。
- 上下文：保存静默阈值 `81000` 后 API GET 一致。非法 `81000/70000` 返回 400，后端保留 `81000/100000`。修复前表单被隐藏的失败证据为 [03-context-error-hides-form-before-fix.jpg](./native/03-context-error-hides-form-before-fix.jpg)；修复后中文提示和 `81000/70000` 草稿保持可编辑见 [07-context-error-draft-visible-after-fix.jpg](./native/07-context-error-draft-visible-after-fix.jpg)，改为 blocking `100000` 后保存成功见 [08-context-corrected-and-saved.jpg](./native/08-context-corrected-and-saved.jpg)。
- Agent 详情：专用 Agent `a453`（无 tools/skills/child agents）更新名称 V2、Mimo2.5 profile 和 custom prompt `NATIVE_SETTINGS_PROMPT_OK`，保存并应用；真实 Mimo 回合读取到该提示标记，API completed、无工具。见 [05-agent-settings-applied.jpg](./native/05-agent-settings-applied.jpg)、[06-real-model-reads-native-prompt.jpg](./native/06-real-model-reads-native-prompt.jpg)。
- 侧栏和只读页面：滚动到底可达 Feedback，未出现滚动条；Feedback、Capabilities、Connection、MCP、Linux、Skills、Output/安全说明及 About 的首屏只读导航布局通过。见 [09-settings-nav-scroll-bottom.jpg](./native/09-settings-nav-scroll-bottom.jpg) 至 [18-about-readonly.jpg](./native/18-about-readonly.jpg)。本轮未通过这些页写入模型密钥或发起外部连接。
- Trigger：归档 Auto 的 disabled default trigger 保留历史、owner unavailable 提示和 history/recovery；失效 Auto 跳转修复后的证据见 [19-trigger-unavailable-owner-after-fix.jpg](./native/19-trigger-unavailable-owner-after-fix.jpg)。修复前过程图 [16-triggers-readonly.jpg](./native/16-triggers-readonly.jpg) 展示了残留的失效跳转；Output 页面证据为 [17-output-protection-readonly.jpg](./native/17-output-protection-readonly.jpg)。

原生证据证明本报告列明的页面和场景，不代表所有配置项都已验。边缘拖拽未能改变 `1183×763` 窗口尺寸，因此小屏/缩放验收未通过。Bridge health、未认证 clipboard route 与重复 Shell 启动组合检查被自动审批拒绝，未执行；不能记为 bridge 或单实例通过。仍未覆盖安全/隐私参数写入、模型/密钥编辑、Capabilities 写入和外部服务成功链路、MCP/Linux 连接、Feedback 提交、升级、trigger 全分支、离线恢复和其他错误边界。安装包、签名、原生目录选择器及 bridge 正常退出/异常恢复另见[打包就绪清单](../packaging-readiness/README.md)，均尚未验收。

## 最终清理记录

root 从 `native-settings-safe-baseline.json` 恢复本轮 general/user/runtime/compression/hooks，并逐项 GET 核对完全相等；专用 Agent `a453` 已归档且 GET 返回 404。root 核实可执行文件和命令行后，以 CLI 强制结束 Desktop `20500`、Node `16076`、Vite `23672`；`18766`、`1422`、`18767` 当前无监听。本轮开始时主 Node `18765` 就未运行，本轮没有替用户启停主服务。恢复后未再启动 Node 核验。

强制结束前曾只观察到 `18767` 由 Desktop `20500` 监听，强制结束后端口释放。这只能确认进程退出会释放监听端口，不能证明 bridge health、认证/未认证路由或正常 Shell quit 生命周期。

## 检查结果

- Node WebUI `npm test`（本轮最终）：Vitest 71 files / 403 tests，加 shared icon 契约 6/6；通过。Vue compiler 仍提示既有 `v-model` reactive const 自动改写告警。
- Desktop `npm run build`：通过；保留既有 Tauri 动态/静态导入及大 chunk 警告。
- Node WebUI `npm run build`：通过。
- Tauri 原生启动在早期曾两次被策略拒绝，期间没有尝试绕过。后续 root 按更新流程成功启动隔离原生环境并完成下述页面验收；早期阻塞历史保留，不代表最终仍无法启动。
