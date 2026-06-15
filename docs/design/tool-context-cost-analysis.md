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
| **`StartBackground` / async 回灌** | `execution_mode.go`、`job_registry.go` | **bash 超时降级** 与内部测试；**不在 schema 暴露** |
| **`call_purpose`** | 各 tool schema | 每 call 必填，略增 arguments 体积（UI 价值高，保留） |
| **`Definitions()` 每步发送** | `orchestrator.runOneStep` | 全量 tools JSON 计入 **续写前缀** |
| **`enrichDefinitions`** | `registry_enrich.go` | `load_skills` 附加 skills 元数据 → catalog 变则 **prefix miss** |
| **`packageToolResult`（旧）** | `turn/tool_result_messages.go` | 已由 **`tool.after_each`** 替代（§3.2.1） |
| **bash_compress** | `bash_compress.go` | bash 输出清洗截断；stats 进 SSE |
| **async 回灌** | `job_registry` + `HandleAsyncToolResult` | 完成时 **+2～3 条** message，但避免完成前 N 次轮询 |

### 2.3 已对齐 vs 未对齐的「等待」模式

| 场景 | 阻塞等待 API | 瞬时 snapshot API | 自动 push |
|------|-------------|-------------------|-----------|
| 后台 bash job | — | `background_job_status` | **async_tool_result** ✅ |
| 临时子 Agent | `wait_temporary_agents(timeout_seconds)`、`create(wait=true)` | `temporary_agent_status` | 父 session 回调 |
| A2A invoke | `agent_invoke` 内 HTTP 等待 | — | — |
| fs / triggers 等 | — | 同步执行 | — |

**缺口**：`temporary_agent_status` 等仍为瞬时 snapshot（→ **WS2**，可选）；bash job 侧 **不** 为 `background_job_status` 增加 long-poll，靠 async 回灌 + **WS6** 重复调用审批收敛轮询。

---

## 3. 成本来源分解（全工具）

### 3.1 轮询型（P0 — WS1）

**典型**：`background_job_status` 无 wait → 每 5～15s 一次 LLM turn（**§5 详述**）。

**同类模式**：`temporary_agent_status`；模型对 async job 的不信任而重复 status。

**策略**：bash job → **async 回灌为主** + ACK 文案 + **WS6 duplicate hook**；子 Agent status → 可选 **WS2** long-poll。

### 3.2 Tool 结果膨胀（P1 — WS3）

| 来源 | 现网缓解 | 待优化 |
|------|----------|--------|
| bash stdout/stderr | **§3.2.1** 清洗 + 落盘 + history 头尾摘要 | 其它组按 enabled_groups 逐步接入 |
| read_file / grep | offset/limit + bytes 上限 | 模型仍整文件多次 read |
| search_replace diff | 输出 diff 摘要 | 大 diff 仍 long |
| agent_invoke result | 对端全文 | 截断 + 落盘（待做） |

#### 3.2.0 tool_result 压缩全链路（现网）

| 阶段 | 位置 | 作用对象 | 行为 |
|------|------|----------|------|
| **Tool 清洗** | `bash_compress.sanitizeBashStream` | bash stdout/stderr | 去 ANSI、合并重复行；**不再**在此截断长度 |
| **Tool 分页/上限** | `fs_*` | read/grep/glob 等 | bytes/行窗口 + 翻页 hint |
| **Job preview** | `job_registry` | status 终态 preview | 2000 bytes；完整靠 async 回灌 |
| **`tool.after_each`** | `hooks.ToolResultPackageHook` → `toolresult.Package` | 配置内工具（首版 **`bash_run`**） | 超长：落盘 + history 头尾摘要 + `read_file` hint |
| **SSE / Client** | `publishToolResult` | TUI | **全文**（清洗后、未 history 摘要） |
| **history** | `appendHistory(role=tool)` | LLM | **摘要或全文**（由 Hook 输出 `ForHistory`） |
| **Hook 元数据** | `ToolExecutionLog` | duplicate 审批 | 结果 preview 200 bytes |
| **TUI 展示** | `client/.../tool_format.go` | 用户界面 | 240 runes / 8 行（不进 history） |
| **Context 压缩** | `compression.Coordinator` | 整段 messages | 摘要替换旧消息（正交） |

**原则**：禁止「只截断不落盘」；history 摘要必须附带 **可 `read_file` 的相对路径**。

#### 3.2.1 WS3 · bash 组（已落地）

**锚点**：`tool.after_each`（`ToolResultPackageHook`）+ `node/internal/toolresult/`。

```text
bash_run Execute
  → sanitizeBashStream（tools.bash_compress.enabled）
  → ForClient = 全文 → SSE
  → 若估算 tokens > hooks.tool_result.max_history_tokens（默认 12000，[DeepSeek 粗算](https://api-docs.deepseek.com/zh-cn/quick_start/token_usage)：汉字×0.6、其它×0.3）：
       落盘 fs_root/.runtime/tool_outputs/<session>/<tool_call_id>.txt
       ForHistory = 头 + ...（已省略约 N tokens，完整输出已写入 "path"，请 read_file 分页）... + 尾
  → async 回灌同路径（ForClient 全文进 SSE，ForHistory 摘要进 tool message）
```

