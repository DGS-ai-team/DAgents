# 动态路由、反射与模板资源审计

日期：2026-10-27

## 结论

本轮没有发现可直接删除的生产动态路由、反射调用或模板资源。发现并修复了 Desktop 路由名失配，以及一条已不可达的旧输入栏实现；Manage 工作组对话页面已从控制台移除，服务端编排 API 暂时保留，供 Desktop → Node → Manage 链路使用。

## 动态路由

| 范围 | 实现与调用方 | 结论 |
| --- | --- | --- |
| Desktop 页面路由 | `desktop/tray-tauri/src/settings-router.js`。`/chat` 是由 `DesktopApp.vue` 管理聊天视图的路由哨兵；设置页按需加载，唯一带 path 参数的设置路由为 `/settings/agents/:agentId`，未知路径重定向到聊天。 | `/chat` 原先挂了一个空渲染组件；移除组件并保留命名路由。设置页“返回对话”与帮助命令插入都改为使用该有效路由。回归测试验证 `chat`、Agent 列表和 Agent 详情路由可解析。 |
| Node HTTP API | 参数化路由集中注册于 `node/internal/api/agents_api.go`、`agent_policy_api.go`、`agent_templates_api.go`、`autonomy_v2_api.go`、`dreaming_scheduler.go`、`linux_channels_api.go`、`linux_transfer_api.go`、`media_api.go`、`mcp_api.go`、`terminal_api.go`、`triggers_api.go`、`tool_call_control_api.go` 与 `workgroup_api.go`；总装入口为 `node/internal/api/server_routes.go`。 | 路径参数是 Agent、工具执行、终端、触发器、制品等资源标识，由 Desktop Node API client 或 Node→Manage client 使用。未发现未注册/未消费的参数化路由；不可只因本地 UI 未直接调用就删除公开 API。 |
| Manage REST API | 路径参数分布在 `manage/llm/routes.py`、`registry/routes.py`、`workgroup/routes.py`、`cases/routes.py`、`skills/routes.py`、`externaltools/routes.py`、`plugins/routes.py`、`releases/routes.py`、`platform/blob_routes.py` 与 `auto_employees/routes.py`；均由 `manage/manage_app.py` 装配。工作组 WebSocket 使用固定 `/v1/workgroups/ws`，通过身份头绑定 Node。 | 资源 ID、版本、平台等都是接口资源定位的一部分。工作组消息/时间线/运行记录 API 在 Node 的 `internal/manage/workgroup_client.go` 和 Desktop Node API client 中仍有调用，因此保留后端接口；本轮只删除 Manage 控制台对话 UI。 |
| Manage Console 导航 | `manage/console/frontend/src/App.vue` 以 view state 切换管理页面，不使用 URL 动态路由。 | 没有动态路径可清理。 |

此前 `SettingsLayout.vue`、`AboutSettings.vue` 与已废弃的 `ChatComposer.vue` 使用了未注册的 `name: "agents"`。前两处现在导航到具名 `/chat`；旧组件原本还会尝试带 `agentId` 跳到这个不存在的路由。`ChatComposer.vue` 在唯一的 `MainChatPanel.vue` 调用点被 `hide-composer` 永久隐藏，Desktop 已有自己的输入栏，因此移除了该 646 行组件、对应 props/emits/草稿转发，以及不再有 DOM 使用的 47 条 `.chat__composer*` 样式规则。

## 反射与动态属性访问

生产 Go 代码只有两处 `reflect.DeepEqual`：

- `node/internal/mcp/manager.go`：比较归一化 MCP 配置，判断是否需要更新，属于有效配置变更检测。
- `node/internal/turn/hook_host.go`：比较 hook 来源与 provenance，判断运行时状态是否改变，属于有效状态比较。

