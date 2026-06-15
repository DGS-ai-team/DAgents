# UX 专题：Agent 自有文件写操作审批信任链

**状态（2026-06）**：**已落地**（Go Node `tool.before_each` / `tool.after_each`）。  
**前置条件**：`write_file` / `search_replace` 策略须为 **`rule`**；`always` 为强制全审，信任链不生效。
**专题边界**：降低 **HITL 审批摩擦**，**不**减少 tool 结果写入 history 的体积（后者见 [tool-context-cost-analysis.md](./tool-context-cost-analysis.md) WS3）。

---

## 1. 动机

| 现状 | 痛点 |
|------|------|
| `write_file` / `search_replace` 默认 **`rule`**（`packaging/runtime/policy/tool.approval.txt`）；`always` 可强制全审 | 模型对**同一文件连续编辑**时，每轮都要用户点确认（未命中信任链时） |
| 首次 `write_file` 创建新文件 | 审批合理（用户需知晓新文件落盘） |
| 后续在同文件上 `write_file`（覆盖）或 `search_replace` | 若文件**未被外界改动**，重复审批信息密度低 |

**目标**：在**不削弱**「外界篡改文件须重新审批」的前提下，对 **Agent 自建且未被外部触碰** 的文件，后续写操作**当轮免审批**。

**明确不做**（本专题）：

- path 级 HITL 策略配置 UI
- 将 `write_file` 全局改为 `never`
- 与 [tool-context-cost-analysis.md](./tool-context-cost-analysis.md) §3.2.2 的 **read 去重 / short_circuit**（已否决）

---

## 2. 方案概要

### 2.1 信任链语义

```text
首次 write_file 创建新文件（经用户审批）→ 标记 path 为 agentOwned，记录 lastAgentWriteMtime
后续 write_file / search_replace 同一 path：
  before_each：Stat.mtime == lastAgentWriteMtime → ActionAuto（本轮免审批）
  执行成功：更新 lastAgentWriteMtime 为新 mtime
  Stat.mtime 不一致 → 视为外界改动，恢复 require_approval
```

| 条件 | 审批 |
|------|------|
| 非 `write_file` / `search_replace` | 不变 |
| path 未标记 `agentOwned` | `always` 审批（含覆盖仓库既有文件） |
| `agentOwned` 且 mtime 一致 | **免审批** |
| `agentOwned` 且 mtime 不一致 | **须审批** |
| 首次创建（`before_each` 时 `ENOENT`） | **须审批** |

### 2.2 与 fs 编码缓存（`pathEncCache`）的关系

| 维度 | `pathEncCache`（WS3 §3.2.2） | 本专题信任表 |
|------|------------------------------|--------------|
| 目的 | 减少错编码乱码重读 | 减少重复写操作审批 |
| 作用域 | `tools.Registry` **进程级** | **per-session**（必须） |
| 更新时机 | read / write / grep 等 | **仅** write_file / search_replace **成功**后 |
| 关键字段 | `Encoding` + `Mtime` | `agentOwned` + `lastAgentWriteMtime` |

**结论**：复用 **mtime 比对**与 **`cachePathKey(relPath)`** 规范化即可；**不合并**进 `pathEncCache` 结构体（语义与作用域不同）。

### 2.3 实现落点（建议）

| 组件 | 职责 |
|------|------|
| `hooks.AgentOwnedFileHook`（新） | `tool.before_each`：在 `PolicyToolHook` 之后，可将 `always` 降为 `auto` |
| session 级 `AgentFileTrust` | `map[pathKey]agentFileTrustEntry`，绑定 Orchestrator（同 `ToolExecutionLog`） |
| `tool.after_each` 或执行成功回调 | 写成功后更新 `Owned` / `lastAgentWriteMtime` |
| `Registry.StatRelPath`（可选） | 供 hook 在 `FSRoot` 沙箱内 `Stat` |

Hook 链顺序：

```text
PolicyToolHook（write_file/search_replace=rule → fallback require_approval）
  → AgentOwnedFileHook（仅 ModeRule；信任命中 → auto）
  → DuplicateToolCallHook（仅 rule+auto；跳过 write_file/search_replace）
  → … 执行 …
  → AgentOwnedFileAfterHook（写成功 → 更新信任表）
```

策略文件默认 `write_file=rule`；若需禁止信任绕过，改回 `write_file=always`。

### 2.4 边界与风险

| 场景 | 行为 |
|------|------|
| `touch` / git checkout / 用户手改 | mtime 变 → 恢复审批（保守） |
| 跨 session 同 path | 信任表随 session 销毁 → 重新审批（安全） |
| 同轮并行两个写同一 path | 第二个 `before_each` 可能在第一个写完前执行 → 需考虑 per-path 串行或「已批准待执行」标记 |
| `search_replace` 改仓库既有文件 | 无 `agentOwned` → 始终审批 |
| 子 Agent session | 建议默认 **不继承** 父 session 信任表 |

---

## 3. 配置（可选，后续）

首版可 **硬编码启用**（无配置项）。若需开关：

```yaml
hooks:
  agent_owned_file_trust:
    enabled: true
    tools:
      - write_file
      - search_replace
```

---

## 4. 验收

| # | 场景 | 期望 |
|---|------|------|
| 1 | 新建文件首次 `write_file` | 须审批；成功后标记 `agentOwned` |
| 2 | 同 session 第二次 `search_replace` 同 path，mtime 未变 | 免审批 |
| 3 | 外界修改文件后 `write_file` | 须审批 |
| 4 | 覆盖既有仓库文件（非 Agent 创建） | 每次须审批 |
| 5 | 新 session 编辑上一 session 创建的文件 | 须审批 |

---

## 5. 相关文档

| 文档 | 关系 |
|------|------|
| [tool-context-cost-analysis.md](./tool-context-cost-analysis.md) | 正交：管 history token，不管 HITL 次数 |
| [tool-before-hook-duplicate-approval.md](./tool-before-hook-duplicate-approval.md) | 同 `tool.before_each` 链；duplicate 仅 `rule`+auto，且跳过写工具 |
| [future/security-and-policy.md](../future/security-and-policy.md) | 写盘策略总原则 |

---

## 6. 代码索引

| 路径 | 说明 |
|------|------|
| `node/internal/hooks/agent_file_trust.go` | session 级信任表 |
| `node/internal/hooks/builtin_agent_owned_file.go` | `tool.before_each` 信任降级 |
| `node/internal/hooks/builtin_agent_owned_file_after.go` | `tool.after_each` 写成功后更新 |
| `node/internal/hooks/registry.go` | Hook 链注册 |
| `node/internal/tools/fs_stat.go` | `StatRelPath` 沙箱内 Stat |
| `node/internal/turn/orchestrator.go` | 绑定信任表与 PathStater |
| `packaging/runtime/policy/tool.approval.txt` | `write_file=rule` |