**配置**（`config.yaml`）：

```yaml
hooks:
  tool_result:
    enabled: true
    max_history_tokens: 12000
    spill_subdir: .runtime/tool_outputs
    tools:
      - bash_run
tools:
  bash_compress:
    enabled: true   # 仅清洗，不截断
```

**代码索引**：`toolresult/`、`hooks/builtin_tool_result.go`、`turn/tool_router.go`（`splitToolResult`）、`turn/tool_result_messages.go`（async）。

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
| **WS1** | 后台 job 轮询治理 | bash 组：ACK 文案；**status 保持瞬时** | **P0** | **§5** | **已落地**（不实现 `wait_seconds`） |
| **WS6** | 重复调用 Hook 审批 | `tool.before_each`；**仅 policy `rule`+auto** + 60s 指纹 + 标准审批原因 | **P0** | — | **已落地** |
| **WS2** | Status 工具统一 wait | `temporary_agent_status` 等 | P1 | §3.1 | 未开始 |
| **WS3** | Tool 结果 budget | `tool.after_each` + 落盘；**bash 组已落地** §3.2.1 | P1 | §3.2 | **bash 已落地**；fs/a2a 待做 |
| **WS4** | Schema 前缀稳定 | enrich 瘦身、description | P2 | §3.3 | 未开始 |
| **WS5** | 度量 | poll_count、tool_turns | P1 | §7 | 未开始 |

```text
feat/tool-context-cost-optimization
  ├── WS1 文案 + status 保持现状          ← 已完成（§5）
  ├── WS6 duplicate tool hook + HITL      ← 已完成
  ├── WS2 status 工具 wait（子 Agent 等，可选）
  ├── WS3 结果体积
  ├── WS4 schema
  └── WS5 metrics
```

---

## 5. WS1：后台 job 轮询治理（bash 组）

本节为 bash job 轮询问题的分析与 **已落地决策**（原 bash 轮询专题，已并入本文档）。

### 5.1 观测现象与目标

生产/联调常见模式：

1. `bash_run` 同步等待 → **`timeout_seconds` 内未完成则自动降级** → 获得 `job_id`。
2. 模型 **不等** `async_tool_result` 自动回灌，连续调用 `background_job_status`。
3. 参数通常 **仅有 `job_id`**（schema **无** 等待字段）。
4. 每次 `status=running` → 模型再推理 → 再 status，直至终态。

| 目标 | 说明 |
|------|------|
| **减少 LLM 往返** | 优先 **0 次（仅 async 回灌）**；若模型仍轮询 → **WS6** 拦截 |
| **保持兼容** | 不改 tool 名、不破坏 `enabled_groups: bash`、policy |
| **`background_job_status`** | **保持瞬时 snapshot**，不增加 `wait_seconds` |

### 5.2 现网实现

#### 5.2.1 bash 三件套

| 工具 | 执行特点 |
|------|----------|
| **`bash_run`** | 默认同步；`timeout_seconds` 内未完成 → **自动降级** |
| **`background_job_status`** | **瞬时** `statusText()`，**不阻塞** |
| **`background_job_cancel`** | 瞬时取消 |

各工具 schema 仅注入 **`call_purpose`**（无 `run_in_background`）。job 管理工具 **只** 服务 `job_registry.go`。`IsBackgroundJobTool` 强制 status/cancel **同步**。

#### 5.2.2 异步路径（bash 超时降级）

```text
bash_run 同步 Execute
  → runShellSyncWithAutoDegrade → timer 触发
  → formatShellRunningResult(job_id) → collector Wait
  → notifyDone → EnqueueAsyncToolResult → HandleAsyncToolResult
```

（内部 `StartBackground` 仍供测试与编排器兼容历史参数，不对模型暴露。）

#### 5.2.3 status 现网语义

```go
func (r *Registry) execBackgroundJobStatus(_ context.Context, raw json.RawMessage) (string, error) {
    job, ok := r.bgJobs.get(args.JobID)
    return job.statusText(), nil  // 瞬时快照
}
```

输出含 `status`、`started_at/finished_at`、终态 `RESULT_PREVIEW`（2000 字符）。**无** `wait_seconds`、**无** 阻塞。

#### 5.2.4 ACK / schema 文案（已落地）

| 位置 | 现状 |
|------|------|
| `formatBackgroundJobAck` / `formatShellRunningResult` | 强调 **async_tool_result 自动回灌**，通常无需轮询 status |
| `background_job_status` schema | 说明完成后自动回灌；仅在取消或主动确认进度时使用 |

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
| **A. 仅 async 回灌** | **0** | **优选**；bash 降级后不再推理，等回灌 |
| **B. long-poll status** | 1 | **不采用**（见 §5.5） |
| **C. N 次 snapshot 轮询** | **N** | 反模式；**WS6 duplicate hook** 兜底 |

