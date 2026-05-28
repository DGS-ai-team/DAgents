# 临时子 Agent 设计

本文定义 architecture-v2 下父 Agent 如何创建短生命周期子 Agent。核心原则是：**临时子 Agent 是受控的短生命周期 Agent Instance，不是父 Agent 内部的线程，也不能获得超过父 Agent 的权限**。

## 1. 定义

```text
Temporary Child Agent
  = Brain Runtime
  + Temporary Brain Profile
  + Body Binding
  + Parent Scope
  + TTL / Budget / Policy
```

临时子 Agent 仍然是 Agent Instance：

- 有自己的 `agent_id`。
- 有自己的 session context。
- 只能绑定一个 Body。
- 由 Backend Control Plane 创建、授权、审计和清理。
- 默认不进入普通 Register Center discover 结果。

## 2. 创建入口

父 Agent 不能直接写数据库创建子 Agent。创建必须通过 Backend tool：

```text
tool.kind = backend
tool.name = create_temporary_agent
```

示例请求：

```json
{
  "purpose": "review generated patch",
  "template_id": "code-review-helper",
  "body_binding": "inherit_parent_body",
  "context_seed": "检查当前 patch 是否引入明显 bug",
  "ttl_seconds": 1800,
  "max_turns": 20,
  "allowed_tools": ["file_read", "grep", "agent_send_message"]
}
```

Control Plane 负责：

- 校验父 Agent 和父 session。
- 校验模板是否允许用于临时子 Agent。
- 收缩权限到父 Agent 权限子集。
- 创建临时 AgentRecord 和 child session。
- 写审计日志。
- 将 child result 返回父 session。

## 3. Body Binding 模式

临时子 Agent 必须遵守“一个 Agent 只绑定一个 Body”的原则。推荐支持三种绑定模式：

| 模式 | 含义 | Phase |
|------|------|-------|
| `backend_only` | 子 Agent 只能使用 Backend tools，不绑定可执行 Body tool | Phase 1 |
| `inherit_parent_body` | 子 Agent 绑定父 Agent 的同一个 Body | Phase 1 |
| `bind_registered_body` | 子 Agent 绑定某个已注册 Body | Phase 2+ |

Phase 1 只支持 `backend_only` 和 `inherit_parent_body`。

`bind_registered_body` 风险更高，因为它可能跨越父 Agent 原本的执行环境边界，必须额外校验 owner、policy、资源范围和审批主体。

## 4. 权限收缩

临时子 Agent 的权限必须是父 Agent 的子集：

```text
child.allowed_tools ⊆ parent.allowed_tools
child.resource_scope ⊆ parent.resource_scope
child.policy_profile 不得弱于 parent.policy_profile
child.ttl <= max_child_agent_ttl
child.max_turns <= max_child_agent_turns
```

如果父 Agent 没有 `file_write`，子 Agent 不能获得 `file_write`。

如果父 Agent 只能访问某个 `fs_root` 或资源范围，子 Agent 不能扩大访问范围。

如果父 Agent 的某个工具需要审批，子 Agent 调用同一工具时也必须遵守相同或更严格策略。

## 5. 可发现性

临时子 Agent 默认不可被普通 A2A discover 发现：

```python
class TemporaryAgentVisibility(BaseModel):
    schedulable: bool = False
    discoverable: bool = False
    parent_agent_id: str
    parent_session_id: str
```

只有以下主体可以寻址临时子 Agent：

- 创建它的父 Agent。
- 创建它的父 session。
- Backend Control Plane。
- 具备显式管理权限的 owner/admin。

如需 A2A 风格通信，也应创建短 TTL 的内部 child session，而不是将临时子 Agent 暴露到普通 RC discover。

## 6. 生命周期

状态机：

```text
creating
  → active
  → completed
  → expired
  → cancelled
  → failed
```

建议记录：

```python
class TemporaryAgentRecord(BaseModel):
    agent_id: str
    parent_agent_id: str
    parent_session_id: str
    body_id: str | None
    template_id: str
    purpose: str
    allowed_tools: list[str]
    ttl_seconds: int
    max_turns: int
    status: Literal["creating", "active", "completed", "expired", "cancelled", "failed"]
    created_at: float
    expires_at: float
```

清理规则：

- TTL 到期自动取消未完成任务。
- 父 session 结束时取消仍在运行的临时子 Agent，除非显式 detached。
- 子 Agent 完成后向父 session 返回 summary、artifacts 和 audit reference。
- 默认只保留摘要和审计，不长期保留完整上下文。

## 7. 父子通信

父 Agent 与子 Agent 之间通过受控 channel 通信，不共享可变 conversation context：

```text
Parent Agent
  → create_temporary_agent
  → send child task
  → child runs own session
  → child returns final summary / artifact
  → parent consumes child_result as tool result
```

子 Agent 的结果必须回到父 session 的 Session Queue：

```text
child_result
  → parent Session Queue
  → parent Brain Runtime
  → parent continues reasoning
```

子 Agent 不能直接修改父 Agent 的 context，也不能直接提交父 Agent 的最终回复。

## 8. Body 执行冲突

如果子 Agent 使用 `inherit_parent_body`，它与父 Agent 共享同一个现实执行环境。因此 Body tool execution 必须继续经过同一套控制：

- `body_id` 级并发限制。
- Body tool policy。
- ProxyConnection presence。
- Body 本地硬约束。
- execution audit。

子 Agent 不能绕过父 Agent 的 Body 执行控制，也不能绕过 Body 本地拒绝规则。

## 9. 模板

临时子 Agent 必须基于 Backend 提供的模板创建：

```python
class TemporaryAgentTemplate(BaseModel):
    template_id: str
    default_brain_profile: BrainProfileRef
    allowed_body_binding_modes: list[Literal["backend_only", "inherit_parent_body", "bind_registered_body"]]
    default_allowed_tools: list[str]
    max_ttl_seconds: int
    max_turns: int
    policy_profile: str
```

示例模板：

| template_id | 用途 | 默认 Body 模式 |
|-------------|------|----------------|
| `code-review-helper` | 只读检查 patch、总结风险 | `backend_only` 或 `inherit_parent_body` |
| `research-helper` | 拆分检索、总结资料 | `backend_only` |
| `ops-check-helper` | 只读检查宿主机状态 | `inherit_parent_body` |

## 10. Phase 1 最小实现

Phase 1 建议只实现：

- `create_temporary_agent` Backend tool。
- `backend_only` 和 `inherit_parent_body` 两种 Body Binding 模式。
- `schedulable=false`、`discoverable=false`。
- 权限必须是父 Agent 子集。
- 默认 TTL 30 分钟，可配置最大值。
- `max_turns` 和 token budget。
- child result 只返回 parent session。
- 创建、取消、完成、过期都写审计。

## 11. 不变量

- 临时子 Agent 不能获得超过父 Agent 的工具、资源或策略权限。
- 临时子 Agent 默认不进入普通 RC discover。
- 临时子 Agent 必须有 TTL、turn budget 或 token budget。
- 子 Agent 输出不能直接写父 Agent context，必须作为 child result 回到父 Session Queue。
- 继承父 Body 的子 Agent 必须经过同一个 Body execution control 和本地硬约束。
- 创建、执行、取消和清理都必须可审计。
