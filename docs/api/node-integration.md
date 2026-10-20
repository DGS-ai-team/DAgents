# Node Runtime API 接入契约

本文覆盖 Node Go HTTP Server 当前注册的 **150 个 method/path 路由**：`GET /health` 与 149 个 `/v1` 操作。它描述的是 Node runtime API，不是 Desktop 设置 API，也不是 Manage 服务 API。路径参数用 `{name}` 表示。Node 不内嵌 Web UI、不提供 `/ui` 静态页面；`node/webui/frontend` 源码仍由 Desktop WebView 复用。

## 传输与安全边界

Node 默认监听 `127.0.0.1:18765`（实际监听地址仍由启动 YAML 决定）。Desktop transport 固定访问本机 `127.0.0.1:18765`，不支持远程 Node 或 SSH tunnel 模式。Node HTTP API 本身没有通用 Bearer 鉴权，也不提供多用户 ACL，因此不要将 API 暴露到不可信网络。`GET /v1/node/update/package` 与未完成首配时的 `GET /v1/node/upgrade-readiness` 另有 loopback 请求限制。Node 发往 Manage 的凭据仅用于出站请求，不会由 settings 或 update API 回传。

多数 JSON 成功响应直接返回 DTO 或操作结果，不存在统一的 `data` envelope。标准错误由 `writeAPIError` 返回 `{"error":{"code":"...","message":"...","details":{...}}}`。具体 handler 可能有专属状态码、二进制或流响应；应以实现和 DTO 为准。Go `ServeMux` 的 method pattern 属于接口契约。

## 配置和持久化职责

| 数据 | 所有者与存储 | 接口和语义 |
|---|---|---|
| Node 监听地址 | 启动 YAML 的 `listen.host`、`listen.port`；修改后重启 Node 生效。 | `/v1/settings` 只读返回 `node.listen_host`、`node.listen_port`；PATCH 不会迁移运行中的 listener。 |
| Node runtime 设置 | Node runtime root 下的 `node_settings.db`。 | 通过 Node 设置接口管理 Node 自身的运行偏好。 |
| LLM profile | Node LLM 配置 SQLite store。 | `/v1/llm/settings` 管理运行时 profile；`/v1/settings` 也提供 setup/profile 视图。API key 只回传是否配置，不回明文。 |
| Agent 定义和运行快照 | Node Agent store、runtime/session 状态。 | `/v1/agents*`、policy、context、skills、MCP/Linux channel 关联及工具/事件接口。`node_id` 表示 Node service identity；历史字段和路由中的 Agent 命名保留兼容，指向 Node 管理的 Agent 时按字段语义理解。Node profile 当前表示单 owner 档案，没有多用户/设备 ACL。 |
| MCP server 设置 | Node MCP server store。 | `/v1/mcp/*`；MCP handler 按接口规则隐藏 secret。 |
| Linux channel 与 credential（含 SSH/远程主机配置） | Node Linux channel store。 | `/v1/linux/*`；属于 Node host 管理能力。当前没有独立 `/v1/ssh/*` 路由。 |
| Agent handbook 与 workspace | 每个 Agent 对应的 Node 文件系统和 handbook 存储。 | `/v1/agents/{agent_id}/handbook*`、`workspace*`、`/media/*`。目录选择由 Node host 的原生 directory-picker 执行。 |
| Manage 连接凭据 | Node 配置：Manage URL、Node token、registration 和 Workgroup 配置。 | `/v1/settings` 接受 write-only `manage.node_token`。GET 仅返回 `has_node_token`。PATCH `node_token: ""` 保留已保存值；`clear_node_token: true` 才清除。 |
| Desktop endpoint 与 Shell 设置 | Desktop 自有 settings JSON：`notification_enabled`、`update_check_enabled`、`update_check_interval_seconds`。Node endpoint 固定为 `127.0.0.1:18765`；旧 `node_endpoint` / `connection_mode` 字段被忽略并在下次保存时移除。 | 不属于 Node `/v1/settings`。Desktop 固定管理本机 Node，不提供远程连接档案或 tunnel 配置。 |
| Desktop notifications 与 schedule | Desktop 自己持有的设置和状态。 | 不属于 Node runtime 设置。 |
| 显示偏好和草稿 | 前端 `localStorage`。 | 不属于服务端配置，也不在 Node/Desktop profile 间共享。 |

Node 首配维护单 owner 档案：`agent.name` 是完成 Node 初始化所必需的 Node 展示名称，不会改变 `node_id` service identity；`user.preferred_name` 可选，仅作为写入 Node prompt 的称呼提示，不代表登录名或认证身份。`onboarding.node_profile_completed` 只表示 Node 档案初始化完成，不代表 LLM profile 已配置、可连接或已验证。LLM 可用性应根据现有 profile 配置和 probe 结果判断。

## 接入流程与关键接口

### Node 信息、聚合与设置

`GET /v1/node/info` 返回 Node info DTO，字段包含 `node_id`、`manage_enabled`、`manage_url`、`manage_registered`，也提供 capabilities、LLM 与 compression 信息。`GET /v1/bootstrap` 为 Desktop WebView 首屏提供 `health`、`info`、`llm`、`user`、`onboarding`、`agent` 聚合；Node 不托管 Web UI；此聚合减少首屏往返，不取代原子 info/settings API。

`GET /v1/settings` 返回 `SettingsView`；`PATCH /v1/settings` 顶层可省略不相关设置块。view 顶层包含 `node`、`llm`、`manage`、`features`、`compression`、`memory`、`runtime`、`agent`、`user`、`onboarding`、`child_agents`、`browser`、`wecom`、`tools`、`hooks`、`available_tool_groups`。`node` 仅含 listener 字段。除 `manage` 以字段 presence 合并外，大多数设置块按其 DTO 一次整体应用；发 patch 前先 GET 并合并该块，仅在客户端明确提供完整块或接口定义了专门的子字段合并时省略旧字段。`manage` 的字段是可选指针，未出现的字段保留现值；出现的字段按 presence 更新。Manage token GET 示例是 `{"has_node_token":true}`，不会有 `node_token` 明文。PATCH `node_token: ""` 保留原 token；`clear_node_token: true` 才清除。显式 `url: ""` 是清空 URL，若最终 `enabled: true` 会因缺 URL 而校验失败。Setup panel 加载时密码输入框始终空白，空值提交保持原 token，明确清除需勾选清除字段。首配完成只要求 Node 名；`preferred_name` 可空。完整 DTO/patch 合并逻辑见 `node/internal/setup/settings.go`。