### 5.4 量化直觉（T_job=90s，sync=30s）

| 策略 | 完成前 LLM 往返 | history 污染 |
|------|----------------|--------------|
| 现网 C（无治理） | 4～12 | 高 |
| **A + WS6（已落地）** | **0～1** | 低 |
| B：long-poll（不采用） | 1 | 低 |

### 5.5 已决方案：不实现 `background_job_status.wait_seconds`

**决策（2026-06）**：`background_job_status` **保持现网瞬时 snapshot**，不为 schema 增加 `wait_seconds` 或服务端 long-poll。

| 理由 | 说明 |
|------|------|
| **主路径已存在** | `async_tool_result` 在 job 终态自动回灌，零轮询即可闭环 |
| **文案已对齐** | ACK / 降级 / status description 已引导「通常无需轮询」 |
| **WS6 兜底** | 60s 内同名同参重复 `background_job_status` → 标准 HITL（`rule`+auto） |
| **避免重叠语义** | `bash_run.timeout_seconds` 已是同步等待窗口；status long-poll 与 HTTP 代理超时、turn 取消交织，收益有限 |
| **无 sleep 工具** | 等待应绑定事件（回灌 / `wait_temporary_agents`），而非 status 阻塞 |

**不采用**（仍成立）：新工具名、删 status、WebSocket 推 status、通用 `sleep` 工具。

> 下文 §5.5 原 long-poll 设计保留为 **历史备选**，供 WS2（子 Agent status）参考时酌情复用，**bash job 不再实施**。

<details>
<summary>历史备选：long-poll 设计（bash job 不实施）</summary>

1. **L1**：`background_job_status` 增加 **`wait_seconds`**；running 时 `select(job.done, timer, ctx.Done())`。
2. **L2**：ACK / schema 明确 async 为主。
3. **L3**：`tools.background_job_status_max_wait_seconds`（默认 120）。

</details>

### 5.6 WS1 已落地项

| 项 | 文件 |
|----|------|
| ACK / 降级文案 | `job_registry.go` |
| status schema description | `tool_job.go` |
| 移除 `run_in_background` schema | `execution_mode.go`、各 tool def |
| 重复 status 审批 | `node/internal/hooks/`（WS6） |

### 5.7 后续（非 WS1）

- **WS5**：`background_job_status` 调用次数、`status_poll_count` 度量
- **WS2**（可选）：仅 **子 Agent** `temporary_agent_status` 是否 long-poll，与 bash job **独立决策**
- P2：orchestrator defer LLM until job terminal（侵入大，不优先）

**不解决**：bash 单次输出过大（`bash_compress` / package）；job 运行中模型并行其他 tool。

---

## 6. WS2–WS6 概要（待展开）

| WS | 要点 |
|----|------|
| **WS6** | 仅 **`rule`+auto** 路径：60s 内同名同参 → 标准 HITL 审批原因；详见 [tool-before-hook-duplicate-approval.md](./tool-before-hook-duplicate-approval.md) |
| **WS2** | `temporary_agent_status.wait_seconds`（**可选**；bash job 已决不做 long-poll） |
| **WS3** | 可配置 `hooks.tool_result`；read 去重提示；A2A 结果截断+落盘 |
| **WS4** | skills enrich 外置或缩短；enabled_groups 减面 |
| **WS5** | `tool_turns/session`、`status_poll_count` 结构化日志 / SSE |

---

## 7. 建议实施顺序

| 阶段 | 内容 |
|------|------|
| ~~**T1**~~ | ~~WS1~~ **已完成**（文案 + status 保持现状 + WS6） |
| **T2** | WS5 基础度量 |
| **T3** | WS2 子 Agent status（可选） |
| **T4** | WS3 结果 budget |
| **T5** | WS4 schema 稳定 |

---

## 8. 相关代码索引

| 主题 | 路径 |
|------|------|
| 工具注册 / enrich | `node/internal/tools/registry.go`, `registry_enrich.go` |
| bash / job | `bash_runner.go`, `bash_run_tool.go`, `job_registry.go`, `tool_job.go` |
| call_purpose / StartBackground（内部） | `execution_mode.go` |
| 编排 | `node/internal/turn/tool_router.go` |
| 结果写 history | `node/internal/turn/tool_result_messages.go` |
| async 回灌 | `node/internal/session/runtime.go`, `node/internal/api/server.go` |
| 子 Agent wait | `node/internal/tools/tool_childagent.go` |
| 压缩（正交） | `node/internal/compression/` |
| 实录 | [major-changes.md](./major-changes.md) §2 |

---

## 9. 结论

本分支以 **「减少低信息密度 LLM 往返 + 控制 tool 写入体积 + 稳定 tools 前缀」** 为纲。**WS1 + WS6** 已落地：bash job 靠 **async 回灌 + ACK 文案** 引导零轮询，`background_job_status` **保持瞬时**；模型仍短时重复调用时由 **duplicate hook** 进 HITL。下一步优先 **WS5 度量** 与 **WS3 结果 budget**。与压缩/cache 优化 **应叠加评估**。
