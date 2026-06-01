# 归属、组织与多租户

本文补充 architecture-v2 中散落在各文档的 **owner、组织、权限与多租户** 概念。Phase 1 可只做最小 owner 校验；Phase 3 面向生产多团队隔离。

> **待修订（2026-05）**：下文仍含 Brain/Body、Proxy bootstrap 等旧术语；AC 阶段以 Node 本地 profile 为准，见 [three-component-model.md](./three-component-model.md)。

## 1. 核心实体

```text
Organization（组织）
  └── User / ServiceAccount（用户或服务身份）
        └── owns → Agent Instance
        └── UserProfile（user.md 等偏好）

Agent Instance
  └── BrainProfile（persona / soul.md）
  └── Body Binding（body_id）
  └── Tool Manifest + policy_profile

Body
  └── 通常对应一台宿主机 + 一个执行身份（服务账号）
  └── bootstrap_token 绑定 Organization 或 Agent 创建权限
```

## 2. Agent 归属

| 字段 | 说明 |
|------|------|
| `owner_id` | 用户或服务账号 ID |
| `organization_id` | 所属组织 |
| `schedulable` | 是否参与 RC 普通 discover |
| `discovery_group` | RC 分组；与 trust domain 配合 |

约束：

- Proxy **不能**自行设定 `owner_id` 或 `schedulable`；由 Backend 在 Agent 创建/绑定时写入。
- `PATCH /v1/agents/{id}` 仅 owner 或 org admin。
- A2A 调用 **不**转移 owner；caller 仅获得 callee 的受控 session。

## 3. 权限矩阵（目标态）

| 操作 | User（owner） | Org Admin | Proxy | A2A Caller |
|------|---------------|-----------|-------|------------|
| 创建 session | ✓（自己的 Agent） | ✓ | ✗ | ✓（受控 A2A session） |
| 批准 body 执行 | ✓ | ✓（可配置） | ✗ | ✗（须 callee 侧审批） |
| 修改 Brain Profile | ✓ | ✓ | ✗ | ✗ |
| 更新 Body Manifest | ✗ | ✗ | ✓（校验 body token） | ✗ |
| RC discover | 公开组 | 公开组 | ✗ | 依 trust domain |
| 创建临时子 Agent | 父 Agent 权限内 | 同左 | ✗ | ✗ |

Phase 1：可实现「单 owner + bootstrap token」；矩阵完整落地在 Phase 3。

## 4. User Profile 与 Organization Profile

见 [three-component-model.md](./three-component-model.md) 与 Node 侧 profile 配置（`soul.md` / `user.md`，待 `agent-node-internals.md` 详述）：

- `user.md` → `user_profile_id` 或 `organization_profile_id`
- `soul.md` → `brain_profile.profile_id`（通常与 `agent_id` 1:1）
- 多 Agent 可共享同一 User Profile；多 User 可有不同 persona Agent

## 5. Trust Domain 与 A2A

| 概念 | 说明 |
|------|------|
| `discovery_group` | RC 可见范围 |
| A2A token | 绑定 caller 身份与允许的 target groups |
| 跨 org A2A | 默认拒绝；显式 federation 配置 |

目标 Agent 的 body 写操作：**caller 不能代替 callee owner 审批**（见 [client-events-and-hitl.md](./client-events-and-hitl.md) §4.4）。

## 6. 多租户隔离（Phase 3）

| 层 | 隔离手段 |
|----|----------|
| 数据 | `organization_id` 分区；Agent/session 查询强制过滤 |
| 网络 | Gateway 路由 + 可选 dedicated Backend 池 |
| 执行 | Body token 绑定 org；Proxy 不能注册到其他 org 的 Agent |
| 配额 | 每 org：LLM token、并发 session、Body 数量、execution 速率 |
| 审计 | 审计记录带 `organization_id`；集中存储按 org 保留策略 |

## 7. Bootstrap Token

Proxy 首次 `POST /v1/bodies/register` 携带 bootstrap token。Backend 校验：

- token 是否有效、未吊销；
- 允许的 `requested_template_id` 列表；
- 是否允许 `desired_agent_id` 或必须由 Backend 分配；
- 创建的 Agent 默认 `schedulable: false` 除非 token 授权。

## 8. Phase 1 最小实现

- AgentRecord 增加 `owner_id`（可选，默认单用户部署）。
- bootstrap token 静态配置（文件或 env）。
- 审批主体：当前 connection 对应用户 = owner 时允许批准。
- 临时子 Agent：权限收缩到父 Agent，inherit parent owner。

## 9. 相关文档

- [three-component-model.md](./three-component-model.md) — 三组件与 ADR
- [security-and-policy.md](./security-and-policy.md) — 策略与审批
- [agent-node-api.md](../architecture/agent-node-api.md) — Node HTTP 鉴权演进
