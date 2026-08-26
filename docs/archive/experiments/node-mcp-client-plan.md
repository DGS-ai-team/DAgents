# Node MCP Client 接入方案

## 1. 结论

给 Node 增加 MCP Client 是可行的，但首期应把它定义为 **Node 本地工具提供器**，而不是把 MCP 当成新的 Agent 类型，也不应让 Manage 直接连接 MCP Server。

Node 已经具备接入所需的几个边界：

- `node/internal/tools.Registry` 统一负责工具定义、allowlist 和执行分发；
- Agent 的配置快照已经由 `defaults.tools` 承载；
- Agent 的审批策略已经按 Agent 持久化；
- Workgroup Worker 已经有独立的成员绑定、工具清单 revision 和 `tool.command` 执行链路；
- 工具调用已经有通用的 `call_purpose` 参数，用于 UI 展示，执行 handler 前会被剥离。

因此 MCP 的最佳接入点是：

```text
MCP Server
   │ stdio / Streamable HTTP
Node MCP Manager
   │ 发现并缓存 tools/list
MCP Registry Adapter
   │ 命名空间工具 + per-agent allowlist + policy
Agent Turn / Workgroup Worker
```

当前实现已覆盖 Node 本地 stdio 与远程 Streamable HTTP；Workgroup 工具转发和
Manage 侧密钥策略仍保持在后续阶段。

## 2. 目标与非目标

### 目标

1. 用户可以在 Node Web UI 配置多个 MCP Server。
2. 每个 Agent 可以独立选择启用哪些 Server、哪些工具以及审批策略。
3. MCP 工具与内置工具共享同一套工具 schema、审批、取消、超时、结果裁剪和审计机制。
4. Server 断线、工具变更和 Agent 配置变更不会破坏当前消息序列。
5. Workgroup 成员只能使用其 Home Node 上、且明确授予该成员的 MCP 工具。

### 首期不做

- 不让 LLM 任意启动本机进程或填写任意远程 URL；
- 不在 system prompt 中注入动态 Server 状态；
- 不首期实现 MCP resources/prompts/sampling；
- 不让 Supervisor 直接拥有某个 Node 的全部 MCP 工具；
- 不把 MCP Server 凭据写入 Agent prompt、Timeline 或普通日志。

## 3. 配置模型

### 3.1 Node 全局 MCP Server

建议在 Node runtime 数据库增加 `mcp_servers`：

| 字段 | 说明 |
|---|---|
| `server_id` | Node 内稳定 ID |
| `display_name` | UI 名称 |
| `transport` | `stdio` 或 `streamable_http` |
| `command` / `args` / `cwd` | stdio 启动信息 |
| `url` | HTTP 端点 |
| `env_refs` | 环境变量名或凭据引用，不保存明文密钥 |
| `header_refs` | HTTP header 到 Node 环境变量名的映射，不保存明文 token |
| `enabled_tools` | 已从 `tools/list` 目录中显式启用的工具名；空数组表示全部禁用 |
| `enabled` | 是否允许被 Agent 绑定 |
| `trust_state` | 未信任、已信任、阻止 |
| `catalog_revision` | tools/list 规范化后的 hash |
| `last_health` | 连接状态、错误摘要、更新时间 |

Server 配置属于 Node，不属于 Manage。这样 MCP 连接的本机文件、进程和网络权限不会被远程 Workgroup 编排层越权扩大。

### 3.2 per-agent MCP 绑定

建议不要把完整 MCP Server 配置复制进 Agent 快照，而是在 Agent 配置中保存声明式绑定：

```json
{
  "mcp": {
    "bindings": [
      {
        "server_id": "github-local",
        "enabled": true,
        "tool_allowlist": ["search_repositories", "get_issue"],
        "approval_mode": "always"
      }
    ]
  }
}
```

推荐实现为“配置快照 + 规范化运行时表”：

- Agent 快照保存用户期望配置，便于创建、复制和版本化；
- `agent_mcp_bindings` 保存校验后的绑定和生效 revision，便于查询、并发更新和回收；
- Server 连接、工具目录、健康状态属于 Node 全局运行时，不进入 Agent prompt。

有效工具集计算为：

```text
effective_tools(agent) = builtin_tools(agent) ∪
  (service.enabled_tools ∩ binding.tool_allowlist)
```

服务级目录与 Agent 级绑定是两层独立开关。Node 仍保存完整的远端工具
目录用于展示和后续启用，但默认 fail-closed；服务未启用的工具不会进入
Agent 的 schema、权限选择、注册表或实际调用路径。

