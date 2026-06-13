# 上下文压缩与 Prompt Cache 命中率分析

> 分支：`feat/compression-cache-optimization`  
> 范围：Go Agent Node（`node/internal/compression`、`node/internal/turn`、`node/internal/llm`）  
> 配置示例：`silent_trigger_tokens: 80000`、`blocking_trigger_tokens: 100000`

---

## 1. 背景：我们在优化什么

DeepSeek / OpenAI 兼容 API 的 **Prompt Cache**（响应里 `prompt_cache_hit_tokens` / `cached_tokens`）按 **请求前缀** 计费与复用：

- 同一 session、连续 turn 中，若 **system + messages 前缀** 与上一轮一致，新增内容只出现在 **尾部**，则前缀 token 可走 cache hit。
- **任意前缀变化**（system 改动、中间 message 改写、压缩替换区间）都会使 **变化点之后** 的 token 变为 miss，并可能使后续前缀无法复用上一轮 cache 槽位（取决于 provider 实现与 TTL）。

DAgents 当前有两类 LLM 请求：

| 类型 | 入口 | system | messages / user |
|------|------|--------|-----------------|
| **主 turn** | `Orchestrator.runOneStep` → `StreamChat` | `BuildSystemPrompt`（大、含侧车/skills） | 完整 `history` + tools |
| **压缩摘要** | `compression.Coordinator.runCompressionFlow` → `CompleteText` | 固定 `summarySystemPrompt`（小） | 单条 user：序列化后的「待压缩块 + 后续文本」 |

两类请求的 **前缀完全不共享**，且压缩请求在触发时往往携带 **接近阈值的巨量文本**，几乎全部计为 **cache miss**。

---

## 2. 当前实现（Go Node）

### 2.1 触发时机

```
用户消息 / resume / tool 结果
  → runtime.runTurnStep(compressBefore=true)   // runtime_turn.go
  → compression.MaybeHandle()
  → （若通过阈值）runCompressionFlow → CompleteText
  → pending → applyReadyResult（替换 messages 区间）
  → Orchestrator.runOneStep → StreamChat（主 turn）
```

- **子 Agent session 不压缩**（`!r.isChildSession()`）。
- **每条** 会 `compressBefore=true` 的入站消息都会调用 `MaybeHandle`（先尝试 apply pending，再判定是否新开压缩任务）。

### 2.2 阈值判定

`plan.go` → `shouldCompress`：

- 使用 `llm.EstimateMessageTokens(messages)`（**全文** `len/4` 粗算，含 reasoning、tool_calls 加权）。
- `total >= blocking_trigger_tokens` → **blocking**（同步压缩，阻塞当前消息）。
- 否则 `total >= silent_trigger_tokens` → **silent**（后台 goroutine，不阻塞本条消息）。
- **blocking 优先于 silent**。

### 2.3 压缩区间

`selectCompressRange`（Go 实现较 Python 简化）：

- 找 **最后一条 `assistant` 消息** 下标 `lastAssistant`。
- 压缩候选：`messages[0 : lastAssistant]`（**不含** 最后一条 assistant）。
- 若 `lastAssistant <= 0` → 不压缩。

与 [context-compression-and-state.md](../context-compression-and-state.md) 中 Python 版描述的差异：

- Python 有 **从尾部回退直至 tool_calls/tool 成对闭合** 的 plan；Go **未实现**该回退，可能把未闭合的 tool 轮次打进压缩块，或区间与 provider 侧「安全前缀」不一致。
- Go 的 `buildHumanBlock` **跳过 role=system**，但 **不剔除** 中间 assistant(tool_calls)/tool 序列的结构性要求。

### 2.4 发给压缩模型的 payload

`runCompressionFlow`：

```text
system: summarySystemPrompt（固定 ~200 字）
user:   "待压缩文本块：" + buildHumanBlock(picked)
        + "；后续文本为：" + buildHumanBlock(source[end+1:])
```