读取 Node 设置：

```sh
curl http://127.0.0.1:18765/v1/settings
```

写入 Manage token（示例值为占位符；Node 不会再从 GET 回显）：

```sh
curl -X PATCH http://127.0.0.1:18765/v1/settings \
  -H 'Content-Type: application/json' \
  -d '{"manage":{"node_token":"REPLACE_WITH_TOKEN"}}'
```

保留现有 token 的 patch：

```sh
curl -X PATCH http://127.0.0.1:18765/v1/settings \
  -H 'Content-Type: application/json' \
  -d '{"manage":{"node_token":""}}'
```

明确清除 token：

```sh
curl -X PATCH http://127.0.0.1:18765/v1/settings \
  -H 'Content-Type: application/json' \
  -d '{"manage":{"clear_node_token":true}}'
```

### LLM 配置与密钥保留

`PATCH /v1/settings` 的 `llm.profiles` 是完整配置列表；更新时保留其他仍需使用的配置，并用 `llm.active` 指定当前配置 ID。每个 profile 的 `api_key` 是只写字段：省略或留空保留该配置的已存密钥，非空值替换密钥，`clear_api_key: true` 明确清空且优先于新值。GET/PATCH 的响应仅通过 `has_api_key` 表示是否已保存，不回传明文。

主动修改配置 ID 时，为该条目附带只写 `original_id`，表示从哪个现有配置保留密钥；它只用于本次保存，不会作为配置字段或长期别名回传。未提供新密钥且未要求清空时，Node 从原配置迁移密钥；来源不存在、来源仍保留在最终列表、多个目标引用同一个来源或目标与其他已有配置冲突时，保存失败，不能静默丢弃或误用密钥。Node 会检查所有未归档 Agent 快照（包括 Auto Agent）的 `defaults.llm.active`：仍被引用的配置 ID 不能改名或删除，`PATCH /v1/settings` 返回 `409 llm_profile_in_use`，错误详情列出 Agent ID，并提示先更新或删除这些 Agent。删除 Agent 会归档其记录，因此归档 Agent 不再阻止配置变更。相同 ID 的普通配置编辑，以及未被活动 Agent 引用的配置改名或删除仍可保存；Node 不会迁移 Agent 绑定。子 Agent 是临时运行时并继承父 Agent 的模型设置，没有独立持久化的模型配置 ID，因此由父 Agent 引用检查覆盖。工作组 Supervisor 配置由 Manage 保存并使用 Manage 的模型配置 ID 命名空间，不属于 Node `llm_configs.db` 的引用检查范围。Desktop 编辑已有配置时，测试获取模型或选择模型不会自动修改配置 ID。

### 消息、SSE 与 HITL

普通 Agent 的空闲边界由 Node 管理，设置字段为 `compression.idle_new_conversation_seconds`（非负整数，`0` 关闭）。它在空闲后的下一条真实用户消息开始前执行，不是后台摘要任务；修改后按设置响应中的 `restart_required` 完成重启。原 `idle_auto_compress_seconds`、扫描间隔及最低 token 字段不再属于当前配置。Auto 使用自身 Dreaming 流程，不按此间隔重置。

成功持久化新段后，SSE `runtime/notice` 的安全载荷包含 `message_kind: "context_boundary"` 和稳定的 `boundary_id`。hydrate transcript 中对应 `{ "kind": "context_boundary", "boundary_id": "..." }`。客户端按 ID 去重，在本次真实用户消息之前显示“新对话 · 上下文已重置”，不得渲染内部衔接正文或归档路径。消息队列中较晚到达的输入不改变此边界的顺序。SQLite 中的既有 transcript 继续保留；模型可读的段归档由 Node 写入 Agent 自身工作区的 `.dagents/<agent_id>/history/`。

`POST /v1/messages` 接收 `agent_id`、`content` 和可选 `request_type`；默认类型为 `message`，`resume` 用于继续等待 HITL 的 Agent。Node 异步执行 turn，接口立即返回 `{ "accepted": true, "agent_id": "...", "priority": "..." }` 一类的接受结果。

```sh
curl -X POST http://127.0.0.1:18765/v1/messages \
  -H 'Content-Type: application/json' \
  -d '{"agent_id":"AGENT_ID","content":"你好"}'
```

`GET /v1/streams` 使用 `text/event-stream`。SSE wire frame 使用标准 `event:`、可选 `id:` 和 `data:`。`data` JSON 是 `stream.Event`：包含 `agent_id`、`type`、`seq`、可选 `agent_seq`、`event_version`、`stream_epoch`、`delivery`、`ts`、事件对应的 `data`。**事件判别字段是 `type`，不是 `event_type`。**`resync_required` 表示回放游标太旧，应先 hydrate。查询游标、live 订阅和 `Last-Event-ID` 以 `server_messages.go` 为准。

```sh
curl -N 'http://127.0.0.1:18765/v1/streams?agent_id=AGENT_ID&live=1'
```

HITL resume、tool-call cancel、execution reconcile 等接口维护运行时状态，不能只根据某个界面暂未调用就判定为废弃。消息请求和 SSE event wire schema 分别见 `node/internal/api/server_messages.go`、`node/internal/api/execution_events_api.go` 与 `node/internal/stream/hub.go`。

### 更新查询和二进制包

`GET /v1/node/update` 每次主动请求时执行 Manage release check，直接返回 shared `update.Status`：`current_version`、`latest_version`、`upgrade_available`、`manage_reachable`、`platform_supported`、`manage_enabled`、`update_enabled`、可选 `check_interval_seconds`、`last_checked_at`、`channel`、`platform`、可选 `release_notes`、`message`、`apply_command`、`asset`。`platform_supported=false` 表示当前 `platform` 没有已发布安装包；release check 与下载都按完整平台标识匹配，不会回退到其他架构。Node status 不声称 Shell 能安装更新。