但每一项都必须经过 deny-by-default、Server enabled、工具 allowlist 和 Agent policy 四层检查。

### 3.3 工具命名

MCP 工具不能直接使用远端原始名称，否则会和内置工具或其他 Server 冲突。建议使用稳定命名空间：

```text
mcp__<server_id>__<tool_name>
```

例如：

```text
mcp__github_local__search_repositories
```

UI 可显示 Server 名和原始工具名，但模型 schema、审批记录、工具调用 ID 和审计记录都使用完整稳定名称。

## 4. Node 运行时设计

新增 `node/internal/mcp`，分为四层：

### 4.1 协议层

- JSON-RPC 2.0 request/response/notification；
- `initialize` / capability negotiation；
- `tools/list` 分页、超时和 revision hash；
- `tools/call` 参数透传与结果规范化；
- 取消请求、连接关闭和请求 ID 映射；
- stdio framing 与 Streamable HTTP；
- SSE 仅作为兼容 transport，不作为新的内部抽象。

### 4.2 Server 生命周期层

`MCPManager` 负责：

- 按 Server ID 管理连接池；
- lazy connect：只有 Agent 实际启用 Server 时才连接；
- 单 Server 并发上限、连接超时、调用超时、空闲回收；
- 进程异常退出后的有限次数重启和退避；
- 配置变更后的旧连接 drain；
- 健康状态、最后错误和目录 revision 的持久化。

stdio Server 必须由 Node 进程监督，不能由模型直接传入 command。HTTP Server 必须经过 URL、协议、重定向和私网访问策略校验，避免 SSRF。

### 4.3 Registry Adapter

MCP Server 的 `tools/list` 结果转换成现有 Registry 的 Tool Definition：

- 复制并校验 `inputSchema`；
- 加入稳定命名空间；
- 保留远端 description 作为模型说明；
- 自动注入 Node 通用的 `call_purpose`，并放入 required 首位；
- 调用时剥离 `call_purpose` 后再发送给 MCP Server；
- handler 内只接受当前 Agent effective toolset 中的名称；
- 输出统一经过大小限制、媒体处理和敏感信息脱敏。

MCP 工具的进度展示必须使用本次调用参数中的 `call_purpose`。不能根据 MCP 工具名猜测用途，也不能把原始参数放入工作组 Timeline。缺失时只显示通用的“执行外部工具”。

### 4.4 Agent Turn 集成

每次 turn 开始时生成不可变的 `EffectiveToolSnapshot`：

```text
agent_id
binding_revision
server_catalog_revisions
tool_definitions
policy_snapshot
snapshot_digest
```

当前 turn 内不动态替换工具定义。配置或目录变化只影响下一次 turn；如果当前调用正在执行，则按照现有取消/工具结果配对规则完成或返回合法失败结果。这样可以避免模型看到的 assistant tool_call 与后续 tool_result 不匹配。

工具 schema 的顺序和 JSON 序列化必须稳定。只有工具绑定、工具目录或 schema 真正变化时才改变请求工具列表，从而把必要的 Prompt Cache miss 控制在配置变化边界内。

## 5. 前端设计

### 5.1 Node 设置中的 MCP Server

新增“连接器 / MCP”设置页：

- Server 列表：名称、transport、启用状态、健康状态、工具数量、更新时间；
- 新建/编辑：stdio command、args、cwd，或 HTTP URL；
- 环境变量仅配置变量名/凭据引用，不在列表和普通表单回显明文；
- “测试连接”执行 initialize + tools/list；
- 工具目录预览和刷新；
- 停用、删除前显示受影响的 Agent 数量；
- Server trust 确认，尤其是 stdio command 和外部网络连接。

建议 API：

```text
GET    /v1/mcp/servers
POST   /v1/mcp/servers
PATCH  /v1/mcp/servers/{server_id}
DELETE /v1/mcp/servers/{server_id}
POST   /v1/mcp/servers/{server_id}/test
POST   /v1/mcp/servers/{server_id}/refresh
GET    /v1/mcp/servers/{server_id}/tools
```

### 5.2 Agent 设置中的 MCP

在现有 AgentSettingsForm 中增加独立的 MCP 区块：

- 按 Server 勾选启用；
- 展开后按工具勾选；
- 显示工具 description、来源 Server、当前目录状态；
- 每个工具或 Server 选择 `继承 / 总是批准 / 自动允许 / 禁止`；
- 默认不启用远端工具；
- Server 下线、工具被删除时显示 stale binding，不静默替换成同名工具；
- 保存时一次性提交 binding revision，避免局部保存造成生效集不一致。

