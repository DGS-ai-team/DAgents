# 工具链上下文成本优化 — 完整分析

> 分支：`feat/tool-context-cost-optimization`  
> 范围：Go Agent Node 全内置工具（`node/internal/tools`、`node/internal/turn` 工具结果写回、编排 dispatch）  
> 与 [context-compression-cache-analysis.md](./context-compression-cache-analysis.md) 的关系：**压缩降 history 体量**；本专题降 **tool loop 内无效 LLM 往返、tool 结果膨胀、schema 前缀扰动** — 二者 **正交、应叠加**。

---

## 1. 背景：我们在优化什么

### 1.1 两类上下文成本

| 类型 | 机制 | 本专题是否覆盖 |
|------|------|----------------|
| **A. History 体量** | messages 累积；超阈值触发压缩 | 部分（tool 结果写入 history 的体积） |
| **B. Turn 次数 × 前缀 replay** | 每次 `StreamChat` 重放 system + **tools schema** + 全量 messages；cache 只减重复 **input 单价**，不减 **轮数 × completion** | **主战场** |

用户观测到的痛点（2026-06）：

- 长会话 + 多轮 tool 后，**单次任务**仍可能触发 **十数次** LLM 调用。
- 部分调用「信息量极低」（如 `background_job_status` 返回 `running`、重复 `read_file`、子 Agent 快照查询）。
- 即使 Prompt Cache **高 hit**，**assistant/tool 新增 tail** 与 **completion** 仍线性累积。

### 1.2 Agent turn 的成本模型（轮询场景）

Go Node 的 LLM 调用以 **turn loop** 为单位：每次模型输出 tool_calls → 执行工具 → 将 **assistant + tool** 消息 append 到 `history` → 再次 `StreamChat`。