`buildHumanBlock` 每条非 system 消息 **content 截断 800 字符**（`plan.go` `truncate(..., 800)`），但：

- **触发阈值** 按 **未截断** 的 `EstimateMessageTokens` 计算 → 可能在「估算 80k、实际 compress prompt 更小」或相反之间漂移。
- 工具结果很长时，截断会 **丢失压缩输入信息**，但 token 估算仍按全文 → 行为不一致。

### 2.5 压缩结果写回

- 将 `[start, end]` 区间替换为 **一条** `role=user` 的摘要。
- 保留 `messages[end+1:]`（含最后一条 assistant 及之后所有消息）。
- 使用 **JSON 全量 fingerprint** 防 stale apply（`fingerprint.go`）。

**对主 turn cache 的影响**：替换发生在 **messages 前缀**，下一轮 `StreamChat` 的 messages 前缀与压缩前 **完全不同** → 主 turn 在 provider 侧 **无法** 复用压缩前积累的 message 前缀 cache。

---

## 3. Cache Miss 来源分解

### 3.1 压缩专用请求（CompleteText）— 主要矛盾

假设 `messages_total_tokens ≈ 80_000`（silent 刚触发）：

| 组成部分 | 相对主 turn | Cache 预期 |
|----------|-------------|------------|
| `summarySystemPrompt` | 与 `BuildSystemPrompt` **不同** | 与主 turn **不共享** prefix |
| user 中「待压缩块」 | 约 `0 .. lastAssistant-1` 的序列化文本 | **全新**；体量 ≈ 阈值量级 |
| user 中「后续文本」 | 最后 assistant 及之后（通常较短） | 全新 |

结论：

1. **整次压缩推理几乎全是 miss**（用户描述的「compress prompt + ~80000 input」成立；system 较小但 user 占绝对主体）。
2. **silent 在 `[80k, 100k)` 区间可能多次触发**：每次新用户消息入口 `MaybeHandle` 时，若仍 ≥ silent 且无上一次 silent 在跑，会 **再开** `startSilentTask` → **又一次** 全量 miss 的 CompleteText。
3. **blocking 在 ≥100k** 时同步压缩，用户感知延迟 + 一次大额 miss。
4. 压缩与主 turn **串行/并行** 均不帮助压缩请求本身命中 cache——除非 **故意** 让压缩请求复用与主 turn 相同的消息排列（见 §5）。

### 3.2 主 turn（StreamChat）— 压缩后的二次伤害

压缩 apply 之后：

- `messages` 前缀从「多轮 user/assistant/tool…」变为 **一条摘要 user**。
- 后续 turn 只能在 **新前缀** 上重新积累 cache；压缩前已付费的 prefix cache **对该 session 基本作废**。
- 若摘要极长，新前缀仍大；若摘要控制得好，主 turn 前缀变短，**长期** hit 率可能回升，但 **压缩当次** 的 miss 成本已发生。

### 3.3 主 turn — 与压缩无关的 cache 扰动

| 因素 | 位置 | 影响 |
|------|------|------|
| **System prompt 重建** | 每 tool loop 步 `buildSystemPrompt` | skills 加载/卸载、侧车 mtime 变更 → system 变 → **整段 prefix miss** |
| **Tools schema** | 每步 `Definitions()` 随请求发送 | 部分 provider 将 tools 纳入 cache 键；工具列表/enrich 变化 → miss |
| **DeepSeek thinking** | `RequestExtra` | 一般不影响 messages prefix，但独立计费维度 |
| **流式增量** | 每 turn 仅尾部新增 assistant/tool | 理想情况下 **前缀 messages 不变则 hit**；tool 结果 append 只 miss 尾部 |
| **Token 估算 vs 真实 usage** | `EstimateMessageTokens` vs SSE `usage` | 阈值触发时机与 provider 真实 token 不一致，可能导致 **过早/过晚** 压缩，间接影响 hit 曲线 |