建议 API：

```text
GET /v1/agents/{agent_id}/mcp
PUT /v1/agents/{agent_id}/mcp
GET /v1/agents/{agent_id}/mcp/effective-tools
```

普通 Agent 工具策略页面应把 MCP 工具显示为 `mcp__...` 的来源分组，但仍调用现有 policy API，避免出现两套审批系统。

## 6. Workgroup 接入边界

### 首期建议：先支持独立 Node Agent

首期 MCP 只在 Node 本地 Agent 生效，Workgroup Supervisor 不直接读取或调用 MCP。这样能先验证协议、权限、审批和断线恢复，不把 MCP 生命周期和 Workgroup provisioning 同时耦合。

### 第二阶段：支持 Workgroup Member

如果成员需要 MCP，授权链路应为：

```text
Manage MemberSpec
  └─ mcp_binding_revision / allowed_mcp_tool_names
        ↓ member.provision
Home Node WorkerBinding
        ↓
member effective MCP catalog
        ↓ tool.command
MCP Manager on Home Node
```

具体规则：

1. Manage 只下发明确的 MCP 工具名称和 binding digest，不下发 Server secret，也不下发任意 command/URL。
2. Home Node 只接受属于自己的 `home_node_id`、lease、generation 和 digest。
3. Worker 为每个成员生成隔离的 effective toolset，不能复用本地主 Agent 的全量 MCP 工具。
4. `tool_catalog_revision` 同时覆盖内置成员工具和 MCP 工具；目录不一致时拒绝执行并要求重新 provision。
5. 成员工具气泡仍只展示 `call_purpose`；MCP 原始工具名、参数和结果只留在成员私有 RunHistory/审计范围。
6. Supervisor 只能看到 assign 结果和成员公开进度，不获得 MCP 凭据或原始工具输出中的未脱敏内容。

## 7. 安全与可靠性

- stdio command 使用显式信任列表；禁止模型动态安装包或修改 Server 配置；
- HTTP 限制 scheme、DNS 重解析、重定向、私网/本机地址和响应大小；
- 凭据只通过环境变量引用、系统凭据存储或 Node secret store 注入；日志统一脱敏；
- Server、工具和 Agent 都 deny-by-default；写操作、网络操作和未知副作用默认需要批准；
- 限制单次调用时间、并发数、输出字节数和返回项数量；
- MCP 进程退出或 HTTP 断线不能自动重放非幂等调用；只能返回明确失败或 indeterminate；
- 每个工具调用保留 server_id、catalog_revision、agent_id、policy decision、request ID 和耗时；
- 工具名称冲突、schema 不合法、返回非 JSON 或 capability 不支持时 fail closed；
- 不支持的 MCP capability 不应伪装成支持，首期只声明 tools 能力。

## 8. 分阶段实施

### P0：可用的本地 MCP 工具

- runtime DB 表和迁移；
- stdio Client、initialize、tools/list、tools/call、超时/取消；
- Registry Adapter 和 `mcp__server__tool` 命名空间；
- Server CRUD、测试连接、工具刷新；
- per-agent Server/工具 allowlist；
- 统一 `call_purpose`、审批、结果裁剪、审计；
- fake MCP Server 单元测试和 Agent 隔离测试。

### P1：生产化连接与 Workgroup Member

- Streamable HTTP、重连、退避、健康检查；
- MCP 工具的细粒度 policy UI；
- member.provision 携带 MCP binding digest；
- Worker per-member MCP toolset、catalog revision 和 fencing；
- Workgroup 成员端到端回归测试。

### P2：远程认证与完整 MCP 能力

- OAuth/凭据轮换；
- resources/prompts；
- Server marketplace/import；
- 更细的副作用分类、租户级审计和管理员策略。

## 9. 必备测试矩阵

1. stdio 握手、tools/list 分页、schema 不合法、重复工具名。
2. 正常调用、超时、取消、进程退出、HTTP 断线和恢复。
3. Agent A 启用工具、Agent B 未启用时的隔离；工具 policy deny/approval/allow。
4. Server 目录变化导致 revision 改变；当前 turn 使用旧快照，下一 turn 使用新快照。
5. `call_purpose` 被正确传给 UI、不会进入 MCP handler 参数、不会进入公开 Timeline 的工具名/参数。
6. 大输出、敏感字段、二进制/媒体结果的裁剪和脱敏。
7. Workgroup Member 的 Home Node、lease、generation、digest 不匹配时拒绝执行。
8. 非幂等调用在断线后不自动重放，Manage 能收到失败或 indeterminate 的合法 tool_result。