`GET /v1/node/upgrade-readiness` 返回当前 session manager 的活动状态。首配完成前仅 loopback 请求可用，以便本机 Desktop 安装器在升级或卸载前确认没有活动 turn 或待处理 HITL；该状态不会通过远程访问开放。

Desktop/Shell 先获取新状态，再读取候选包：

```sh
curl -i http://127.0.0.1:18765/v1/node/update
curl -f http://127.0.0.1:18765/v1/node/update/package -o dagents-update-package
```

包接口仅 loopback 可用，不接受 query/body；候选必须有 `download_url`、`filename`、`sha256`、`size_bytes`，且 `size_bytes` 在 1 字节至 1 GiB 间。Node 仅允许配置 Manage URL 同源的地址，拒绝重定向；带 Manage token 的 upstream status/download client 均不跟随 redirect。候选 status 最近一次成功检查不得超过 15 分钟，过期返回 `409 update_check_expired`，调用方应重新请求 `/v1/node/update` 再下载。Node 先将包写入 OS 临时目录，校验完整字节数和 SHA-256 后才开始发响应。成功是 `200 application/octet-stream`，响应头含 `Content-Disposition`、`Content-Length`、`X-Node-Package-Version`、`X-Node-Package-SHA256`。DAgents Desktop updater 通过 Node package 接口取包，不携带 Manage token。其他失败仍返回 Node JSON error envelope。安装、重启由 Desktop Shell 负责，不属于这个下载 API 的能力。

### Workspace、媒体、terminal 与平台能力

Workspace 文件路径通过 `path` query 参数传递。读取使用 `GET /v1/agents/{agent_id}/workspace/file?path=...`，响应体是原始文件字节并带 ETag；更新使用 `PUT` 和原始字节请求体，必须携带 `If-Match: <etag>`，新建则用 `If-None-Match: *`。上传使用 `POST /v1/agents/{agent_id}/workspace/upload?path=...`，请求体同样是文件原始字节（不是 multipart）；Node 按新建处理，目标已存在时返回 412。成功的写入/上传返回 JSON 元数据和新的 ETag。Desktop 客户端通过 `desktop_workspace_binary_request` 传输这些二进制请求，路径和条件头分别作为 IPC 参数传入。媒体读取是另一条二进制路由：`GET /v1/agents/{agent_id}/media/{media_id}`。

文件 GET 支持 `If-None-Match` 条件读取：使用内容摘要生成的强 ETag，条件比较按 HTTP 的弱比较语义处理；匹配返回 `304`、当前 ETag 和空正文，未匹配返回 `200` 及最新字节。客户端必须保留 304 之前的内容，不能把空正文当作空文件。此读取条件不改变 PUT 的 `If-Match` 冲突保护。Desktop 自动刷新只检查当前可见目录及打开文件；大小与修改时间用于加速发现变化，不能替代内容版本校验。调度及草稿规则见[资源页自动刷新方案](../design/desktop-product/resource-auto-refresh.md)，消息资源引用和 HTML/SVG 预览见[资源引用协议](../development/resource-references.md)。

Browser 页面只读快照由 `GET /v1/agents/{agent_id}/browser/view?page_id=...` 提供；它读取该 Agent 已有浏览器会话的页面/截图，不会启动浏览器、执行页面代码或提供交互控制。AutoDreaming 状态由 `/v1/agents/{agent_id}/dreaming` 提供，体验条目、待办和唤醒触发器分别由 experience/todos/triggers 领域路由管理。Browser sidecar 固定在 `127.0.0.1:18766`，由本机 Node 启动和停止。

`GET /v1/agents/{agent_id}/terminals/ws` 升级为 WebSocket。客户端首帧可为 `open`（创建终端，可带 target、shell、cwd、rows、cols）或 `resume`（用 `session_id` 重新连接已有会话）；后续客户端命令为 `input`、`resize`、`terminate`、`close`、`ping`。JSON 中的 `data` 是 base64 编码的字节。断开 WebSocket 会 detach 会话，终端仍可恢复；显式 `close` 或 `terminate` 才关闭会话。screen stream、transfer events 也是流协议，细节以 handler 为准。目录选择是 Node host 原生能力：`/v1/platform/capabilities` 报告例如 `native_directory_picker`，`POST /v1/platform/directory-picker` 由 Node 进程调用 OS picker。

剪贴板文件路径、窗口聚焦、update apply **不是 Node HTTP 路由**。Desktop UI 中对应的路径字符串仅是 `globalThis.__DAGENTS_DESKTOP_REQUEST__` 的 IPC selector，由 Tauri 直接处理；不要向 Node HTTP server 请求。纯浏览器环境中这些 Shell 功能不可用。

## 路由边界与调整结论

- `GET /v1/bootstrap` 与 `GET /v1/node/info` 保留：前者是减少 Desktop WebView 首屏请求的聚合，后者是 Node 信息的独立 DTO。
- `/v1/settings` 与 `/v1/llm/settings` 都保留：settings 是全量 Node 管理/setup 视图，LLM endpoint 是现有 runtime profile 的轻量操作面；职责虽有字段重叠，但消费者和保存通道不同，当前不合并。
- Agent、policy、context、workspace、HITL、tools、MCP、Linux channels、Manage Workgroups、feedback、Manage upload 保留为领域 API。Workgroup 路由由当前 Node Workgroup adapter 处理，不是已退役的 Desktop bridge API；Feedback 写入 Node 本地记录并尝试同步 Manage；Manage upload 是 Node 到 Manage 的上传代理。不能因为 Desktop WebView 当下没有调用就移除 Manage、Node 内部工具或 Go/Python client 依赖的操作。
- Desktop Shell 专属路径退出 Node HTTP：剪贴板、窗口聚焦与 update apply 改走 Shell IPC；Node 自己的 native directory picker 和 host API 仍保留。
- Node update status 与 binary package 留在 Node：Manage token 仅由 Node 持有；Shell 仅读取状态和完整性已验证的包。Node 不再暴露 Desktop runtime config 或安装动作。
- 当前没有为剩余路由推断弃用状态。拆分应以真实所有权和消费者为准，不为减少路径而强行合并不同 domain CRUD。

## 当前路由全集