- **每一次** 低信息密度 tool 调用（如瞬时 `background_job_status`），只要模型 **主动发起新一轮推理**，就是一次完整 LLM 请求。
- 请求 input 含 **整段 history** + tools schema；上下文较长时单次 input 可达数万 token。
- 即便 [Prompt Cache](https://api-docs.deepseek.com/zh-cn/guides/kv_cache) 命中历史前缀，**本轮新增 assistant/tool** 与 **completion** 仍计费；多次轮询 **线性放大** 成本。

### 1.3 优化总目标

```text
在不大改 Agent 能力边界的前提下：
  1. 减少「低信息密度」的 LLM 往返（轮询、重复 read、不必要的 status）
  2. 控制写入 history 的 tool 结果体积（与 bash_compress / package 策略一致）
  3. 稳定 tools schema 前缀（减少 enrich 漂移导致的 cache 断档）
  4. 可度量：每任务 tool_turns、tool_result_chars、status_poll_count
```

---

## 2. 现网工具链与成本触点

### 2.1 内置工具分组（`tools.enabled_groups`）

见 [built-in-tools.md](../built-in-tools.md) §0。

| 组 | 工具 | 主要成本触点 |
|----|------|----------------|
| **fs** | read/write/glob/grep/search_replace | 大段文件进 **tool message**；grep 分页仍可能 bulky；重复 read |
| **bash** | bash_run, background_job_status/cancel | **长输出**（bash_compress）；**N 次 status 轮询**；async 回灌 vs 轮询 |
| **hitl** | ask_user_information | 额外 user 回合（必要成本） |
| **skills** | load/unload/clear | **load_skills description enrich** 随 catalog 变 → **tools 前缀变** |
| **triggers** | trigger_* | CRUD 结果 + condition 说明长 schema |
| **a2a** | agent_invoke, agent_discover | 阻塞等待对端；结果文本进 history |
| **child_agents** | create/wait/status/cancel | **wait_temporary_agents** 已有 `timeout_seconds`；**status 仍瞬时** |

### 2.2 跨工具通用机制

| 机制 | 文件 | 对上下文的影响 |
|------|------|----------------|
| **`run_in_background`** | `execution_mode.go` | 任意注入工具可后台化；完成走 **async_tool_result** |
| **`call_purpose`** | 各 tool schema | 每 call 必填，略增 arguments 体积（UI 价值高，保留） |
| **`Definitions()` 每步发送** | `orchestrator.runOneStep` | 全量 tools JSON 计入 **续写前缀** |
| **`enrichDefinitions`** | `registry_enrich.go` | `load_skills` 附加 skills 元数据 → catalog 变则 **prefix miss** |
| **`packageToolResult`** | `turn/tool_result_messages.go` | 同步 tool 结果 **middle-clip 12k 字符** 进 history |
| **bash_compress** | `bash_compress.go` | bash 输出清洗截断；stats 进 SSE |
| **async 回灌** | `job_registry` + `HandleAsyncToolResult` | 完成时 **+2～3 条** message，但避免完成前 N 次轮询 |

### 2.3 已对齐 vs 未对齐的「等待」模式

| 场景 | 阻塞等待 API | 瞬时 snapshot API | 自动 push |
|------|-------------|-------------------|-----------|
| 后台 bash job | — | `background_job_status` | **async_tool_result** ✅ |
| 临时子 Agent | `wait_temporary_agents(timeout_seconds)`、`create(wait=true)` | `temporary_agent_status` | 父 session 回调 |
| A2A invoke | `agent_invoke` 内 HTTP 等待 | — | — |
| fs / triggers 等 `run_in_background` | — | 同 bash job 表 | async_tool_result ✅ |

**缺口**：snapshot 类 status 工具 **无统一 `wait_seconds`**；模型默认 **轮询**。

---

## 3. 成本来源分解（全工具）

### 3.1 轮询型（P0 — WS1）

**典型**：`background_job_status` 无 wait → 每 5～15s 一次 LLM turn（**§5 详述**）。

**同类模式**：`temporary_agent_status`；模型对 async job 的不信任而重复 status。

**策略**：统一 **long-poll 参数** + **强调 auto-push 主路径**。

### 3.2 Tool 结果膨胀（P1 — WS3）

| 来源 | 现网缓解 | 待优化 |
|------|----------|--------|
| bash stdout/stderr | `bash_compress` + 12k package | 跨工具统一 budget |
| read_file / grep | offset/limit | 模型仍整文件多次 read |
| search_replace diff | 输出 diff 摘要 | 大 diff 仍 long |
| agent_invoke result | 对端全文 | 截断策略 |

### 3.3 Tools schema 前缀（P2 — WS4）

- 每步 `StreamChat` 携带完整 `tools.Definitions()`（tools 计入续写前缀）。
- `load_skills` enrich 随 catalog 变化 → **整段 tools miss**。

### 3.4 多余 tool 步（P2 — WS4）

并行 tool_calls、重复 read、HITL — 分别靠批处理合理性、schema 约束、必要成本保留。

### 3.5 与压缩专题的边界

| 本专题 | [context-compression-cache-analysis.md](./context-compression-cache-analysis.md) |
|--------|----------------------------------------------------------------------------------|
| 减少 **turn 次数**、单次 tool 写入量 | 减少 **history 总 token**、侧车 cache hit |
| 延缓触达 silent/blocking 阈值 | 触达后降 miss 成本 |

---

## 4. 工作流路线图（本分支）

| ID | 名称 | 范围 | 优先级 | 本文档章节 | 状态 |
|----|------|------|--------|------------|------|
| **WS1** | 后台 job 长轮询 | bash 组：status/cancel + ACK 文案 | **P0** | **§5** | 设计完成 |
| **WS2** | Status 工具统一 wait | `temporary_agent_status` 等 | P1 | §3.1 | 未开始 |
| **WS3** | Tool 结果 budget | `packageToolResult`、grep、A2A | P1 | §3.2 | 未开始 |
| **WS4** | Schema 前缀稳定 | enrich 瘦身、description | P2 | §3.3 | 未开始 |
| **WS5** | 度量 | poll_count、tool_turns | P1 | §7 | 未开始 |

```text
feat/tool-context-cost-optimization
  ├── WS1 background_job wait_seconds     ← 首个 PR（§5）
  ├── WS2 status 工具泛化
  ├── WS3 结果体积
  ├── WS4 schema
  └── WS5 metrics
```

---

## 5. WS1：后台 job 长轮询（bash 组）

本节为 **首个落地工作流** 的完整分析（原 bash 轮询专题，已并入本文档）。

### 5.1 观测现象与目标

生产/联调常见模式：

1. `bash_run`（同步超时 **自动降级** 或 `run_in_background=true`）→ 获得 `job_id`。
2. 模型 **不等** `async_tool_result` 自动回灌，连续调用 `background_job_status`。
3. 参数通常 **仅有 `job_id`**（schema **无** 等待字段）。
4. 每次 `status=running` → 模型再推理 → 再 status，直至终态。

| 目标 | 说明 |
|------|------|
| **减少 LLM 往返** | **N 次瞬时 status** → **1 次带 wait 的 status** 或 **0 次（仅回灌）** |
| **保持兼容** | 不改 tool 名、不破坏 `enabled_groups: bash`、policy |
| **服务端可阻塞** | tool handler 内 long-poll，不占用额外 LLM slot |
| **对齐现有模式** | 与 `wait_temporary_agents(timeout_seconds)` 一致 |

### 5.2 现网实现

#### 5.2.1 bash 三件套

| 工具 | 执行特点 |
|------|----------|
| **`bash_run`** | 默认同步；`timeout_seconds` 内未完成 → **自动降级**；或 `run_in_background=true` |
| **`background_job_status`** | **瞬时** `statusText()`，**不阻塞** |
| **`background_job_cancel`** | 瞬时取消 |

`injectRunInBackgroundParam` 也为 fs/skills/triggers 等注入 `run_in_background`；job 管理工具 **只** 服务 `job_registry.go`。`IsBackgroundJobTool` 强制 status/cancel **同步**。

#### 5.2.2 异步两条路径

```text
路径 A：run_in_background=true
  invokeTool → StartBackground → [TOOL_BACKGROUND] job_id
  → goroutine Execute → notifyDone → EnqueueAsyncToolResult → HandleAsyncToolResult

路径 B：bash_run 同步超时降级
  runShellSyncWithAutoDegrade → timer → formatShellRunningResult(job_id)
  → collector Wait → notifyDone → 同路径 A
```

#### 5.2.3 status 现网语义

```go
func (r *Registry) execBackgroundJobStatus(_ context.Context, raw json.RawMessage) (string, error) {
    job, ok := r.bgJobs.get(args.JobID)
    return job.statusText(), nil  // 瞬时快照
}
```

输出含 `status`、`started_at/finished_at`、终态 `RESULT_PREVIEW`（2000 字符）。**无** `wait_seconds`、**无** 阻塞。

#### 5.2.4 引导轮询的文案

| 位置 | 问题 |
|------|------|
| `formatBackgroundJobAck` | 「可用 background_job_status 查询」 |
| `formatShellRunningResult` | 同上 |
| `background_job_status` schema | 未提 async 回灌、未提 wait |

#### 5.2.5 自动回灌（已存在）

```text
SetBackgroundJobNotifier → EnqueueAsyncToolResult（PriorityAsyncCompletion）
→ handleAsyncToolResult → buildAsyncToolMessages → 可选 RunToolMessageTurn
```

Issue #25：pending HITL 时仍写 history 但 **恢复** pending。

#### 5.2.6 对照子 Agent

| 工具 | 等待 |
|------|------|
| `wait_temporary_agents` | `timeout_seconds`（0=快照） |
| `temporary_agent_status` | 无（瞬时，同类问题 → **WS2**） |

### 5.3 轮询成本分解

单次轮询 turn 至少 **assistant + tool** 两条 message，常 **+1 assistant 续写**。Cache 只降重复 prefix 单价，**不消除 N × completion**。

设 `H ≈ 60k～100k`，`N ≈ 5～15`，每次 completion `C ≈ 50～300`：

- **output**：`N × C`（与 cache 无关）
- **input**：高 hit 下仍每轮付 tail miss + 变长 history

| 路径 | 完成前 LLM 次数 | 说明 |
|------|----------------|------|
| **A. 仅 async 回灌** | **0** | bash 后不再推理，等回灌 |
| **B. 1 次 long-poll status** | **1** | `wait_seconds ≥ 剩余耗时` |
| **C. 现网 N 次 snapshot** | **N** | 反模式 |

`bash_run.timeout_seconds`（同步窗口）与 `background_job_status.wait_seconds`（查询阻塞）**独立**，不可互相替代。

### 5.4 量化直觉（T_job=90s，sync=30s）

| 策略 | 完成前 LLM 往返 | history 污染 |
|------|----------------|--------------|
| 现网 C | 4～12 | 高（重复 status 消息） |
| B：`wait_seconds=120` 一次 | 1 | 低 |
| A：零轮询 | 0 | 最低 |

### 5.5 优选方案：`wait_seconds` 长轮询

1. **L1（P0）**：`background_job_status` 增加 **`wait_seconds`**；running 时 `select(job.done, timer, ctx.Done())`。
2. **L2（P0）**：ACK / schema 明确 **async_tool_result 为主**；查询须带 wait。
3. **L3（P1）**：`tools.background_job_status_max_wait_seconds`（默认 120）。

**不采用**：新工具名、删 status、WebSocket 推 status（Client 改造过大）。

#### 参数语义

| `wait_seconds` | 行为 |
|----------------|------|
| 省略或 0 | 现网瞬时 snapshot |
| > 0 | running 时阻塞至多 `min(wait, max_wait)`；完成→终态；超时→`running` + `waited_seconds` + hint |
| 已终态 | 忽略 wait |

#### Handler 伪代码

```go
func (r *Registry) execBackgroundJobStatus(ctx context.Context, raw json.RawMessage) (string, error) {
    args := parseBackgroundJobStatusArgs(raw)
    job, ok := r.bgJobs.get(args.JobID)
    wait := clampWait(args.WaitSeconds, r.bgJobStatusMaxWait)
    if wait > 0 && job.isRunning() {
        select {
        case <-job.done:
        case <-time.After(time.Duration(wait) * time.Second):
        case <-ctx.Done():
            return "", ctx.Err()
        }
    }
    return job.statusTextWithWaitMeta(waited), nil
}
```

并发：wait 监听 `job.done` channel，避免与 `cancelJob` 写锁死锁。

#### 文案（L2）拟

- status description：「完成后 async_tool_result 自动回灌，通常无需轮询；若查询请设 wait_seconds」
- ACK / 降级：「通常无需 status；若查询请 wait_seconds 一次等待」

#### 配置（拟）

```yaml
tools:
  background_job_status_max_wait_seconds: 120
```

### 5.6 WS1 补充方向

- 强化零轮询回灌（schema + TUI 提示）
- 度量：`background_job_status` 调用次数、`wait_seconds` 分布（→ **WS5**）
- P2：orchestrator defer LLM until job terminal（侵入大，不优先）

### 5.7 WS1 实施步骤

| 步骤 | 内容 | 文件 |
|------|------|------|
| 1 | config `BackgroundJobStatusMaxWaitSeconds` | `shared/config`, `config.example.yaml` |
| 2 | handler 长轮询 + `statusTextWithWaitMeta` | `tool_job.go`, `job_registry.go` |
| 3 | schema + ACK 文案 | `tool_job.go`, `job_registry.go`, `bash_runner.go` |
| 4 | 单测 | `job_registry_test.go` — sleep job + wait 一次 succeeded |
| 5 | 度量日志 | `tool_job.go`（M3 / WS5） |

### 5.8 WS1 风险

| 风险 | 缓解 |
|------|------|
| HTTP/proxy 超时 < wait | max_wait ≤ 120；nginx `proxy_read_timeout` |
| 用户 Esc 取消 turn | `ctx.Done()` |
| 模型仍不传 wait | L2 文案（不默认隐式 wait，保兼容） |

**不解决**：bash 单次输出过大（`bash_compress` / package）；job 运行中模型并行其他 tool。

---

## 6. WS2–WS5 概要（待展开）

| WS | 要点 |
|----|------|
| **WS2** | `temporary_agent_status.wait_seconds`，与 WS1 同一套 status 约定 |
| **WS3** | 可配置 `model_content_max`；read 去重提示；A2A 结果截断 |
| **WS4** | skills enrich 外置或缩短；enabled_groups 减面 |
| **WS5** | `tool_turns/session`、`status_poll_count` 结构化日志 / SSE |

---

## 7. 建议实施顺序

| 阶段 | 内容 |
|------|------|
| **T1** | WS1 代码 + 单测 + 文案 |
| **T2** | WS5 基础度量 |
| **T3** | WS2 子 Agent status |
| **T4** | WS3 结果 budget |
| **T5** | WS4 schema 稳定 |

---

## 8. 相关代码索引

| 主题 | 路径 |
|------|------|
| 工具注册 / enrich | `node/internal/tools/registry.go`, `registry_enrich.go` |
| bash / job | `bash_runner.go`, `bash_run_tool.go`, `job_registry.go`, `tool_job.go` |
| run_in_background | `execution_mode.go` |
| 编排 | `node/internal/turn/tool_router.go` |
| 结果写 history | `node/internal/turn/tool_result_messages.go` |
| async 回灌 | `node/internal/session/runtime.go`, `node/internal/api/server.go` |
| 子 Agent wait | `node/internal/tools/tool_childagent.go` |
| 压缩（正交） | `node/internal/compression/` |
| 实录 | [major-changes.md](./major-changes.md) §2 |

---

## 9. 结论

本分支以 **「减少低信息密度 LLM 往返 + 控制 tool 写入体积 + 稳定 tools 前缀」** 为纲，覆盖 **全部内置工具组**。**WS1（bash job `wait_seconds`）** 是首个落点：Node 已有 async 回灌，但瞬时 status + ACK 引导导致 **N 次 LLM 轮询**；长轮询与文案修订预期收敛为 **0～1 次**。WS2–WS5 将同一模式扩展到子 Agent status、结果 package 与 schema enrich。与压缩/cache 优化 **应叠加评估**。