### 3.4 其他 LLM 调用

- **子 Agent / 合规等**：独立 session、独立 system → 与父 session cache 无关。
- **bash 输出 compress**（`tools/bash_runner.go`）：本地截断，非 LLM cache 问题。

---

## 4. 量化直觉（当前 config）

设主 turn 平均每步新增 `Δ` tokens，silent 阈值 `T_s=80k`，blocking `T_b=100k`：

```
累计 tokens
  │
100k├──────────────── blocking：同步 CompleteText（~80k+ miss 一次）+ apply 前缀重写
    │
 80k├──────────────── silent 区间：每条入站消息可能再触发 silent CompleteText
    │                 （若上次 silent 已结束且仍 ≥80k）
    │                 每次 ~80k miss
    │
    └─ 80k 以下：主 turn 前缀稳定时 cache hit 逐步升高
```

**频率放大器**：

- tool 密集任务：每步 `MaybeHandle` + 多步 `StreamChat`，silent 与主 turn **交替** 产生 miss。
- 阈值越高：单次 compress input 越大，**单次 miss 成本** 越高。
- 阈值越低：压缩更频繁，**前缀重写** 更频繁，主 turn cache **重置** 更频繁。

二者存在 **Pareto 权衡**，当前 80k/100k 偏向「少压缩次数、单次压缩极贵」。

---

## 5. 优化方向（按推荐顺序）

### P0 — 低成本、立刻减 miss 面积

#### 5.1 压缩与主 turn **分离模型或参数**

- 配置 `compression.model`（小模型 / 非 thinking）+ 可选独立 `base_url`。
- 压缩走 cheap 路径，**不计入** 主模型 cache 策略；避免 thinking token 与主模型 cache 槽位混淆。
- 实现：`Coordinator` 注入 `CompleteTextClient` 接口，不必与 turn `StreamChat` 共用同一 `RuntimeSettings`。

#### 5.2 Silent **去重 / 冷却**

- 同一 session 在 pending 已存在或 **刚 apply 后 N 秒 / M tokens 内** 不重复 `startSilentTask`。
- 在 `[T_s, T_b)` 区间避免「每条消息一次 80k miss 的 CompleteText」。
- 实现：`Coordinator` 增加 `lastCompressAt` / `lastAppliedFingerprint` 闸门。

#### 5.3 用 **真实 usage** 驱动阈值

- `messages_total_tokens` 维护为 SSE `usage.prompt_tokens`（或 hit+miss）而非仅 `EstimateMessageTokens`。
- 减少「估算 80k、实际 60k 仍压」或「估算未到、实际已超」的误触发。

#### 5.4 压缩输入与触发 **同一套 token 度量**

- `shouldCompress` 与 `buildHumanBlock` 对齐：要么压缩也用全文，要么阈值基于截断后估算。
- 避免误触发大额 CompleteText。

### P1 — 结构性减 miss（中等工作量）

#### 5.5 **增量 / 分段压缩**（滚动摘要）

- 不要每次把 `[0, lastAssistant)` 全量塞进 user prompt。
- 维护 **持久化 running summary**（SQLite 字段或首条 `user` 摘要消息）：
  - 仅将 **自上次摘要以来的 delta** 送压缩模型：`旧摘要 + delta → 新摘要`。
- 压缩请求 input 从 **O(阈值)** 降为 **O(Δ)**，cache miss 面积大幅下降。
- 与 Python 文档中「区间替换」兼容，但摘要消息 **稳定 id/位置**，利于主 turn prefix 仅尾部变化。

#### 5.6 **对齐 Python 的 compression plan**

- 实现 `_assistant_tool_pairs_complete` 等价逻辑：压缩区间不得切断 tool 轮次。
- 减少「压缩后主 turn 行为异常 → 重试 / 补消息」导致的额外 miss。

