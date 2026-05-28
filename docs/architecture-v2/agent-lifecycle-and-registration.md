# Agent 创建、注册与同步流程

本文定义 architecture-v2 下 Agent Instance、Body 和 Go Proxy 的创建与同步关系。核心原则是：**Proxy 连接 Backend 不等于自动创建可调度 Agent；Agent 是 Backend 侧受控资源，Body/Proxy 只能提交注册请求和能力声明**。

## 1. 核心原则

```text
Backend 是 AgentRecord、Brain Profile、Tool Manifest 和策略配置的权威来源。
Proxy / Body 是宿主机能力与本地硬约束的权威来源。
Agent 创建必须经过 Backend 鉴权、模板初始化和策略校验。
```

因此不建议采用“Proxy 一连上 Backend 就隐式创建 Agent”的模型。推荐采用显式注册流程：Proxy 先注册 Body，再由 Backend 绑定已有 Agent 或按模板创建新 Agent。

## 2. 推荐创建流程

```text
Go Proxy 启动
  → 读取本地 Body 配置、bootstrap token、本地硬约束
  → 扫描 host_info、resources、capabilities、local tool availability
  → POST /v1/bodies/register
      { bootstrap_token, desired_agent_id?, body_manifest, requested_template_id? }
  → Backend 校验 token、owner、组织、策略
  → Backend 选择：
      ├── 绑定到已有 Agent Instance
      ├── 基于 AgentTemplate 创建新 Agent Instance
      └── 拒绝或进入 pending_approval
  → 返回 agent_id、body_id、proxy_connection_id、accepted_manifest_version
  → Proxy 建立 outbound control channel
  → Backend 将 Agent 标记为 online / schedulable
```

`desired_agent_id` 只是请求，不是事实。最终 `agent_id` 和 `body_id` 必须由 Backend 接受并返回。

## 3. AgentTemplate

Backend 可以提供内建 Agent 模板，用于初始化新 Agent：

```python
class AgentTemplate(BaseModel):
    template_id: str
    name: str
    default_brain_profile: BrainProfileRef
    default_tools: list[ToolRef]
    default_policy_profile: str
    schedulable_default: bool = False
    required_body_capabilities: list[str]
```

模板用于解决两个问题：

- 新 Body 首次接入时，不需要 Proxy 自己生成 Brain Profile、`soul.md` 或策略。
- 组织可以控制哪些默认 Agent 能被创建，以及创建后是否自动可调度。

Phase 1 可以提供少量内建模板：

| template_id | 用途 | 默认状态 |
|-------------|------|----------|
| `personal-terminal-agent` | 用户自己的终端/工作站 Body | `schedulable: false` |
| `ops-terminal-agent` | 运维宿主机 Body | `pending_approval` |
| `backend-helper-agent` | 主要使用 Backend 工具的辅助 Agent | `schedulable: false` |

## 4. Agent 创建状态机

```text
requested
  → pending_approval
  → active
  → draining
  → disabled
  → deleted
```

| 状态 | 含义 |
|------|------|
| `requested` | Proxy/用户提交创建请求，Backend 尚未接受 |
| `pending_approval` | 需要 owner 或组织管理员批准 |
| `active` | Agent 可创建 session，Body tool 可执行 |
| `draining` | 不接收新 session 或新 execution，等待现有任务结束 |
| `disabled` | Agent 保留配置但不可调度、不可执行 |
| `deleted` | Agent 逻辑删除或彻底移除 |

默认情况下，自动创建的 Agent 不应直接 `schedulable: true`，除非 bootstrap token 明确授权该模板自动激活。

## 5. Proxy 可以修改什么

Proxy 可以上报和更新 Body Manifest：

- `host_info`
- `capabilities`
- `resources` 摘要
- `environment` 摘要
- 本地硬约束摘要，例如 `fs_root`、命令 allow/deny、最大并发
- Body tool availability，例如 `shell_exec`、`file_read`、`kubectl_exec`

Proxy 不能直接修改：

- `soul.md` / Brain Profile
- `user.md` / User 或 Organization Profile
- 全局策略规则
- `schedulable`
- Agent owner
- Backend tool 实现

这些修改必须通过 Backend API，由有权限的用户、owner 或管理员执行。

## 6. 配置版本与同步

Agent、Body 和 Tool Manifest 都应带版本号：

```python
class AgentConfigVersion(BaseModel):
    agent_id: str
    agent_version: int
    brain_profile_version: int
    body_manifest_version: int
    tool_manifest_version: int
    policy_version: int
    updated_at: float
```

同步规则：

1. Proxy 上报 `body_manifest_version` 和本地扫描摘要。
2. Backend 比较版本，接受后生成新的权威 manifest version。
3. Backend 返回 accepted version 给 Proxy。
4. Proxy control channel 心跳携带当前版本。
5. 如果 Backend 检测到 Proxy 版本过旧，可下发 reload/reconcile 指令。
6. 如果 Proxy 本地硬约束比 Backend 更严格，以 Proxy 本地拒绝为准。

## 7. 修改 Agent 的流程

用户或管理员修改 Agent：

```text
PATCH /v1/agents/{agent_id}
  → 校验 owner / admin 权限
  → 修改 Brain Profile、Tool Manifest、policy 或 schedulable
  → 生成新版本
  → 写入审计
  → 如果影响 Body tool，下发 reconcile 到 Proxy
```

Proxy 修改 Body 能力：

```text
POST /v1/bodies/{body_id}/manifest
  → 校验 proxy_connection_id 和 token
  → Backend 校验该 Body 是否属于 agent_id
  → 更新 Body Manifest version
  → 重新计算 Agent capability summary
  → 写入审计
```

## 8. 冲突处理

- Backend 是 Agent 配置的最终事实来源。
- Proxy 是本地环境事实来源，但只能上报摘要和硬约束。
- 同时修改时使用版本号做乐观并发控制。
- 版本冲突返回 `409 conflict`，调用方必须重新读取最新版本再提交。
- 影响安全边界的修改必须写审计，必要时进入 `pending_approval`。

## 9. 不变量

- Proxy 连接不能自动绕过审批创建可调度 Agent。
- `agent_id`、`body_id`、`proxy_connection_id` 必须由 Backend 接受或生成。
- `soul.md`、`user.md`、policy 不由 Proxy 直接改写。
- Body Manifest 可以被 Proxy 更新，但必须经过 Backend 校验和版本化。
- Agent 创建、激活、禁用、删除和关键配置修改必须写审计。
