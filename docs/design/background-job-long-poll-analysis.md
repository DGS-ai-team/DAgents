# 后台 Job 长轮询与 LLM 轮询成本分析

> 分支：`feat/tool-context-cost-optimization`（**WS1** 子专题）  
> 父文档：[tool-context-cost-analysis.md](./tool-context-cost-analysis.md)  
> 范围：Go Agent Node（`node/internal/tools`、`node/internal/turn`、`node/internal/session`）

---

## 1. 背景：我们在优化什么

### 1.1 Agent turn 的成本模型

Go Node 的 LLM 调用以 **turn loop** 为单位：每次模型输出 tool_calls → 执行工具 → 将 **assistant + tool** 消息 append 到 `history` → 再次 `StreamChat`。因此：

- **每一次** `background_job_status` 调用，只要模型在上一轮结束后 **主动发起新一轮推理**，就是一次完整 LLM 请求。
- 请求 input 含 **整段 history**（system 出站注入 + messages + tools schema）。上下文较长时，单次 input 可达数万 token。
- 即便 DeepSeek **Prompt Cache** 命中历史前缀（见 [上下文硬盘缓存](https://api-docs.deepseek.com/zh-cn/guides/kv_cache)），**本轮新增的 assistant/tool 消息** 与 **模型续写 completion** 仍计费；多次轮询会 **线性放大** 调用次数与 tail 增量。

本优化 **不修改** cache 机制本身，而是减少 **不必要的 LLM 往返次数**——与 §1 压缩/cache 方案 **正交、可叠加**。

### 1.2 观测现象

生产/联调中常见模式：

1. 模型调用 `bash_run`（同步超时 **自动降级**，或 `run_in_background=true`）→ 获得 `job_id`。
2. 模型 **不等** Node 侧 `async_tool_result` 自动回灌，连续调用 `background_job_status`。
3. 参数通常 **仅有 `job_id`**（schema 中 **无** 等待时长字段）。
4. 每次返回 `status=running` → 模型再次推理 → 再次 status → 直至 `succeeded` 或用户打断。

在 **长会话**（多轮 tool、大段 bash 输出已进 history）下，即使 cache hit 比例高，**5～15 次** 轮询仍会带来可观的 **completion token** 与 **新增 message tail** 成本，并拉长 wall-clock 时间。

### 1.3 优化目标

| 目标 | 说明 |
|------|------|
| **减少 LLM 往返** | 将「N 次瞬时 status + N 次模型步」收敛为 **1 次带等待的 status** 或 **0 次（仅依赖回灌）** |
| **保持兼容** | 不改 tool 名、不破坏 `enabled_groups: bash`、policy 与 Client 展示 |
| **服务端可阻塞** | 在 tool handler 内 long-poll，**不**占用额外 LLM slot |
| **对齐现有模式** | 与 `wait_temporary_agents(timeout_seconds)` 语义一致 |

---

## 2. 当前实现（Go Node）

### 2.1 bash 工具组与 `run_in_background`

`tools.enabled_groups` 中 **`bash`** 组包含三个配套工具（见 [built-in-tools.md](../built-in-tools.md) §0）：

| 工具 | 定义 | 执行特点 |
|------|------|----------|
| **`bash_run`** | `bash_run_tool.go` / `bash_runner.go` | 默认 **同步**；`timeout_seconds` 内未完成 → **自动降级**后台 job；或 `run_in_background=true` 立即后台化 |
| **`background_job_status`** | `tool_job.go` | **瞬时**读 `backgroundJobRegistry` → `statusText()`，**不阻塞** |
| **`background_job_cancel`** | `tool_job.go` | 瞬时取消；杀进程或 `cancelFn` |

此外，`injectRunInBackgroundParam` 为 **fs、skills、triggers、hitl** 等工具注入通用参数 `run_in_background`（`execution_mode.go`）。编排器对 **任意** 工具名均可 `StartBackground`（`tool_router.go` → `invokeTool`）。生产上长耗时任务 **几乎总是 `bash_run`**；job 管理工具 **只** 服务 `job_registry.go` 中的条目。

`background_job_status` / `background_job_cancel` 通过 `IsBackgroundJobTool` **强制同步**（忽略 `run_in_background`）。

### 2.2 异步执行的两条路径

```text
路径 A：显式 run_in_background=true
  Client → POST message → turn loop
  → orchestrator.invokeTool(runInBackground=true)
  → Registry.StartBackground(sessionID, toolName, toolCallID, cleanedArgs)
  → 立即返回 formatBackgroundJobAck（含 job_id）
  → goroutine: Execute(..., WithBackgroundExecution)
  → 完成: bgJobs.notifyDone → server.go SetBackgroundJobNotifier
  → Manager.EnqueueAsyncToolResult（PriorityAsyncCompletion）
  → runtime.handleAsyncToolResult → HandleAsyncToolResult → 可选 RunToolMessageTurn

路径 B：bash_run 同步超时自动降级
  execBashRun → runShellSyncWithAutoDegrade
  → timer 到期: formatShellRunningResult（含 job_id），进程继续
  → startShellOutputCollector 在后台 Wait + 写 job 终态
  → autoDegraded=true 时 notifyDone → 同路径 A 回灌
```

**共同点**：完成后 **无需模型轮询** 即可通过 `async_tool_result` 触发新一轮 turn（`buildAsyncToolMessages` 写 assistant+tool 或 user+assistant+tool，见 `tool_result_messages.go`）。

### 2.3 `background_job_status` 现网语义

```go
// tool_job.go — 简化
func (r *Registry) execBackgroundJobStatus(_ context.Context, raw json.RawMessage) (string, error) {
    job, ok := r.bgJobs.get(args.JobID)
    return job.statusText(), nil  // 持锁读快照后立即返回
}
```

`statusText()`（`job_registry.go`）输出：

- `[BACKGROUND_JOB_STATUS] job_id=… tool_name=… status=running|succeeded|failed|cancelled`
- `started_at_unix_ms` / `finished_at_unix_ms`
- 可选 `cwd`、`timeout_seconds`、`degraded_from_sync_timeout`
- 终态时 `RESULT_PREVIEW`（`clipText` 2000 字符）

**无** `wait_seconds`、**无** 阻塞、**无** 「建议下次 wait 多久」字段。

### 2.4 引导轮询的文案来源

| 位置 | 文案要点 |
|------|----------|
| `formatBackgroundJobAck` | 「可用 background_job_status / background_job_cancel 查询或取消」 |
| `formatShellRunningResult` | 「命令超过同步等待时间，已自动降级…可用 background_job_status 查询」 |
| `background_job_status` schema | 「查询…状态与输出摘要」（未提自动回灌、未提 wait） |

模型从 tool 结果学到的 **默认策略** 是 **主动轮询**，而非 **等待 async 回灌**。

### 2.5 自动回灌链路（已存在）

```text
api/server.go:
  tools.SetBackgroundJobNotifier(func(sessionID, done BackgroundJobDone) {
    mgr.EnqueueAsyncToolResult(sessionID, AsyncToolResultPayload{...})
  })

session/runtime.go:
  RequestTypeAsyncToolResult → handleAsyncToolResult
  → orch.HandleAsyncToolResult → append history + SSE tool_callback/tool_result
  → 若 tail 允许 → RunToolMessageTurn（继续 tool 回合）

queue.PriorityAsyncCompletion — 高于 human，便于尽快续跑
```

**Issue #25**：若 session 存在 pending HITL，`async_tool_result` 仍写 history，但 **恢复** saved pending，避免打断审批。

回灌 user 消息示例（`buildAsyncToolMessages`）：

```text
role=user, name=async_tool:
  「工具bash_run，job_id已完成，请获取执行结果并继续任务。」
→ synthetic assistant tool_call (tool_callback)
→ role=tool 含完整执行结果（package 后最多 ~12k 字符）
```

理论上模型 **可以** 在收到回灌后继续任务 **而不再调用 status**；实践中模型仍常 **提前轮询**。

### 2.6 对照：`wait_temporary_agents`

子 Agent 工具组已提供 **阻塞等待** 模式（`tool_childagent.go`）：

| 工具 | 等待参数 | 行为 |
|------|----------|------|
| `create_temporary_agent` | `wait: true` | 创建后阻塞至子 session 终态 |
| `wait_temporary_agents` | `timeout_seconds`（**0 = 立即快照**） | 服务端等待子 Agent，超时返回当前状态 |
| `temporary_agent_status` | 无 | 非阻塞快照（与 background_job_status 类似） |

**缺口**：后台 bash job **没有** 与 `wait_temporary_agents.timeout_seconds` 对称的 server-side wait。

---

## 3. 轮询成本分解

### 3.1 单次「轮询 turn」的消息增量

假设模型在第 k 步发起 status 查询，history 已有 ~H tokens：

```text
Step k   : assistant(tool_calls: background_job_status{job_id})
Step k+1 : tool(statusText → running)
Step k+2 : assistant(决定再查 / 做别的事 / 向用户汇报)
```

每轮至少追加 **2 条 message**（assistant + tool），often **+1 条 assistant 续写**。若 job 仍 running 且模型选择 **立即再查**，进入下一循环。

**与 cache 的关系**：

- `messages[0:H]` 前缀若与上一轮一致 → provider 侧 **可能** hit cache。
- **Step k 的 assistant + tool** 是 **新 tail** → **miss**。
- 模型 **completion**（决定下一步）全程 **新 token**。

因此：**cache 降低的是重复前缀 input 单价，不消除轮询次数 × completion 成本。**

### 3.2 N 次轮询的粗略量级

设：

- 压缩前 history 体量 `H ≈ 60k～100k` tokens（与 silent/blocking 阈值同量级时常见）
- 每次 status turn 新增 tail `Δ ≈ 200～800` tokens（tool_call 参数 + status 文本 + 短 assistant）
- 每次 completion `C ≈ 50～300` tokens
- 轮询次数 `N ≈ 5～15`（30s sync 超时 + 60～120s 任务，模型每 5～10s 查一次）

**input 侧（含 cache）**：

| 场景 | 粗算 |
|------|------|
| 无 cache | `N × (H + Δ)` — 灾难级 |
| 高 cache hit（~90% 前缀） | 每轮仍付 `~0.1H + Δ` miss + 全量 tail 变长后 hit 区略缩 |

**output 侧**：

- `N × C` — **与 cache 无关**，线性增长。

**结论**：在长上下文会话中，**减少 N** 比 **提高 cache hit** 对「轮询型」浪费更直接（两者叠加时收益更大）。

### 3.3 理想路径 vs 现网路径

| 路径 | LLM 调用次数（job 完成前） | 说明 |
|------|---------------------------|------|
| **A. 仅 async 回灌** | **0**（完成前） | 模型提交 bash 后 **不再推理**，等 `async_tool_result` 触发 1 次续跑 |
| **B. 1 次 long-poll status** | **1**（含 wait 阻塞） | `wait_seconds ≥ 剩余耗时` → 单次 tool 返回终态 |
| **C. 现网：N 次 snapshot status** | **N** | 每次 running → 模型再推理 |

目标：把 **C → A 或 B**。

### 3.4 与 bash 同步窗口的关系

| 参数 | 所属工具 | 作用 |
|------|----------|------|
| `bash_run.timeout_seconds` | bash_run | **同步**等待上限；超时 **降级** job，**不杀进程** |
| （拟）`background_job_status.wait_seconds` | background_job_status | **查询**时在服务端阻塞上限；与 bash 进程 **独立** |

二者 **不可互相替代**：sync 超时决定「何时把控制权还给模型并给出 job_id」；status wait 决定「模型拿到 job_id 后是否在一次 tool 调用内等到终态」。

---

## 4. 量化直觉（典型场景）

设 bash 任务真实耗时 `T_job = 90s`，sync `timeout_seconds = 30`：

```text
t=0     bash_run 同步开始
t=30    降级 → 返回 job_id + RUNNING（路径 B）
t=30~   模型开始轮询 status（路径 C）
        每 5～15s 一次 LLM turn，直到 t=90 job 完成
        ≈ 4～12 次 LLM 调用 × 长上下文

t=90    async_tool_result 回灌（路径 A 终局本也会发生）
        若已轮询过，history 中额外多了 8～24 条 assistant/tool 消息
```

**若采用 `wait_seconds=120` 单次 status（路径 B）**：

```text
t=30    模型调用 background_job_status(job_id, wait_seconds=120)
t=30~90 handler 阻塞在 job.done
t=90    同一 tool 返回 succeeded + RESULT_PREVIEW
        LLM 额外往返：1 次（而非 4～12 次）
```

**若模型零轮询（路径 A）**：

```text
t=30    模型结束 turn（不再 call status）
t=90    async_tool_result → 1 次 RunToolMessageTurn
        LLM 额外往返：0 次（完成前）
```

| 策略 | 完成前 LLM 往返（量级） | 对 history 污染 |
|------|-------------------------|-----------------|
| 现网 C | 4～12 | 高（重复 status assistant/tool） |
| B long-poll | 1 | 低 |
| A 仅回灌 | 0 | 最低 |

---

## 5. 优选方案：`background_job_status.wait_seconds` 长轮询

本节为 **首选实现方案**（待开发）。

### 5.1 核心思路

1. **L1（P0）**：为 `background_job_status` 增加可选 **`wait_seconds`**；`running` 时在服务端 `select(job.done, timer, ctx.Done())`。
2. **L2（P0）**：修订 ACK / 降级 / status **description**，明确 **async_tool_result 自动回灌为主路径**；若主动查询 **应带 wait_seconds**。
3. **L3（P1）**：`tools.background_job_status_max_wait_seconds` 配置 cap（默认 120），防止单次 tool 占用 worker 过久。

**不采用**（理由简述）：

| 方案 | 不选原因 |
|------|----------|
| 合并为 `background_job_wait` 新工具 | 破坏 policy / enabled_groups / 已有 prompt 记忆 |
| orchestrator 层「无 LLM 自动 sleep」 | 模型不可控，仍可能空转 |
| 删除 `background_job_status` | 取消/异常/无回灌场景仍需快照查询 |
| WebSocket 推 status（无 LLM） | 需 Client 协议改造，超出本阶段范围 |

### 5.2 参数语义（与 `wait_temporary_agents` 对齐）

```yaml
# background_job_status 参数（拟）
job_id: string          # 必填
wait_seconds: integer   # 可选，默认 0
```

| `wait_seconds` | 行为 |
|----------------|------|
| **省略或 0** | **现网兼容**：瞬时 `statusText()` |
| **> 0** | 若 `status=running`，阻塞至多 `min(wait_seconds, max_wait)`；完成 → 返回终态 + preview；超时 → 仍 `running` + **`waited_seconds=N`** + 提示 |
| **已完成/失败/取消** | 忽略 wait，立即返回（与 0 相同） |

**响应增补字段（拟）**：

```text
waited_seconds=37
wait_timed_out=true|false
hint=「任务仍在运行；可增大 wait_seconds 或等待 async_tool_result 自动回灌」
```

### 5.3 Handler 伪代码

```go
func (r *Registry) execBackgroundJobStatus(ctx context.Context, raw json.RawMessage) (string, error) {
    args := parseBackgroundJobStatusArgs(raw) // job_id, wait_seconds
    job, ok := r.bgJobs.get(args.JobID)
    if !ok { return ERROR not found }

    wait := clampWait(args.WaitSeconds, r.bgJobStatusMaxWait)

    if wait > 0 && job.isRunning() {
        deadline := time.After(time.Duration(wait) * time.Second)
        select {
        case <-job.done:
        case <-deadline:
        case <-ctx.Done():
            return "", ctx.Err()
        }
    }
    return job.statusTextWithWaitMeta(waited), nil
}
```

**并发安全**：`job.done` 在 `StartBackground` 与 auto-degrade collector 中 `close`；wait 期间持 **读锁** 或 **不持锁仅等 channel**（需避免与 `cancelJob` 死锁——现网 `cancelJob` 持 `job.mu`，应优先 `RLock` + 读 status 或 wait 前释放锁仅监听 `done`）。

### 5.4 Schema / 文案变更（L2）

**`background_job_status` description（拟）**：

```text
查询 bash_run 后台任务状态。任务完成后会通过 async_tool_result 自动回灌，通常无需轮询。
若需主动等待，请设置 wait_seconds（建议≈剩余耗时）；0 表示立即返回当前快照。
```

**`formatBackgroundJobAck` / `formatShellRunningResult`（拟）**：

```text
任务完成后将自动回灌结果（async_tool_result），通常无需调用 background_job_status。
若需主动查询，请使用 wait_seconds 一次等待，避免反复轮询。
```

**`bash_run` tail 文案**：可缩短对 status 的强调，避免与 L2 冲突。

### 5.5 配置

```yaml
# packaging/agent-client/config.example.yaml（拟）
tools:
  background_job_status_max_wait_seconds: 120  # 单次 wait_seconds 上限
```

`shared/config/config.go` → `ToolsConfig` 校验：`>= 0`，默认 120；`0` 表示 **不允许** long-poll（仅 snapshot）或仍 cap 为 0——实现时二选一并文档化（**推荐默认 120**）。

### 5.6 与 async 回灌的协作

```text
                    ┌─────────────────────────────────────┐
                    │  推荐：模型 bash_run 后结束 turn     │
                    │  → 0 次 status                       │
                    └─────────────────┬───────────────────┘
                                      │ job 完成
                                      ▼
                    async_tool_result → RunToolMessageTurn

                    ┌─────────────────────────────────────┐
                    │  可接受：1 次 status(wait_seconds)   │
                    └─────────────────┬───────────────────┘
                                      │ 阻塞至完成或超时
                                      ▼
                    tool 返回终态 → 1 次 LLM 步继续

                    ┌─────────────────────────────────────┐
                    │  反模式（现网）：N 次 status(0)      │
                    └─────────────────────────────────────┘
```

**不改动** `async_tool_result` 优先级与 `HandleAsyncToolResult` 逻辑；long-poll 仅减少 **回灌前** 的无效 LLM 步。

### 5.7 可行性评估

| 维度 | 结论 | 说明 |
|------|------|------|
| 工程可实现性 | **高** | 改动集中 `tool_job.go` + `job_registry.go` + config + 文案 |
| Token 收益 | **中高** | 取决于模型是否配合 wait；L2 文案 + 默认示例可提升配合率 |
| 服务端风险 | **中低** | 单请求阻塞 ≤ max_wait；需与 HTTP 超时、turn cancel 协调 |
| Client 感知 | **低** | tool 执行时间变长，但 SSE 仍显示「工具执行中」 |
| 子 Agent 一致性 | **高** | 与 `wait_temporary_agents` 模式统一 |

---

## 6. 其他优化方向（补充）

### P0 — 与 §5 正交或叠加

#### 6.1 强化「零轮询」回灌路径

- system / tool schema 中声明：**长任务 bash_run 完成后由 runtime 推送结果**。
- TUI 展示 `[TOOL_BACKGROUND]` 时提示用户「后台执行中，完成后自动继续」——减少模型 **向用户解释轮询** 的 assistant 废话。

#### 6.2 回灌时 **dedupe** 已终态 job 的 status tool_calls

- 若 history 最近已有同一 `job_id` 的 succeeded status，async 回灌 **跳过** 重复 synthetic messages（侵入大，**P2**）。

### P1 — 度量

#### 6.3 结构化日志 / metrics

- 计数：`background_job_status` 调用次数 / session / job_id；`wait_seconds` 分布；`wait_timed_out` 比例。
- 对比：同一任务 **polling turns** vs **async-only** 路径。

### P2 — 架构级

#### 6.4 Orchestrator「defer LLM until job terminal」

- session 记录 in-flight job_ids；turn 空闲且无 user 消息时 **不** 调 LLM——仅当回灌或 cancel。**侵入大**，且难处理模型并行 tool_calls。

#### 6.5 合并 status + cancel 为交互式 HITL

- 过度设计，不优先。

---

## 7. 建议实施路线

| 阶段 | 内容 | 状态 |
|------|------|------|
| **M1 长轮询** | `wait_seconds` + `max_wait` config + handler 阻塞 + 单测 | 未开始 |
| **M2 文案** | ACK / 降级 / schema description；bash_run tail 调整 | 未开始 |
| **M3 度量** | 日志字段 `bg_job_status_wait_ms`、timeout 率；可选 TUI 提示 | 未开始 |
| **M4（可选）** | 压测：长 session 下轮询次数 before/after | 未开始 |

### 7.1 实施步骤（对照 compression §10）

#### 步骤 1：配置与参数类型 — M1

**文件**：`shared/config/config.go`、`packaging/agent-client/config.example.yaml`

- 新增 `ToolsConfig.BackgroundJobStatusMaxWaitSeconds`（默认 120）。
- `Registry` 构造或 `SetBackgroundJobStatusMaxWait` 注入。

**验收**：config 单测；默认 YAML 有说明。

#### 步骤 2：`execBackgroundJobStatus` 长轮询 — M1

**文件**：`node/internal/tools/tool_job.go`、`job_registry.go`

- `backgroundJobStatusArgs{ JobID, WaitSeconds *int }`。
- 实现 §5.3；`statusTextWithWaitMeta`。
- 并发：wait on `job.done` 不持 `job.mu` 写锁。

**验收**：`job_registry_test.go` — `sleep 2` job + `wait_seconds=5` 一次 succeeded；`wait=0` 仍瞬时 running。

#### 步骤 3：Schema 与 ACK 文案 — M2

**文件**：`tool_job.go`、`job_registry.go`（`formatBackgroundJobAck`）、`bash_runner.go`（`formatShellRunningResult`）

- 更新 description 与 ACK（§5.4）。

**验收**：`bash_run_tool_test` / snapshot 测试 description 含「async_tool_result」「wait_seconds」。

#### 步骤 4：文档 — M2

**文件**：本稿、`major-changes.md` §2、`node/internal/tools/README.md`、`REFERENCE.md`

**验收**：major-changes 状态改为「M1 已落地」等。

#### 步骤 5：度量 — M3

**文件**：`tool_job.go` 或 registry logger

- 记录 wait 耗时、终态、timeout。

---

## 8. 风险与边界

| 风险 | 缓解 |
|------|------|
| HTTP / reverse proxy 超时 < wait | 文档建议 max_wait ≤ 120；生产 nginx `proxy_read_timeout` |
| turn `ctx` 取消（用户 Esc） | `select` 监听 `ctx.Done()`，返回 cancelled 或 partial status |
| 模型仍不传 wait_seconds | L2 文案；可选 future：省略 wait 时 **默认** short wait（**破坏兼容**，不首选） |
| 多 job 并行 | 每个 job_id 独立 wait；不引入 batch status |
| auto-degrade job 无 `StartBackground` session ctx | 已有 `sessionIDFromContext`；降级路径已 `put(job)` + notifyDone |

**不解决**：

- bash 输出过大导致的 **单次** tool result token（由 `bash_compress` / package 截断负责）。
- 模型在 job 运行中 **并行** 发起其他 LLM  intensive 操作（属 agent 策略问题）。

---

## 9. 相关代码索引

| 主题 | 路径 |
|------|------|
| bash 执行 / 降级 | `node/internal/tools/bash_runner.go` |
| bash schema | `node/internal/tools/bash_run_tool.go` |
| job 注册 / ACK | `node/internal/tools/job_registry.go` |
| status / cancel 工具 | `node/internal/tools/tool_job.go` |
| run_in_background | `node/internal/tools/execution_mode.go` |
| 编排 dispatch | `node/internal/turn/tool_router.go` → `invokeTool` |
| async 回灌 | `node/internal/api/server.go` → `SetBackgroundJobNotifier` |
| 回灌 turn | `node/internal/session/runtime.go` → `handleAsyncToolResult` |
| history 形态 | `node/internal/turn/tool_result_messages.go` |
| 子 Agent wait 对照 | `node/internal/tools/tool_childagent.go` |
| 工具组 | `docs/built-in-tools.md` §0 |
| Prompt cache（正交） | [context-compression-cache-analysis.md](./context-compression-cache-analysis.md) |

---

## 10. 结论

**现网矛盾**：Node 已实现 **`async_tool_result` 自动回灌**，但 `background_job_status` 为 **瞬时 snapshot**，加上 ACK 文案引导，导致模型在长会话中对 running job **高频轮询**，产生 **与 Prompt Cache 正交的多次 LLM 往返成本**。

**首选方案（§5）**：为 `background_job_status` 增加 **`wait_seconds` 服务端长轮询**，并修订 schema/ACK **明确回灌为主、wait 为辅**，预期将典型 **N 次轮询** 收敛为 **0～1 次** LLM 步。

**与 compression/cache 的关系**：压缩优化降低 **单次** 侧车/主 turn 前缀 miss；本优化降低 **job 等待阶段** 的 **turn 次数**。二者应 **同时** 在长会话工具密集场景下评估收益。
