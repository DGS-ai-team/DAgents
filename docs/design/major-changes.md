# 重大设计变更与优化实录

本页记录 **Go 本地助手栈**（`node/`、`client/`）中值得单独成篇的架构级优化：背景、思路、落地与延伸阅读。日常小改动见 [CHANGELOG.md](../../CHANGELOG.md)；模块 API 见各包 `README.md` / `REFERENCE.md`。

**如何阅读**

| 板块 | 内容 |
|------|------|
| 背景与痛点 | 改之前为什么痛、代价在哪 |
| 优化思路 | 核心判断与取舍 |
| 落地方案 | 代码/配置层面的实现要点 |
| 效果与局限 | 预期收益与仍存在的结构性限制 |
| 延伸阅读 | 设计稿、代码路径 |

新增条目时复制文末 **条目模板**，保持「问题 → 思路 → 落地」顺序，并链接到可长期维护的设计文档（避免只在 PR 描述里留档）。

---

## 1. 上下文压缩与 Prompt Cache 对齐（M2 + M3）

| | |
|---|---|
| **分支/时期** | `feat/compression-cache-optimization`（2026-06） |
| **范围** | `node/internal/compression`、`node/internal/session/runtime_turn.go`、`node/internal/turn`（`ToolDefinitions`） |
| **配置** | `compression.silent_trigger_tokens` / `compression.blocking_trigger_tokens`（示例 80k / 100k） |

### 背景与痛点

Go Node 在上下文超过阈值时会做 **summary 压缩**（silent 后台 + blocking 同步）。改前实现用独立的 `CompleteText` 调用：

- **system** 使用固定小 prompt，与主 turn 的 `BuildSystemPrompt` **不一致**
- **无 tools**，与主 turn 出站前缀 **不一致**
- 待压缩内容经 `buildHumanBlock` **二次序列化**进一条巨型 user

在 DeepSeek 等 **按请求前缀** 计费的 Prompt Cache 下，压缩请求与主 turn **几乎零共享前缀**；触发时 input 体量又接近阈值（~80k），侧车单次 **几乎全 miss**。更糟的是，在 `[T_s, T_b)` 区间每条入站消息都可能再开 silent，重复支付大额 miss。

### 优化思路

1. **侧车与主 turn 同一续写前缀**：`BuildSystemPrompt` + `tools.Definitions()` + `messages` 前缀（至切点 `end`），仅尾部追加 ephemeral 摘要指令（`summaryUserPrompt` 作为**最后一条 user**）。
2. **同一 API 栈**：侧车改 `StreamChat`（走 adapter 出站），与主 turn 一致，从响应 `usage` 读取 `prompt_cache_hit_tokens` 等。
3. **压缩区间合法化**：tool 批须闭合；两种 apply 模式（保留 tail assistant / 合并 next user）；非法 messages **noop 不修复**。
4. **抑制重复 silent（M3）**：`readyCompressions` pending 去重 + apply 后冷却（默认 60s 或 token 增量 ≥4000）。

### 落地方案

```text
runtime.runTurnStep（步前）
  → sidecarPrefix() = { SystemPrompt, Tools }   // 与 runOneStep 同源
  → compression.MaybeHandle(..., prefix)
       → evaluateCompression（单次 buildCompressionPlan）
       → 侧车 Summarize(StreamChat) → readyCompressions
       → applyCompressionReplacement + fingerprint
```

| 模块 | 职责 |
|------|------|
| `plan.go` | `evaluateCompression`、`buildCompressionPlan`、`leadingSystemSkip`（P9 防御） |
| `sidecar.go` | `BuildSidecarChatRequest`、`Summarize` |
| `coordinator.go` | silent/blocking 状态机、`silent_cooldown` |
| `metrics.go` / `last_compression.go` | SSE/API usage；`GET /context` 的 `last_compression` |

