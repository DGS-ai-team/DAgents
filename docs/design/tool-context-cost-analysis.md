# 工具链上下文成本优化 — 总览与分析索引

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

### 1.2 优化总目标

```text
在不大改 Agent 能力边界的前提下：
  1. 减少「低信息密度」的 LLM 往返（轮询、重复读、不必要的 status）
  2. 控制写入 history 的 tool 结果体积（与 bash_compress / package 策略一致）
  3. 稳定 tools schema 前缀（减少 enrich 漂移导致的 cache 断档）
  4. 可度量：每任务 tool_turns、tool_result_chars、status_poll_count
```

---

## 2. 现网工具链与成本触点

### 2.1 内置工具分组（`tools.enabled_groups`）

见 [built-in-tools.md](../built-in-tools.md) §0。每组与 **上下文成本** 相关的触点如下：

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
| **`run_in_background`** | `execution_mode.go` | 任意注入工具可后台化；完成走 **async_tool_result**；ACK 文案偏 bash |
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
| 临时子 Agent | `wait_temporary_agents(timeout_seconds)`、`create(wait=true)` | `temporary_agent_status` | 父 session 回调（子 Agent 路径） |
| A2A invoke | `agent_invoke` 内 HTTP 等待 | — | — |
| fs / triggers 等 `run_in_background` | — | 同 bash job 表 | async_tool_result ✅ |

**缺口**：snapshot 类 status 工具 **无统一 `wait_seconds`**；模型默认 **轮询**。

---

## 3. 成本来源分解

### 3.1 轮询型（P0 — 工作流 WS1）

**典型**：`background_job_status` 无 wait → 每 5～15s 一次 LLM turn（详见子文档）。

**同类模式**：

- `temporary_agent_status`（子 Agent running 时）
- 模型对 **已提交 async job** 的不信任而重复 status
- 未来：任何「瞬时 status + 后台完成」组合

**策略**：统一 **long-poll 参数** + **强调 auto-push 主路径**。

→ 子文档：[background-job-long-poll-analysis.md](./background-job-long-poll-analysis.md)（bash 三件套先行落地）

### 3.2 Tool 结果膨胀（P1 — WS2）

| 来源 | 现网缓解 | 待优化 |
|------|----------|--------|
| bash stdout/stderr | `bash_compress` + 12k package | 跨工具统一 budget；raw_ref 实际落盘一致性 |
| read_file / grep | offset/limit | 模型仍整文件多次 read |
| search_replace diff | 输出 diff 摘要 | 大 diff 仍 long |
| agent_invoke result | 对端全文 | 截断策略 |

**策略**：按工具类型 configurable **model_content_max**；schema 强调「已 read 勿重复」；可选 **read_file 指纹** 提示（P2）。

### 3.3 Tools schema 前缀（P1 — WS3）

- 每步 `StreamChat` 携带完整 `tools.Definitions()`（OpenAI 兼容续写模型 **tools 计入 prefix**）。
- `load_skills` enrich 随 skills 目录变化 → **整段 tools miss**。
- 各 tool description 较长（bash 平台文案、fs 约定、trigger schedule 示例）。

**策略**：

- 静态/动态 description 分离（catalog 进 **运行时 API** 或缩短 enrich）
- enabled_groups 减面 → 少工具少 prefix
- 避免无意义的 per-request enrich 抖动

### 3.4 多余 tool 步（P2 — WS4）

- 并行 tool_calls 批：合理保留。
- 同一文件连续 read：靠 prompt + policy 软约束。
- HITL / ask_user：必要成本。

### 3.5 与 §1 压缩的边界

| 本专题 | 压缩专题 |
|--------|----------|
| 减少 **turn 次数**、单次 tool 写入量 | 减少 **history 总 token**、侧车 cache hit |
| 优化后 **延缓** 触达 silent/blocking 阈值 | 触达后 **降 miss 成本** |

---

## 4. 工作流路线图（本分支）

| ID | 名称 | 范围 | 优先级 | 分析文档 | 状态 |
|----|------|------|--------|----------|------|
| **WS1** | 后台 job 长轮询 | bash 组：status/cancel + ACK 文案 | **P0** | [background-job-long-poll-analysis.md](./background-job-long-poll-analysis.md) | 设计完成 |
| **WS2** | Status 工具统一 wait | `temporary_agent_status` 等对齐 `wait_seconds` | P1 | （待写 §WS2 或合入 WS1 终章） | 未开始 |
| **WS3** | Tool 结果 budget | `packageToolResult`、bash_compress、grep 分页策略统一 | P1 | （待写） | 未开始 |
| **WS4** | Schema 前缀稳定 | enrich 策略、description 瘦身、skills 元数据外置 | P2 | （待写） | 未开始 |
| **WS5** | 度量 | tool_turns/session、poll_count、result_chars SSE/log | P1 | — | 未开始 |

```text
feat/tool-context-cost-optimization
  ├── WS1 background_job wait_seconds     ← 首个 PR
  ├── WS2 status 工具泛化
  ├── WS3 结果体积
  ├── WS4 schema
  └── WS5 metrics
```

---

## 5. WS1 摘要（bash — 详见子文档）

**问题**：`background_job_status` 瞬时 snapshot + ACK 引导轮询 → 长上下文 **N 次 LLM 往返**。

**方案**：`wait_seconds` 服务端 long-poll + async 回灌文案 + `background_job_status_max_wait_seconds` config。

**不改**：tool 名、async_tool_result 链路、cancel 语义。

→ 完整推导、伪代码、M1–M4 步骤见 **[background-job-long-poll-analysis.md](./background-job-long-poll-analysis.md)**。

---

## 6. 建议实施顺序

| 阶段 | 内容 |
|------|------|
| **T1** | 开分支 `feat/tool-context-cost-optimization`；落地 WS1 + 单测 |
| **T2** | WS5 基础度量（status 调用计数、wait 使用率） |
| **T3** | WS2 `temporary_agent_status.wait_seconds` |
| **T4** | WS3 结果 budget 配置化 |
| **T5** | WS4 schema 稳定（需与 skills 产品行为对齐） |

---

## 7. 相关代码索引

| 主题 | 路径 |
|------|------|
| 工具注册 / enrich | `node/internal/tools/registry.go`, `registry_enrich.go` |
| 执行 / 后台 | `execution_mode.go`, `job_registry.go`, `bash_runner.go` |
| 编排 dispatch | `node/internal/turn/tool_router.go` |
| 结果写 history | `node/internal/turn/tool_result_messages.go` |
| async 回灌 | `node/internal/session/runtime.go`, `node/internal/turn/orchestrator.go` |
| 子 Agent wait | `node/internal/tools/tool_childagent.go` |
| 压缩（正交） | `node/internal/compression/` |
| 实录索引 | [major-changes.md](./major-changes.md) §2 |

---

## 8. 结论

本分支 **不是** 仅修 bash 轮询，而是以 **「减少低信息密度 LLM 往返 + 控制 tool 写入体积 + 稳定 tools 前缀」** 为纲，对 **全部内置工具组** 分工作流推进。**WS1（bash job long-poll）** 是观测最明确、改动最集中、收益可验证的第一落点；后续 WS2–WS5 将同一套模式扩展到子 Agent status、结果 package 与 schema enrich。