下表从 `node/internal/api/server_routes.go` 及其调用的 `register*Routes` 函数生成。完整路径面、HTTP 方法均逐项列出。除上文已给出 wire contract 的 API 外，各端点细节以对应 `*_api.go`、请求/响应 DTO 和测试为准；OpenAPI 对其余项标为 `surface-only`，只声明路由注册，不假装具备完整 schema。

### 探活、Node runtime、设置和平台 host

| Method | Path | Handler | Source |
|---|---|---|---|
| `GET` | `/health` | `handleHealth` | [server_routes.go#L6](../../node/internal/api/server_routes.go#L6) |
| `GET` | `/v1/bootstrap` | `handleUIBootstrap` | [ui_aggregate_api.go#L12](../../node/internal/api/ui_aggregate_api.go#L12) |
| `POST` | `/v1/llm/probe-models` | `handleProbeLLMModels` | [setup_api.go#L19](../../node/internal/api/setup_api.go#L19) |
| `GET` | `/v1/llm/settings` | `handleGetLLMSettings` | [llm_api.go#L16](../../node/internal/api/llm_api.go#L16) |
| `PATCH` | `/v1/llm/settings` | `handlePatchLLMSettings` | [llm_api.go#L17](../../node/internal/api/llm_api.go#L17) |
| `GET` | `/v1/node/info` | `handleAgentInfo` | [server_routes.go#L7](../../node/internal/api/server_routes.go#L7) |
| `GET` | `/v1/node/update` | `handleAgentUpdate` | [server_routes.go#L8](../../node/internal/api/server_routes.go#L8) |
| `GET` | `/v1/node/update/package` | `handleNodeUpdatePackage` | [server_routes.go#L9](../../node/internal/api/server_routes.go#L9) |
| `GET` | `/v1/node/upgrade-readiness` | `handleAgentUpgradeReadiness` | [server_routes.go#L10](../../node/internal/api/server_routes.go#L10) |
| `GET` | `/v1/platform/capabilities` | `handlePlatformCapabilities` | [platform_api.go#L8](../../node/internal/api/platform_api.go#L8) |
| `POST` | `/v1/platform/directory-picker` | `handlePlatformDirectoryPicker` | [platform_api.go#L9](../../node/internal/api/platform_api.go#L9) |
| `GET` | `/v1/settings` | `handleGetSetupConfig` | [setup_api.go#L17](../../node/internal/api/setup_api.go#L17) |
| `PATCH` | `/v1/settings` | `handlePatchSetupConfig` | [setup_api.go#L18](../../node/internal/api/setup_api.go#L18) |

### Agent、消息、SSE 与 HITL

| Method | Path | Handler | Source |
|---|---|---|---|
| `GET` | `/v1/agent-templates` | `handleListAgentTemplates` | [agent_templates_api.go#L16](../../node/internal/api/agent_templates_api.go#L16) |
| `POST` | `/v1/agent-templates` | `handleCreateAgentTemplate` | [agent_templates_api.go#L18](../../node/internal/api/agent_templates_api.go#L18) |
| `DELETE` | `/v1/agent-templates/{id}` | `handleDeleteAgentTemplate` | [agent_templates_api.go#L19](../../node/internal/api/agent_templates_api.go#L19) |
| `GET` | `/v1/agent-templates/{id}` | `handleGetAgentTemplate` | [agent_templates_api.go#L17](../../node/internal/api/agent_templates_api.go#L17) |
| `GET` | `/v1/agents` | `handleListAgents` | [agents_api.go#L26](../../node/internal/api/agents_api.go#L26) |
| `POST` | `/v1/agents` | `handleCreateAgent` | [agents_api.go#L25](../../node/internal/api/agents_api.go#L25) |
| `DELETE` | `/v1/agents/{agent_id}` | `handleDeleteAgent` | [agents_api.go#L36](../../node/internal/api/agents_api.go#L36) |
| `GET` | `/v1/agents/{agent_id}` | `handleGetAgent` | [agents_api.go#L27](../../node/internal/api/agents_api.go#L27) |
| `PATCH` | `/v1/agents/{agent_id}` | `handlePatchAgent` | [agents_api.go#L28](../../node/internal/api/agents_api.go#L28) |
| `POST` | `/v1/agents/{agent_id}/ack` | `handleAgentAck` | [agents_api.go#L46](../../node/internal/api/agents_api.go#L46) |
| `GET` | `/v1/agents/{agent_id}/auto-config` | `handleAutonomyV2ConfigGet` | [autonomy_v2_api.go#L16](../../node/internal/api/autonomy_v2_api.go#L16) |
| `PUT` | `/v1/agents/{agent_id}/auto-config` | `handleAutonomyV2ConfigPut` | [autonomy_v2_api.go#L17](../../node/internal/api/autonomy_v2_api.go#L17) |
| `POST` | `/v1/agents/{agent_id}/auto-config/reconcile` | `handleAutonomyV2ConfigReconcile` | [autonomy_v2_api.go#L18](../../node/internal/api/autonomy_v2_api.go#L18) |
| `GET` | `/v1/agents/{agent_id}/browser/view` | `handleAgentBrowserView` | [browser_view_api.go#L9](../../node/internal/api/browser_view_api.go#L9) |
| `POST` | `/v1/agents/{agent_id}/cancel` | `handleAgentCancel` | [agents_api.go#L44](../../node/internal/api/agents_api.go#L44) |
| `GET` | `/v1/agents/{agent_id}/child-agents` | `handleAgentListChildAgents` | [agents_api.go#L52](../../node/internal/api/agents_api.go#L52) |
| `GET` | `/v1/agents/{agent_id}/child-agents/{child_agent_id}` | `handleAgentGetChildAgent` | [agents_api.go#L53](../../node/internal/api/agents_api.go#L53) |
| `POST` | `/v1/agents/{agent_id}/child-agents/{child_agent_id}/cancel` | `handleAgentCancelChildAgent` | [agents_api.go#L54](../../node/internal/api/agents_api.go#L54) |
| `POST` | `/v1/agents/{agent_id}/clear-context` | `handleAgentClearContext` | [agents_api.go#L47](../../node/internal/api/agents_api.go#L47) |
| `POST` | `/v1/agents/{agent_id}/compress` | `handleAgentCompress` | [agents_api.go#L48](../../node/internal/api/agents_api.go#L48) |
| `GET` | `/v1/agents/{agent_id}/context` | `handleAgentContext` | [agents_api.go#L45](../../node/internal/api/agents_api.go#L45) |
| `GET` | `/v1/agents/{agent_id}/dreaming` | `handleDreamingStatus` | [dreaming_scheduler.go#L185](../../node/internal/api/dreaming_scheduler.go#L185) |
| `POST` | `/v1/agents/{agent_id}/ensure` | `handleAgentEnsure` | [agents_api.go#L38](../../node/internal/api/agents_api.go#L38) |
| `GET` | `/v1/agents/{agent_id}/execution-events` | `handleAgentExecutionEvents` | [agents_api.go#L41](../../node/internal/api/agents_api.go#L41) |
| `GET` | `/v1/agents/{agent_id}/experience` | `handleAutonomyV2ExperienceGet` | [autonomy_v2_api.go#L23](../../node/internal/api/autonomy_v2_api.go#L23) |
| `GET` | `/v1/agents/{agent_id}/handbook` | `handleGetAgentHandbook` | [agents_api.go#L33](../../node/internal/api/agents_api.go#L33) |
| `GET` | `/v1/agents/{agent_id}/handbook/history` | `handleGetAgentHandbookHistory` | [agents_api.go#L34](../../node/internal/api/agents_api.go#L34) |
| `POST` | `/v1/agents/{agent_id}/handbook/restore` | `handleRestoreAgentHandbook` | [agents_api.go#L35](../../node/internal/api/agents_api.go#L35) |
| `GET` | `/v1/agents/{agent_id}/hydrate` | `handleAgentHydrate` | [agents_api.go#L40](../../node/internal/api/agents_api.go#L40) |
| `GET` | `/v1/agents/{agent_id}/linux-channels` | `handleGetAgentLinuxChannels` | [linux_channels_api.go#L62](../../node/internal/api/linux_channels_api.go#L62) |
| `PUT` | `/v1/agents/{agent_id}/linux-channels` | `handlePutAgentLinuxChannels` | [linux_channels_api.go#L63](../../node/internal/api/linux_channels_api.go#L63) |
| `GET` | `/v1/agents/{agent_id}/mcp` | `handleGetAgentMCP` | [mcp_api.go#L42](../../node/internal/api/mcp_api.go#L42) |
| `PUT` | `/v1/agents/{agent_id}/mcp` | `handlePutAgentMCP` | [mcp_api.go#L43](../../node/internal/api/mcp_api.go#L43) |
| `GET` | `/v1/agents/{agent_id}/mcp/effective-tools` | `handleGetAgentEffectiveMCPTools` | [mcp_api.go#L44](../../node/internal/api/mcp_api.go#L44) |
| `GET` | `/v1/agents/{agent_id}/media/{media_id}` | `handleGetAgentMedia` | [media_api.go#L19](../../node/internal/api/media_api.go#L19) |
| `GET` | `/v1/agents/{agent_id}/policy` | `handleGetAgentPolicy` | [agent_policy_api.go#L33](../../node/internal/api/agent_policy_api.go#L33) |
| `GET` | `/v1/agents/{agent_id}/policy/grants` | `handleGetAgentPolicyGrants` | [agent_policy_api.go#L36](../../node/internal/api/agent_policy_api.go#L36) |
| `POST` | `/v1/agents/{agent_id}/policy/grants` | `handlePostAgentPolicyGrant` | [agent_policy_api.go#L37](../../node/internal/api/agent_policy_api.go#L37) |
| `DELETE` | `/v1/agents/{agent_id}/policy/grants/{grant_id}` | `handleDeleteAgentPolicyGrant` | [agent_policy_api.go#L38](../../node/internal/api/agent_policy_api.go#L38) |
| `PUT` | `/v1/agents/{agent_id}/policy/shell/{shell_type}` | `handlePutAgentShellPolicy` | [agent_policy_api.go#L35](../../node/internal/api/agent_policy_api.go#L35) |
| `PUT` | `/v1/agents/{agent_id}/policy/tools` | `handlePutAgentToolPolicy` | [agent_policy_api.go#L34](../../node/internal/api/agent_policy_api.go#L34) |
| `GET` | `/v1/agents/{agent_id}/prompt-context` | `handleGetAgentPromptContext` | [agent_policy_api.go#L39](../../node/internal/api/agent_policy_api.go#L39) |
| `PUT` | `/v1/agents/{agent_id}/prompt-context` | `handlePutAgentPromptContext` | [agent_policy_api.go#L40](../../node/internal/api/agent_policy_api.go#L40) |
| `DELETE` | `/v1/agents/{agent_id}/prompt-context/memory/{entry_id}` | `handleDeleteAgentMemoryEntry` | [agent_policy_api.go#L42](../../node/internal/api/agent_policy_api.go#L42) |
| `PATCH` | `/v1/agents/{agent_id}/prompt-context/memory/{entry_id}` | `handlePatchAgentMemoryEntry` | [agent_policy_api.go#L41](../../node/internal/api/agent_policy_api.go#L41) |
| `POST` | `/v1/agents/{agent_id}/reload` | `handleAgentReload` | [agents_api.go#L39](../../node/internal/api/agents_api.go#L39) |
| `GET` | `/v1/agents/{agent_id}/screen/status` | `handleAgentScreenStatus` | [screen_api.go#L17](../../node/internal/api/screen_api.go#L17) |
| `GET` | `/v1/agents/{agent_id}/screen/stream` | `handleAgentScreenStream` | [screen_api.go#L16](../../node/internal/api/screen_api.go#L16) |
| `GET` | `/v1/agents/{agent_id}/skills` | `handleAgentListSkills` | [agents_api.go#L49](../../node/internal/api/agents_api.go#L49) |
| `POST` | `/v1/agents/{agent_id}/skills/load` | `handleAgentLoadSkill` | [agents_api.go#L50](../../node/internal/api/agents_api.go#L50) |
| `POST` | `/v1/agents/{agent_id}/skills/unload` | `handleAgentUnloadSkill` | [agents_api.go#L51](../../node/internal/api/agents_api.go#L51) |
| `GET` | `/v1/agents/{agent_id}/terminals` | `handleAgentTerminals` | [terminal_api.go#L50](../../node/internal/api/terminal_api.go#L50) |
| `GET` | `/v1/agents/{agent_id}/terminals/ws` | `handleAgentTerminalWS` | [terminal_api.go#L49](../../node/internal/api/terminal_api.go#L49) |
| `DELETE` | `/v1/agents/{agent_id}/terminals/{terminal_id}` | `handleDeleteAgentTerminal` | [terminal_api.go#L51](../../node/internal/api/terminal_api.go#L51) |
| `GET` | `/v1/agents/{agent_id}/timeline` | `handleAgentTimeline` | [agents_api.go#L42](../../node/internal/api/agents_api.go#L42) |
| `GET` | `/v1/agents/{agent_id}/todos` | `handleAutonomyV2TodosGet` | [autonomy_v2_api.go#L19](../../node/internal/api/autonomy_v2_api.go#L19) |
| `POST` | `/v1/agents/{agent_id}/todos` | `handleAutonomyV2TodoPost` | [autonomy_v2_api.go#L20](../../node/internal/api/autonomy_v2_api.go#L20) |
| `DELETE` | `/v1/agents/{agent_id}/todos/{todo_id}` | `handleAutonomyV2TodoDelete` | [autonomy_v2_api.go#L22](../../node/internal/api/autonomy_v2_api.go#L22) |
| `PATCH` | `/v1/agents/{agent_id}/todos/{todo_id}` | `handleAutonomyV2TodoPatch` | [autonomy_v2_api.go#L21](../../node/internal/api/autonomy_v2_api.go#L21) |
| `POST` | `/v1/agents/{agent_id}/tool-calls/{tool_call_id}/cancel` | `handleAgentToolCallCancel` | [tool_call_control_api.go#L12](../../node/internal/api/tool_call_control_api.go#L12) |
| `GET` | `/v1/agents/{agent_id}/tool-jobs` | `handleAgentToolJobs` | [tool_call_control_api.go#L13](../../node/internal/api/tool_call_control_api.go#L13) |
| `POST` | `/v1/agents/{agent_id}/turns/{turn_id}/steps/{step_id}/tool-executions/{execution_id}/reconcile` | `handleAgentReconcileToolExecution` | [agents_api.go#L43](../../node/internal/api/agents_api.go#L43) |
| `GET` | `/v1/agents/{agent_id}/workspace` | `handleWorkspaceList` | [agents_api.go#L29](../../node/internal/api/agents_api.go#L29) |
| `GET` | `/v1/agents/{agent_id}/workspace/file` | `handleWorkspaceGet` | [agents_api.go#L30](../../node/internal/api/agents_api.go#L30) |
| `PUT` | `/v1/agents/{agent_id}/workspace/file` | `handleWorkspacePut` | [agents_api.go#L31](../../node/internal/api/agents_api.go#L31) |
| `POST` | `/v1/agents/{agent_id}/workspace/upload` | `handleWorkspaceUpload` | [agents_api.go#L32](../../node/internal/api/agents_api.go#L32) |
| `POST` | `/v1/messages` | `handlePostMessage` | [server_routes.go#L25](../../node/internal/api/server_routes.go#L25) |
| `GET` | `/v1/streams` | `handleStreams` | [server_routes.go#L26](../../node/internal/api/server_routes.go#L26) |

### 自动化、trigger 与 skills

| Method | Path | Handler | Source |
|---|---|---|---|
| `GET` | `/v1/auto/overview` | `handleAutoOverview` | [server_routes.go#L15](../../node/internal/api/server_routes.go#L15) |
| `GET` | `/v1/skills/catalog` | `handleNodeSkillsCatalog` | [server_routes.go#L33](../../node/internal/api/server_routes.go#L33) |
| `GET` | `/v1/triggers` | `handleListTriggers` | [triggers_api.go#L15](../../node/internal/api/triggers_api.go#L15) |
| `POST` | `/v1/triggers` | `handleCreateTrigger` | [triggers_api.go#L14](../../node/internal/api/triggers_api.go#L14) |
| `DELETE` | `/v1/triggers/{trigger_id}` | `handleDeleteTrigger` | [triggers_api.go#L18](../../node/internal/api/triggers_api.go#L18) |
| `GET` | `/v1/triggers/{trigger_id}` | `handleGetTrigger` | [triggers_api.go#L16](../../node/internal/api/triggers_api.go#L16) |
| `PATCH` | `/v1/triggers/{trigger_id}` | `handleUpdateTrigger` | [triggers_api.go#L17](../../node/internal/api/triggers_api.go#L17) |
| `POST` | `/v1/triggers/{trigger_id}/fire` | `handleFireTrigger` | [triggers_api.go#L19](../../node/internal/api/triggers_api.go#L19) |
| `GET` | `/v1/triggers/{trigger_id}/history` | `handleTriggerHistory` | [triggers_api.go#L20](../../node/internal/api/triggers_api.go#L20) |
| `POST` | `/v1/triggers/{trigger_id}/recover` | `handleRecoverTrigger` | [triggers_api.go#L21](../../node/internal/api/triggers_api.go#L21) |

### MCP 与 Linux channels

| Method | Path | Handler | Source |
|---|---|---|---|
| `GET` | `/v1/linux/channels` | `handleListLinuxChannels` | [linux_channels_api.go#L53](../../node/internal/api/linux_channels_api.go#L53) |
| `POST` | `/v1/linux/channels` | `handleCreateLinuxChannel` | [linux_channels_api.go#L54](../../node/internal/api/linux_channels_api.go#L54) |
| `DELETE` | `/v1/linux/channels/{channel_id}` | `handleDeleteLinuxChannel` | [linux_channels_api.go#L56](../../node/internal/api/linux_channels_api.go#L56) |
| `PATCH` | `/v1/linux/channels/{channel_id}` | `handlePatchLinuxChannel` | [linux_channels_api.go#L55](../../node/internal/api/linux_channels_api.go#L55) |
| `POST` | `/v1/linux/channels/{channel_id}/test` | `handleTestLinuxChannel` | [linux_channels_api.go#L57](../../node/internal/api/linux_channels_api.go#L57) |
| `GET` | `/v1/linux/credentials` | `handleListLinuxCredentials` | [linux_channels_api.go#L58](../../node/internal/api/linux_channels_api.go#L58) |
| `POST` | `/v1/linux/credentials` | `handleCreateLinuxCredential` | [linux_channels_api.go#L59](../../node/internal/api/linux_channels_api.go#L59) |
| `DELETE` | `/v1/linux/credentials/{credential_id}` | `handleDeleteLinuxCredential` | [linux_channels_api.go#L61](../../node/internal/api/linux_channels_api.go#L61) |
| `PATCH` | `/v1/linux/credentials/{credential_id}` | `handlePatchLinuxCredential` | [linux_channels_api.go#L60](../../node/internal/api/linux_channels_api.go#L60) |
| `GET` | `/v1/mcp/config` | `handleGetMCPConfig` | [mcp_api.go#L32](../../node/internal/api/mcp_api.go#L32) |
| `PUT` | `/v1/mcp/config` | `handlePutMCPConfig` | [mcp_api.go#L33](../../node/internal/api/mcp_api.go#L33) |
| `GET` | `/v1/mcp/servers` | `handleListMCPServers` | [mcp_api.go#L34](../../node/internal/api/mcp_api.go#L34) |
| `POST` | `/v1/mcp/servers` | `handleCreateMCPServer` | [mcp_api.go#L36](../../node/internal/api/mcp_api.go#L36) |
| `DELETE` | `/v1/mcp/servers/{server_id}` | `handleDeleteMCPServer` | [mcp_api.go#L38](../../node/internal/api/mcp_api.go#L38) |
| `PATCH` | `/v1/mcp/servers/{server_id}` | `handlePatchMCPServer` | [mcp_api.go#L37](../../node/internal/api/mcp_api.go#L37) |
| `POST` | `/v1/mcp/servers/{server_id}/refresh` | `handleRefreshMCPServer` | [mcp_api.go#L40](../../node/internal/api/mcp_api.go#L40) |
| `POST` | `/v1/mcp/servers/{server_id}/test` | `handleTestMCPServer` | [mcp_api.go#L39](../../node/internal/api/mcp_api.go#L39) |
| `GET` | `/v1/mcp/servers/{server_id}/tools` | `handleMCPServerTools` | [mcp_api.go#L41](../../node/internal/api/mcp_api.go#L41) |
| `GET` | `/v1/mcp/status` | `handleMCPStatus` | [mcp_api.go#L35](../../node/internal/api/mcp_api.go#L35) |

`GET /v1/mcp/config` 返回可编辑配置视图，其中字面量环境变量和请求头值会替换为 `__DAGENTS_SECRET_REDACTED__`。`PUT` 仅可用该标记保留同一服务、同一键已有的值；若没有对应旧值则拒绝保存。提交新值会轮换密钥，删除键或服务会真正删除旧值。配置视图包含 `enabled` 与 `enabled_tools`，因此 GET 后原样 PUT 会保留服务停用状态及工具允许列表。此处只遮罩 API 编辑视图；本地运行时格式化仍保持原值。

### 文件传输、feedback 与 Manage upload

| Method | Path | Handler | Source |
|---|---|---|---|
| `GET` | `/v1/feedback` | `handleFeedbackList` | [feedback_api.go#L54](../../node/internal/api/feedback_api.go#L54) |
| `POST` | `/v1/feedback` | `handleFeedbackCreate` | [feedback_api.go#L52](../../node/internal/api/feedback_api.go#L52) |
| `GET` | `/v1/feedback/target` | `handleFeedbackTarget` | [feedback_api.go#L53](../../node/internal/api/feedback_api.go#L53) |
| `GET` | `/v1/feedback/{client_feedback_id}` | `handleFeedbackGet` | [feedback_api.go#L55](../../node/internal/api/feedback_api.go#L55) |
| `POST` | `/v1/feedback/{client_feedback_id}/sync` | `handleFeedbackSync` | [feedback_api.go#L56](../../node/internal/api/feedback_api.go#L56) |
| `POST` | `/v1/manage/upload/externaltool` | `handleManageUploadExternalTool` | [manage_upload.go#L40](../../node/internal/api/manage_upload.go#L40) |
| `POST` | `/v1/manage/upload/plugin` | `handleManageUploadPlugin` | [manage_upload.go#L41](../../node/internal/api/manage_upload.go#L41) |
| `POST` | `/v1/manage/upload/skill` | `handleManageUploadSkill` | [manage_upload.go#L39](../../node/internal/api/manage_upload.go#L39) |
| `GET` | `/v1/transfers` | `handleLinuxTransfers` | [linux_transfer_api.go#L11](../../node/internal/api/linux_transfer_api.go#L11) |
| `GET` | `/v1/transfers/events` | `handleLinuxTransferEvents` | [linux_transfer_api.go#L13](../../node/internal/api/linux_transfer_api.go#L13) |
| `POST` | `/v1/transfers/{transfer_id}/cancel` | `handleLinuxTransferCancel` | [linux_transfer_api.go#L12](../../node/internal/api/linux_transfer_api.go#L12) |

### Manage Workgroups

| Method | Path | Handler | Source |
|---|---|---|---|
| `GET` | `/v1/workgroups` | `handleListWorkgroups` | [workgroup_api.go#L16](../../node/internal/api/workgroup_api.go#L16) |
| `POST` | `/v1/workgroups` | `handleCreateWorkgroup` | [workgroup_api.go#L17](../../node/internal/api/workgroup_api.go#L17) |
| `GET` | `/v1/workgroups/meta/agents` | `handleListRegisteredAgents` | [workgroup_api.go#L18](../../node/internal/api/workgroup_api.go#L18) |
| `GET` | `/v1/workgroups/{workgroupId}` | `handleGetWorkgroup` | [workgroup_api.go#L19](../../node/internal/api/workgroup_api.go#L19) |
| `PATCH` | `/v1/workgroups/{workgroupId}` | `handlePatchWorkgroup` | [workgroup_api.go#L20](../../node/internal/api/workgroup_api.go#L20) |
| `GET` | `/v1/workgroups/{workgroupId}/acl` | `handleGetWorkgroupACL` | [workgroup_api.go#L23](../../node/internal/api/workgroup_api.go#L23) |
| `POST` | `/v1/workgroups/{workgroupId}/collaborators` | `handleAddWorkgroupCollaborator` | [workgroup_api.go#L24](../../node/internal/api/workgroup_api.go#L24) |
| `GET` | `/v1/workgroups/{workgroupId}/events` | `handleWorkgroupEvents` | [workgroup_api.go#L34](../../node/internal/api/workgroup_api.go#L34) |
| `GET` | `/v1/workgroups/{workgroupId}/hitl` | `handleListWorkgroupHITL` | [workgroup_api.go#L29](../../node/internal/api/workgroup_api.go#L29) |
| `POST` | `/v1/workgroups/{workgroupId}/hitl` | `handleCreateWorkgroupHITL` | [workgroup_api.go#L30](../../node/internal/api/workgroup_api.go#L30) |
| `POST` | `/v1/workgroups/{workgroupId}/hitl/{hitlId}/resolve` | `handleResolveWorkgroupHITL` | [workgroup_api.go#L31](../../node/internal/api/workgroup_api.go#L31) |
| `GET` | `/v1/workgroups/{workgroupId}/human-queue` | `handleWorkgroupHumanQueue` | [workgroup_api.go#L38](../../node/internal/api/workgroup_api.go#L38) |
| `DELETE` | `/v1/workgroups/{workgroupId}/human-queue/{queueId}` | `handleDeleteWorkgroupHumanQueue` | [workgroup_api.go#L40](../../node/internal/api/workgroup_api.go#L40) |
| `PATCH` | `/v1/workgroups/{workgroupId}/human-queue/{queueId}` | `handlePatchWorkgroupHumanQueue` | [workgroup_api.go#L39](../../node/internal/api/workgroup_api.go#L39) |
| `POST` | `/v1/workgroups/{workgroupId}/human-queue/{queueId}/send-now` | `handleSendWorkgroupHumanQueueNow` | [workgroup_api.go#L41](../../node/internal/api/workgroup_api.go#L41) |
| `GET` | `/v1/workgroups/{workgroupId}/llm-configs` | `handleListWorkgroupLLMConfigs` | [workgroup_api.go#L22](../../node/internal/api/workgroup_api.go#L22) |
| `GET` | `/v1/workgroups/{workgroupId}/members` | `handleListWorkgroupMembers` | [workgroup_api.go#L25](../../node/internal/api/workgroup_api.go#L25) |
| `POST` | `/v1/workgroups/{workgroupId}/members` | `handleCreateWorkgroupMember` | [workgroup_api.go#L26](../../node/internal/api/workgroup_api.go#L26) |
| `PATCH` | `/v1/workgroups/{workgroupId}/members/{memberId}` | `handlePatchWorkgroupMember` | [workgroup_api.go#L27](../../node/internal/api/workgroup_api.go#L27) |
| `POST` | `/v1/workgroups/{workgroupId}/members/{memberId}/archive` | `handleArchiveWorkgroupMember` | [workgroup_api.go#L28](../../node/internal/api/workgroup_api.go#L28) |
| `POST` | `/v1/workgroups/{workgroupId}/messages` | `handlePostWorkgroupMessage` | [workgroup_api.go#L36](../../node/internal/api/workgroup_api.go#L36) |
| `POST` | `/v1/workgroups/{workgroupId}/messages/stream` | `handlePostWorkgroupMessageStream` | [workgroup_api.go#L37](../../node/internal/api/workgroup_api.go#L37) |
| `POST` | `/v1/workgroups/{workgroupId}/publish` | `handlePublishWorkgroup` | [workgroup_api.go#L21](../../node/internal/api/workgroup_api.go#L21) |
| `GET` | `/v1/workgroups/{workgroupId}/runs` | `handleListWorkgroupRuns` | [workgroup_api.go#L43](../../node/internal/api/workgroup_api.go#L43) |
| `GET` | `/v1/workgroups/{workgroupId}/runs/{runId}/history` | `handleGetWorkgroupRunHistory` | [workgroup_api.go#L44](../../node/internal/api/workgroup_api.go#L44) |
| `DELETE` | `/v1/workgroups/{workgroupId}/subscribe` | `handleUnsubscribeWorkgroup` | [workgroup_api.go#L33](../../node/internal/api/workgroup_api.go#L33) |
| `POST` | `/v1/workgroups/{workgroupId}/subscribe` | `handleSubscribeWorkgroup` | [workgroup_api.go#L32](../../node/internal/api/workgroup_api.go#L32) |
| `GET` | `/v1/workgroups/{workgroupId}/timeline` | `handleWorkgroupTimeline` | [workgroup_api.go#L35](../../node/internal/api/workgroup_api.go#L35) |
| `POST` | `/v1/workgroups/{workgroupId}/turn/cancel` | `handleCancelWorkgroupTurn` | [workgroup_api.go#L42](../../node/internal/api/workgroup_api.go#L42) |

## 移除接口与迁移表

| 移除路由 | 新路由或职责方 |
|---|---|
| `GET /v1/agent/info` | `GET /v1/node/info` |
| `GET /v1/ui/bootstrap` | `GET /v1/bootstrap` |
| `GET/PATCH /v1/setup/config` | `GET/PATCH /v1/settings` |
| `POST /v1/setup/llm/probe-models` | `POST /v1/llm/probe-models` |
| `GET /v1/agent/update` | `GET /v1/node/update` |
| `GET /v1/agent/upgrade-readiness` | `GET /v1/node/upgrade-readiness` |
| `GET /ui`、`GET /ui/` | 移除 Node 静态托管；Vue source 保留给 Desktop 复用。 |
| `/v1/desktop/runtime-config` | Desktop 自有设置 JSON。 |
| `/v1/agent/update/apply`、`/v1/platform/update/apply` | Desktop Shell updater。 |
| `/v1/platform/clipboard/files`、`/v1/platform/ui-focus` | Desktop Shell IPC selector，不是 Node HTTP。 |

`docs/architecture/openapi-node.yaml` 覆盖当前所有注册路由。核心设置/token、Node info/bootstrap、update status 与 package binary 提供关键 schema；其余标注为 surface-only。检查入口：`py -3 scripts/ci/check_contracts.py` 和 `py -3 scripts/ci/sync_openapi_routes.py --check`。