`node/internal/session/agent_policy_refresh_test.go` 使用 `reflect.ValueOf`、`FieldByName` 和 `unsafe` 读取私有 policy 字段，仅用于验证 session 运行时策略刷新；它不是生产反射调用或死代码，但测试与内部字段名耦合，后续可考虑通过可观察行为或测试接口降低脆弱性。其他 Go 反射命中也只在测试中。

Manage 中的 `getattr` / `hasattr` 没有发现按字符串动态调用任意业务方法或动态导入模块的情况，当前用途是可选属性与协议兼容：

- `manage/workgroup/routes.py`、`turn_kernel.py`、`ws_hub.py`：处理 Pydantic/dataclass/事件对象的可选字段、`model_dump`/`close`/`stream_chat` 能力，以及队列 claim token。
- `manage/workgroup/native_tools.py`：允许注册表记录缺省 host 元数据。
- `manage/platform/auth.py`、`auth_routes.py`：读取由应用装配注入的状态字段。
- `manage/llm/probe.py`：兼容不同 HTTP response 对象的 status 属性。

这些分支都处于运行调用链，暂不应按“动态访问”一概删去。扫描未发现生产 `importlib`、`__import__` 或按名字反射调用的路由分发器。

## 模板与静态资源

| 资源 | 加载路径 | 结论 |
| --- | --- | --- |
| `node/internal/agenttemplate/builtin/{general,code-reviewer,ops-runner}.yaml` | `agenttemplate.Loader.List/Get` 读取嵌入文件；Agent API 用于列出、读取和创建模板。 | 三个嵌入文件与 `packaging/agent-templates/` 的发布副本逐个 SHA-256 相同，重复是为安装包和无工作目录运行提供保障，不是死资源。源文件双份仍有漂移风险，可增加同步校验，但不应删副本。 |
| `manage/workgroup/schemas/assign_workgroup_task.openai.json` | `manage/workgroup/native_tools.py:load_assign_workgroup_task_tool` 在构造 supervisor 工具时读取。 | 运行时加载，属于活跃工具 schema。 |
| Manage `frontend/index.html`、`frontend/product.html` | Vite 构建到 `manage/console/static`；`manage/manage_app.py` 挂载 `/console/`，并从 `/`、`/download/` 返回产品下载页。 | 活跃静态入口，不是未引用模板。Manage 不使用服务端 HTML 模板引擎。 |
| `node/internal/webui/static/` | 当前工作区有本地生成的旧 WebUI 文件，但没有 Go `embed` 声明或生产处理器引用；`server_test.go` 明确断言 `/ui/` 返回 404。 | 这是被忽略的本地构建残留，不属于当前 Node 发布资源。不要据此恢复 Node WebUI；也不在本轮清理用户机器上的 ignored 产物。旧文档/归档中的 `/ui/` 叙述按历史材料保留。 |

## 已执行的清理与验证

- Manage Console 移除独立工作组聊天页面、入口、聊天专用 API client 和聊天样式；工作组管理页只保留创建、成员/ACL/模型配置、发布和归档。
- Node/Manage 工作组后端编排和消息 API 继续保留，因 Desktop 经 Node 使用这些服务。
- 移除不可达的 Desktop `ChatComposer.vue` 旧实现与无 DOM 消费者的专属样式；同步更新当前设计索引中的源文件路径。
- `desktop/tray-tauri`：前端测试 77 个文件 / 467 项通过，Lint 和 Vite build 通过。构建保留既有 Tauri 动态/静态导入及大 chunk 警告。
- `manage/console/frontend`：Vite build 通过；第三方 Page Agent 包仍报告 `eval` 警告。

## 仍建议单独处理的事项

1. 为三份 Agent 模板副本添加自动同步测试，避免发布目录与嵌入副本后续漂移。
2. 逐步把 policy refresh 测试从 `unsafe` 私有字段检查改为公开行为验证；这是可维护性改进，不是运行时风险。
3. 忽略目录中的 `node_modules` 与旧 `node/internal/webui/static` 仅是本机产物；清理前应确认没有其他本地进程依赖它们。本审计没有删除这些 ignored 文件。