**观测**：压缩成功时 SSE `end/applied` 与 POST `/compress` 返回 `prompt_tokens`、`completion_tokens`、`prompt_cache_hit_tokens` 等（对齐 [DeepSeek Chat Completions usage](https://api-docs.deepseek.com/zh-cn/api/create-chat-completion)）。

### 效果与局限

| 维度 | 改前 | 现网 |
|------|------|------|
| 侧车单次 miss | ~O(阈值) 全新前缀 | 主要为 ephemeral 尾部（~数百 token） |
| `[T_s,T_b)` 重复 silent | 每条入站可再触发 | 冷却期内至多 1 次（无 pending 时） |
| apply 后主 turn cache | 前缀被摘要替换，仍 **重置** | **未消除**（结构性限制） |

**生产共识（P9）**：session/SQLite `messages` **不含** `role=system`；system 由 `llm.MessagesWithSystem` 出站注入。压缩区间在生产路径为 `messages[0:end+1]`。

### 延伸阅读

- 完整分析：[context-compression-cache-analysis.md](./context-compression-cache-analysis.md)
- 模块说明：[node/internal/compression/README.md](../../node/internal/compression/README.md)
- 用户向专题（历史 Python 栈对照）：[context-compression-and-state.md](../context-compression-and-state.md)

---

## 2. 工具链上下文成本优化（规划中）

| | |
|---|---|
| **分支/时期** | `feat/tool-context-cost-optimization`（2026-06） |
| **范围** | 全内置工具组（`node/internal/tools`、`turn` 工具结果写回、编排 dispatch） |
| **配置** | 按工作流分项（WS1 拟 `tools.background_job_status_max_wait_seconds` 等） |

### 背景与痛点

Agent turn 的成本 = **history 体量**（§1 压缩/cache 专题）× **LLM 往返次数**。长会话中除压缩 miss 外，常见浪费包括：

- **轮询型 status**（`background_job_status`、`temporary_agent_status` 等瞬时 snapshot）
- **tool 结果膨胀**（bash 输出、read/grep、A2A 全文进 history）
- **tools schema 前缀漂移**（`load_skills` enrich 随 catalog 变化）

与 Prompt Cache **正交**：cache 降低重复 prefix 单价，**不减少**轮询带来的 completion 与 message tail。

**总览与分工作流路线图**：[tool-context-cost-analysis.md](./tool-context-cost-analysis.md)

### 工作流（本分支）

| ID | 内容 | 分析 | 状态 |
|----|------|------|------|
| **WS1** | 后台 job `wait_seconds` 长轮询（bash 组先行） | [tool-context-cost-analysis.md](./tool-context-cost-analysis.md) §5 | 设计完成 |
| **WS2** | status 工具统一 wait（子 Agent 等） | 合入总览 §4 | 未开始 |
| **WS3** | tool 结果 budget / package | 合入总览 §3.2 | 未开始 |
| **WS4** | schema 前缀稳定（enrich 瘦身） | 合入总览 §3.3 | 未开始 |
| **WS5** | 度量（poll_count、tool_turns） | — | 未开始 |

### 优化思路（总纲）

1. **能 push 不 poll**：async_tool_result / 阻塞 wait 优先于 snapshot 轮询。
2. **能一次不等 N 次**：status 类工具服务端 long-poll（`wait_seconds`）。
3. **能短不长**：统一 tool 结果写入 history 的 budget。
4. **能稳不动**：减少 tools schema 无谓 enrich 抖动。

### 延伸阅读

- **[tool-context-cost-analysis.md](./tool-context-cost-analysis.md)**（完整分析，含 WS1 §5）
- [context-compression-cache-analysis.md](./context-compression-cache-analysis.md)（正交）
- [built-in-tools.md](../built-in-tools.md) §0

---

## 条目模板（复制使用）

```markdown
## N. 标题（一句话）

| | |
|---|---|
| **分支/时期** | |
| **范围** | |
| **配置** | |

### 背景与痛点

### 优化思路

### 落地方案

### 效果与局限

### 延伸阅读
```