#### 5.7 主 turn **稳定 system 前缀**

- 将极少变化的静态段（`staticSystemPrompt`、环境、工作区说明）与 **易变段**（loaded skills、custom）分离：
  - 固定段尽量不变；
  - skills 变更时仅 miss 尾部（若 provider 支持 messages 内分段 cache；DeepSeek 主要认 **字节级前缀**）。
- 侧车 `promptcontext.Reader` 已有 mtime cache；避免无谓 touch 文件。

#### 5.8 压缩 **不单独造一条 user 巨型 prompt**

- 方案 A：用 **多轮 messages** 复用主 turn 已存在的 message 结构（相同 JSON 序列化），仅追加一条「请摘要以上」的 user；provider 可能对 **与上一条主 turn 相同的前缀** 命中 cache（需实测 DeepSeek）。
- 方案 B：使用 provider **Responses / 专用 summarize API**（若有）而非独立 chat 模板。

### P2 — 架构级（高收益、改动大）

#### 5.9 **库内摘要 / 无 LLM 压缩**

- 对 tool 输出、bash 日志等 **结构化截断**（已有 bash output compress）扩展到 history 层。
- 仅对「语义摘要」必要部分调 LLM，其余规则压缩。

#### 5.10 **Session 级 cache 键 / store**

- 本地保存 provider 返回的 `cache_id` 或最后 hit 前缀 hash（若 API 暴露）。
- 压缩前评估「是否值得」：若 miss 成本 > 不压缩继续对话，则推迟。

#### 5.11 Hook：`turn.before_compress`（见 [agent-hooks.md](./agent-hooks.md)）

- 允许策略插件：跳过 silent、只 blocking、改区间、改摘要 prompt。
- 便于 A/B 不同 cache 策略而无需 fork Coordinator。

---

## 6. 建议实施路线（本分支）

| 阶段 | 内容 | 预期效果 |
|------|------|----------|
| **M1 度量** | SSE 汇总 session 级 hit/miss；压缩前后各打 log；`/context` 暴露最近一次 compress input 估算 token | 可验证假设 |
| **M2 闸门** | silent 冷却 + pending 去重；可选压缩专用 model | 立即减少重复 80k miss |
| **M3 增量摘要** | running summary + delta 压缩 | 单次 compress input 从 80k → 数千级 |
| **M4 plan 对齐** | tool 对闭合 + 阈值与 buildHumanBlock 一致 | 正确性 + 少 retry miss |
| **M5 主 turn 前缀稳定** | system 分层、skills 变更隔离 | 提升压缩间隔内主 turn hit 率 |

---

## 7. 相关代码索引

| 主题 | 路径 |
|------|------|
| 压缩协调 | `node/internal/compression/coordinator.go` |
| 阈值与区间 | `node/internal/compression/plan.go` |
| 步前挂钩 | `node/internal/session/runtime_turn.go` |
| 主 turn LLM | `node/internal/turn/orchestrator.go` → `StreamChat` |
| 压缩 LLM | `node/internal/llm/openai.go` → `CompleteText` |
| Cache 统计 | `node/internal/llm/usage.go` |
| Token 估算 | `node/internal/llm/messageutil.go` → `EstimateMessageTokens` |
| 配置 | `shared/config/config.go` → `CompressionConfig` |
| 设计 Hook 点 | `docs/design/agent-hooks.md` |

---

## 8. 结论（一句话）

当前实现在 silent 阈值附近会周期性发起 **与主 turn 完全隔离、且 input 规模≈阈值** 的 `CompleteText`，几乎 **100% prompt cache miss**；压缩 apply 后又会 **重写 messages 前缀**，进一步 **清零** 主 turn 已积累的 prefix cache。优化核心不是「把阈值调低」，而是 **减少单次压缩 input 体积、降低 silent 重复频率、并与主 turn 共享可缓存的前缀结构**。
