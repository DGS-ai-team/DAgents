# 安全与策略模型

`tool.kind == "body"` 的工具允许 LLM 驱动绑定 Body 执行命令和文件操作，必须默认按高风险能力设计。v2 的安全目标是：**所有 Body 执行都可授权、可审批、可审计、可限制、可撤销**。

## 1. 安全边界

```text
User / A2A caller
  → Backend authentication
  → connection/session authorization
  → Brain Layer tool decision
  → Control Plane policy decision
  → approval if required
  → Body local hard constraints
  → execution
  → audit record
```

任何一层拒绝，执行都不能继续。

## 2. 策略决策

策略层输入：

- `agent_id`、`body_id` 与 `tool.kind`。
- `session_id`、`connection_id`、调用来源。
- Brain Profile、Body policy profile、Tool policy profile、工具名与参数。
- 是否来自用户会话、A2A 调用或自动任务。
- Body host_info、resources、environment 与能力标签。
- 历史风险信号，例如失败率、频繁审批拒绝、异常时间段。

策略层输出：

| decision | 含义 |
|----------|------|
| `auto` | 允许自动执行 |
| `require_approval` | 暂停执行，等待授权主体审批 |
| `deny` | 拒绝执行 |

策略结果必须生成 `policy_decision_id`，并与后续 `execution_id` 和审计记录关联。

## 3. 默认策略建议

| 工具或行为 | 默认决策 | 说明 |
|------------|----------|------|
| 只读文件读取，限于 `fs_root` | `auto` 或 `require_approval` | 取决于目录敏感级别 |
| 文件写入 | `require_approval` | 需要展示目标路径和内容摘要 |
| shell 只读命令 | `auto` 或 `require_approval` | 例如 `ls`、`kubectl get` 可按 Agent/Body 配置自动执行 |
| shell 变更命令 | `require_approval` | 例如 `kubectl apply`、`systemctl restart` |
| 删除、格式化、权限修改 | `require_approval` 或 `deny` | 默认不自动执行 |
| 网络扫描、压力测试 | `deny` | 除非明确授权的安全测试环境 |
| 读取密钥、token、私钥 | `deny` | 需要专门 break-glass 流程 |
| A2A 触发的远程写操作 | `require_approval` | 不能默认信任其他 Agent |

## 4. 审批模型

审批请求应包含：

- 调用来源：用户、A2A caller、session。
- 目标 Agent、Body 和 Proxy 宿主机。
- 工具名、参数、工作目录、超时。
- 策略命中的规则。
- 风险摘要。
- 对文件写入展示 diff 或内容摘要。
- 对 shell 展示完整命令和环境限制。

审批结果：

```json
{
  "approval_id": "appr-...",
  "decision": "approved",
  "approved_by": "user-or-owner-id",
  "scope": "single_execution",
  "expires_at": 1760000000
}
```

审批 scope 默认只对单次 execution 生效。批量授权或一段时间内授权必须显式配置，并进入审计。

## 5. 授权主体

不同来源需要不同审批人：

| 场景 | 审批主体 |
|------|----------|
| 用户自己的 Body tool execution | 当前用户或该 Agent owner |
| A2A 调用 Body tool execution | 目标 Agent owner 或目标 session 用户 |
| 生产组 schedulable Agent | 生产组授权人 |
| 高风险命令 | 更高权限审批人或 deny |

Backend 必须能判断审批人是否有权批准目标 Agent + Body 的执行请求。

## 6. Body 本地硬约束

策略层在 Backend，但 Body 仍必须执行本地硬约束，防止 Backend 配置错误或 token 泄露导致无限制执行。

承载 Body tool execution 的 Body 必须支持：

- `fs_root` 文件边界。
- 禁止 `..`、symlink escape、Windows drive escape、UNC path escape。
- 命令允许列表或拒绝列表。
- 最大执行时间。
- 最大 stdout/stderr 输出。
- 最大并发和队列长度。
- 环境变量白名单。
- 可选网络访问限制。
- 进程树取消和清理。

Backend 策略允许不代表 Body 必须执行；Body 可以因为本地硬约束拒绝任务。

## 7. Shell 执行约束

远程 shell 是最高风险能力。建议按阶段收敛：

1. Phase 1 允许字符串 shell，但必须审批和审计。
2. Phase 2 引入结构化命令描述，例如 `program + args + cwd + env`。
3. Phase 3 对高价值工具提供专用 tool schema，减少裸 shell。

命令展示给审批人时必须是最终执行形态，不能隐藏插值、环境变量或工作目录。

## 8. 文件访问约束

文件操作必须满足：

- 所有路径 canonicalize 后位于 `fs_root` 内。
- 拒绝 symlink 指向 `fs_root` 外部。
- Windows 下拒绝 drive prefix 和 UNC path 绕过。
- 文件写入需要大小限制。
- 二进制文件读取默认拒绝或只返回摘要。
- 敏感文件模式可配置拒绝，例如 `*.key`、`.env`、`id_rsa`。

## 9. 审计日志

每次策略判定、审批和执行都要写审计。

最小审计字段：

```python
class AuditRecord(BaseModel):
    audit_id: str
    event_type: Literal["policy_decision", "approval", "execution_started", "execution_finished"]
    agent_id: str
    body_id: str | None
    session_id: str | None
    connection_id: str | None
    execution_id: str | None
    policy_decision_id: str | None
    actor: str
    tool_name: str | None
    target_host: str | None
    decision: str | None
    command_or_path_redacted: str | None
    result_summary: str | None
    created_at: float
```

审计日志应避免完整保存敏感 stdout/stderr，但要保留足够信息支持追责和排障。

## 10. Token 与认证

- Proxy 注册使用 Agent/Body 级 token。
- A2A 请求使用现有 `x-dagents-a2a-token`，并绑定 discovery group 或 trust domain。
- 用户入口使用 Gateway 或 Backend 认证。
- Proxy token 应支持轮换和吊销。
- 生产环境建议启用 mTLS 或等价双向认证。

## 11. 取消与撤销

系统必须支持：

- 用户取消当前 execution。
- 审批超时后自动拒绝。
- Proxy token 吊销后拒绝新任务并断开旧 control channel。
- Agent owner 将 Agent 设置为 `schedulable: false` 后停止被 A2A discover。
- Backend draining 时不再接收新 Proxy control channel。

## 12. 安全不变量

- 未经策略判定的远程执行不能发生。
- `require_approval` 未批准前不能下发到 Proxy。
- `deny` 结果不能被普通用户覆盖。
- A2A 调用不能绕过目标 Agent owner 的策略。
- Body 本地硬约束优先于 Backend 允许结果。
- 所有远程执行都有审计记录。
