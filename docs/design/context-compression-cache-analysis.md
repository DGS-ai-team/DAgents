# 上下文压缩与 Prompt Cache 命中率分析

> 分支：`feat/compression-cache-optimization`  
> 范围：Go Agent Node（`node/internal/compression`、`node/internal/turn`、`node/internal/llm`）  
> 配置示例：`silent_trigger_tokens: 80000`、`blocking_trigger_tokens: 100000`  
> DeepSeek 缓存机制：[上下文硬盘缓存](https://api-docs.deepseek.com/zh-cn/guides/kv_cache)

---

## 1. 背景：我们在优化什么

### 1.1 服务端视角：所有请求都是「续写」

在 LLM 服务端，**所有 chat 请求本质均为前缀续写**：将 **system prompt + messages + tools schema** 拼接为一段完整输入文本，再逐 token 自回归预测。因此 **prompt cache 的前缀单元不仅含 system 与 messages，也含 tools**（同一套 tool 定义须字节级一致，其后的 messages 尾部才可能在后续请求中命中）。

对本方案的直接推论：

- 侧车压缩要与主 turn **共享 cache**，须同时满足：**相同 system**、**相同 tools**、**相同 messages 前缀**（至 cut 点为止）。
- 压缩请求 **不能** 省略 tools 或换用 `tool_choice: none` 等会改变输入前缀的参数，否则即使 system/messages 一致也会 **整段 miss**。
- `RequestExtra`（如 thinking 开关）若进入请求体并改变序列化前缀，同样会破坏命中；侧车压缩应与主 turn **对齐**（待实测是否纳入前缀）。

DeepSeek 公开文档侧重 messages 多轮示例（[上下文硬盘缓存](https://api-docs.deepseek.com/zh-cn/guides/kv_cache)），但与本仓库对接的 **续写式** 服务端行为一致：**tools 计入前缀**。

### 1.2 Prompt Cache 行为摘要

- 同一 session、连续请求中，若 **system + messages 前缀 + tools** 与上一轮一致，仅 **messages 尾部** 新增，则重复部分可走 cache hit。
- DeepSeek 规则：后续请求须 **完整匹配** 已落盘的「缓存前缀单元」；尾部不同但存在公共前缀时，系统会落盘公共段供后续命中（见官方 [例一 / 例二](https://api-docs.deepseek.com/zh-cn/guides/kv_cache)）。
- **任意前缀变化**（system 改动、tools 列表/enrich 变化、中间 message 改写、压缩替换区间）都会使 **变化点之后** 的 token 变为 miss。

DAgents 当前有两类 LLM 请求：

| 类型 | 入口 | system | messages | tools |
|------|------|--------|----------|-------|
| **主 turn** | `Orchestrator.runOneStep` → `StreamChat` | `BuildSystemPrompt` | 完整 `history` | `tools.Definitions()` |
| **压缩摘要（M2 已落地）** | `runCompressionFlow` → `Summarize`（`StreamChat`） | **同** `BuildSystemPrompt` | `snapshot[0:end]` + ephemeral 尾部 | **同** `tools.Definitions()` |

改前（CompleteText + 独立 system + 无 tools + `buildHumanBlock`）的问题分析见 **§3.1**；M2 侧车已与主 turn **前缀三元组对齐**。

---

## 2. 当前实现（Go Node，M2 落地后）

### 2.1 触发时机

```
用户消息 / resume / tool 结果
  → runtime.runTurnStep(compressBefore=true)
  → sidecarPrefix = { SystemPromptForSession, ToolDefinitions }
  → compression.MaybeHandle(..., prefix)
  → （若通过阈值）runCompressionFlow → Summarize(StreamChat)
  → readyCompressions → applyReadyCompression（替换 messages 区间）
  → Orchestrator.runOneStep → StreamChat（主 turn）
```

- **子 Agent session 不压缩**（`!r.isChildSession()`）。
- **每条** 会 `compressBefore=true` 的入站消息都会调用 `MaybeHandle`（先写回 `readyCompressions`，再判定是否新开压缩任务）。
- **Human 消息**：`MaybeHandle` 在 append 本条 user **之前**执行；侧车 snapshot 不含即将写入的 user，主 turn 含。
- **POST `/compress`**：`runtime.compressContext` 传入相同 `sidecarPrefix`，走 `ForceBlocking`。

### 2.2 阈值判定

`plan.go` → `shouldCompress`：全文 `EstimateMessageTokens`；blocking 优先于 silent；须 `buildCompressionPlan` 成功才 `Should=true`。

### 2.3 压缩区间与合法性

`buildCompressionPlan`（`computePrefixClosure` 一次 O(n) + 合法边界）：

- **情况一**：`keep_following_assistant` — 边界为 tool / 无 tc assistant / **user 且下一条为 assistant**；tail 以 assistant 开头。
- **情况二**：`merge_next_user` — 结论 assistant 后紧跟 user，摘要与 tail user 合并写回。
- 多 `tool_call` 仅在该批**最后一个 tool** 处满足 `prefixClosed`；非法 messages 序列 → noop（不修复）。

### 2.4 侧车 LLM 请求

`sidecar.go`：`BuildSidecarChatRequest` + `Summarize`（与主 turn 相同 `Client` / adapter 出站路径）。

```text
system:  BuildSystemPrompt(session)     ← runtime.SidecarPrefix
tools:   tools.Definitions()            ← 同上
messages: clone(snapshot[0:end])
          + [情况一] synthetic assistant + user(summaryUserPrompt)
          + [情况二] user(summaryUserPrompt)
```

不再使用 `CompleteText`、`buildHumanBlock` 或二次序列化「待压缩块 + 后续」。

### 2.5 写回与度量

- `applyCompressionReplacement`：区间替换 + JSON fingerprint 防 stale。
- 压缩成功 SSE/API（`status=applied`）携带 [DeepSeek usage](https://api-docs.deepseek.com/zh-cn/api/create-chat-completion)：`prompt_tokens`（侧车输入）、`completion_tokens`（摘要输出）、`prompt_cache_hit_tokens` / `prompt_cache_miss_tokens`、`token_reduction_rate`。
- **对主 turn cache**：apply 仍重写 messages 前缀，下一轮主 turn 无法在 provider 侧复用压缩前 message 前缀（§3.2 仍成立）。

---

## 2A. 改前实现（CompleteText，仅供 §3 对照）

### 2A.1 触发时机（改前）

```
  → runCompressionFlow → CompleteText
```

### 2A.2 压缩区间（改前）

- 仅 `messages[0:lastAssistant]`，**无** tool 对闭合回退。

### 2A.3 payload（改前）

```text
system: summarySystemPrompt
user:   "待压缩文本块：" + buildHumanBlock(picked) + "；后续文本为：" + buildHumanBlock(tail)
```

`buildHumanBlock` 每条 content 截断 800 字符；阈值按未截断全文估算。

### 2A.4 写回（改前，与现网相同）

- 将 `[start, end]` 替换为一条 `role=user` 摘要；保留 `messages[end+1:]`。

---

## 2B. （原 §2 末段保留）主 turn cache 影响

**对主 turn cache 的影响**：替换发生在 **messages 前缀**，下一轮 `StreamChat` 的 messages 前缀与压缩前 **完全不同** → 主 turn 在 provider 侧 **无法** 复用压缩前积累的 message 前缀 cache。

（原 §2.5 内容，逻辑未变。）

---

## 3. Cache Miss 来源分解

### 3.1 改前：压缩专用请求（CompleteText）— 主要矛盾

> **M2 后**：侧车 `StreamChat` 与主 turn 共享 system+tools+messages 前缀，miss 区主要为 ephemeral 尾部（~数百 token）。本节保留改前定量直觉。

假设 `messages_total_tokens ≈ 80_000`（silent 刚触发）：

| 组成部分 | 相对主 turn | Cache 预期 |
|----------|-------------|------------|
| `summarySystemPrompt` | 与 `BuildSystemPrompt` **不同** | prefix **从 system 起即不共享** |
| tools | 主 turn 有完整 schema；`CompleteText` **无 tools** | **整段 tools 前缀 miss** |
| user 中「待压缩块」 | 约 `0 .. lastAssistant-1` 的序列化文本 | **全新**；体量 ≈ 阈值量级 |
| user 中「后续文本」 | 最后 assistant 及之后（通常较短） | 全新 |

结论：

1. **整次压缩推理几乎全是 miss**（system、tools、messages 三路均不对齐；user 内 ~80k 序列化占主体）。
2. **silent 在 `[80k, 100k)` 区间可能多次触发**（改前）：每次入站消息可能再开 `startSilentTask` → 又一次全量 miss。**M3 后**：`silent_cooldown` + pending 去重，apply 后 60s / +4000 tokens 内不重复侧车（见 §4）。
3. **blocking 在 ≥100k** 时同步压缩，用户感知延迟 + 一次大额 miss。
4. 压缩与主 turn **串行/并行** 均不帮助压缩请求本身命中 cache——除非 **故意** 让侧车请求与主 turn 共享相同的 **system + tools + messages 前缀**（见 §5）。

### 3.2 主 turn（StreamChat）— 压缩后的二次伤害

压缩 apply 之后：

- `messages` 前缀从「多轮 user/assistant/tool…」变为 **一条摘要 user**。
- 后续 turn 只能在 **新前缀** 上重新积累 cache；压缩前已付费的 prefix cache **对该 session 基本作废**。
- 若摘要极长，新前缀仍大；若摘要控制得好，主 turn 前缀变短，**长期** hit 率可能回升，但 **压缩当次** 的 miss 成本已发生。

### 3.3 主 turn — 与压缩无关的 cache 扰动

| 因素 | 位置 | 影响 |
|------|------|------|
| **System prompt 重建** | 新会话、压缩或其他 context boundary | frozen Skills catalog 元数据快照变化 → **变化点之后的 prefix miss**；普通目录变化由 `list_available_skills` 查询，不改写当前 prompt |
| **Tools schema** | 每步 `Definitions()` 随请求发送 | **续写模型下 tools 计入前缀**；列表/enrich/顺序变化 → 自 tools 段起 miss |
| **DeepSeek thinking** | `RequestExtra` | 一般不影响 messages prefix，但独立计费维度 |
| **流式增量** | 每 turn 仅尾部新增 assistant/tool | 理想情况下 **前缀 messages 不变则 hit**；tool 结果 append 只 miss 尾部 |
| **Token 估算 vs 真实 usage** | `EstimateMessageTokens` vs SSE `usage` | 阈值触发时机与 provider 真实 token 不一致，可能导致 **过早/过晚** 压缩，间接影响 hit 曲线 |
| **出站路径** | 主 turn 与侧车均经 `adapterClient.StreamChat` → `PrepareOutboundMessages` | M2 后一致；改前 `CompleteText` 绕过 adapter |

### 3.4 其他 LLM 调用

- **子 Agent / 合规等**：独立 session、独立 system → 与父 session cache 无关。
- **bash 输出 compress**（`tools/bash_runner.go`）：本地截断，非 LLM cache 问题。

---

## 4. 量化直觉（当前 config，M2+M3 落地后）

设主 turn 平均每步新增 `Δ` tokens，silent 阈值 `T_s=80k`，blocking `T_b=100k`：

```
累计 tokens
  │
100k├──────────────── blocking：同步侧车 StreamChat（前缀 hit + 尾部 miss）+ apply 前缀重写
    │
 80k├──────────────── silent 区间（M3 冷却后）：
    │                 · 首次达阈值：后台侧车 1 次（前缀 hit + 摘要 user miss）
    │                 · apply 后 60s 且 token 增量 <4k：跳过后续 silent（pending/冷却闸门）
    │                 · 无在跑任务且无 readyCompressions 时才新开侧车
    │
    └─ 80k 以下：主 turn 前缀稳定时 cache hit 逐步升高
```

**与改前对比**（§3.1）：

| 维度 | 改前 CompleteText | 现网 M2 侧车 | M3 冷却后 |
|------|-------------------|--------------|-----------|
| 侧车单次 miss 体量 | ~O(阈值) 全新前缀 | O(尾部 ephemeral) | 同左 |
| `[T_s,T_b)` silent 次数 | 每条入站消息可再触发 | 同左（未冷却时） | **至多 1 次/冷却窗** |
| blocking | 同步 ~80k+ miss | 前缀 hit + 尾部 miss | 同左 |

**频率放大器**（仍成立）：

- tool 密集任务：每步 `MaybeHandle` + 多步 `StreamChat`；M3 降低 **重复侧车**，不消除主 turn 自身增量 miss。
- 阈值越高：单次侧车 **前缀越长**（hit 区越大），但 apply 后前缀重写成本不变。
- 阈值越低：压缩更频繁，主 turn cache **重置**更频繁。

二者仍存在 **Pareto 权衡**；80k/100k 在 M2 后单次侧车成本已大幅下降，M3 主要抑制 silent **重复触发**。

---

## 5. 优选方案：前缀对齐的 StreamChat 压缩

本节为 **本分支首选实现方案**（已澄清的最终形态）。

### 5.1 核心思路与两种压缩区间

在每轮推理开始前（现有 `MaybeHandle` 钩子），若达到压缩阈值，由 `buildCompressionPlan` 判定 **情况一 / 情况二**（已实现于 `plan.go` + `apply.go`）：

| | 触发条件 | 压缩区间 | 侧车 ephemeral 追加 | apply 写回 |
|---|----------|----------|----------------------|------------|
| **情况一** | 边界为 tool / 无 tc assistant / **user→assistant**；`messages[end+1]` 为 **assistant**（可含 tool_calls） | `[leadingSystemSkip:end]`（生产 skip=0） | **assistant + user**（摘要指令） | `[summary user]` + **保留 tail 及之后** |
| **情况二** | 最后一条 **无 tool_calls 的 assistant** 后 **紧跟 user** | **含** 该结论 assistant | **仅 user**（摘要指令） | `[summary + "\n\n" + 原 user 合并为一条 user]` + rest |

共同约束：

1. 侧车 snapshot **不写入** session；主 turn 仍用原 messages 继续。
2. 侧车与主 turn 使用 **相同 `BuildSystemPrompt` + `tools.Definitions()`**；`summarySystemPrompt` 作为 **最后一条 user**。
3. 压缩区间内 tool 对须闭合；情况一 tail 须以 **assistant** 开头，情况二 tail 须以 **user** 开头；否则 `buildCompressionPlan` 返回 false。
4. 情况二 fingerprint 含 `[start:end+2]`（含待合并 user），防 stale apply。

示例（情况一）：

```text
… user → assistant(tool_calls) → tool → assistant(结论) → [侧车: +assistant +user]
apply: … → [summary user] → assistant(结论) → …
```

示例（情况二）：

```text
… user → assistant(结论) → user(新消息) → [侧车: +user]
apply: … → [summary + 新消息合并 user] → …
```

### 5.2 侧车压缩请求形态

```text
API:        StreamChat（与主 turn 相同 Client / adapter 出站路径，非 CompleteText）
system:     BuildSystemPrompt(session)              ← 与主 turn 完全相同
tools:      tools.Definitions()                     ← 与主 turn 完全相同（计入前缀，不可省略）
messages:   clone(snapshot[0 : end])                ← 情况一含至 tool 闭合；情况二含结论 assistant
            + [情况一] assistant: 「上下文过长，请准备摘要」
            + user: summarySystemPrompt 全文         ← 两种情况均有
RequestExtra: 与主 turn 一致（含 thinking 等）；可另设 max_tokens 上限（若不影响前缀键）
输出:       assistant 摘要正文 → pending → apply
```

**前缀对齐三元组**（缺一不可）：

```text
BuildSystemPrompt(session) + ToolsDefinitions() + PrepareOutboundMessages(snapshot[0:end])
```

仅 messages 尾部追加 `[可选 assistant] + user(summarySystemPrompt)` 为 **miss 区**。

**不再使用**：

- `buildHumanBlock` 序列化「待压缩块 / 后续文本」；
- `CompleteText(summarySystemPrompt, giantUserPrompt)` 双轨模板。

`summarySystemPrompt` 常量 **保留语义**，仅 **角色从 system 改为最后一条 user**：

```text
你是会话压缩助手。你会基于给定消息块生成结构化摘要，必须严格包含以下四段并使用中文：
1) 任务目标
2) 重要结论
3) 修改过的文件和资源
4) 下一步动作
…
```

#### 5.2.1 session history 是否含 `system`（P9 共识）

| 路径 | `role=system` 是否在 `messages` 中 |
|------|-------------------------------------|
| **生产**（`runtime.messages` / SQLite） | **否**。`Orchestrator.appendHistory` 仅写 user/assistant/tool；出站时 `llm.MessagesWithSystem` 临时 prepend system，**不落库**（`node/internal/llm/messages.go`） |
| **侧车 snapshot** | 与 session 同源，**不含** system；`SidecarPrefix.SystemPrompt` 单独携带 |
| **异常** | `history.Journal.InsertMessage` 可插入任意 role（仅测试/人工）；若 leading `system` 存在，`leadingSystemSkip` 跳过压缩区间前缀，apply 保留 |

因此压缩可压区间在生产环境恒为 `messages[0:end+1]`；P9 防御仅针对 journal 等异常写入。

### 5.3 与主 turn 的对比

| | 主 turn | 侧车压缩 |
|---|---------|----------|
| system | `BuildSystemPrompt` | **相同** |
| tools | `tools.Definitions()` | **相同** |
| messages | 完整 `history`（本步正常 mutate） | `snapshot[0:end]` + 尾部 1～2 条 **仅 API、不落库** |
| 写回 session | 是 | **否**（仅 pending 摘要） |

### 5.4 为何能命中前缀 cache

相对现状，侧车请求与上一轮（或同轮上一步）主 turn 的 **共享续写前缀** 为：

```text
BuildSystemPrompt(session)
+ ToolsDefinitions()                    // 与主 turn 同序、同 schema
+ PrepareOutboundMessages(snapshot[0 : end])
```

新增 miss 主要为 **messages 尾部**：

```text
[可选 assistant] + user(summarySystemPrompt)   // 约 200～400 token
```

对比：

| | 现状 CompleteText | 本方案 |
|---|-------------------|--------|
| system 与主 turn | 不同 | **相同** |
| tools 与主 turn | 无 tools | **相同 Definitions()** |
| ~80k 待压缩内容 | 重新序列化进 user 字符串 | **已在 messages 里** |
| 出站路径 | 绕过 `PrepareOutboundMessages` | **与主 turn 一致** |
| 预期 miss 规模 | ~80k + tools 段 | **~数百 token（尾部）+ 前缀 hit** |

**Tool loop 内** 多步 `StreamChat` 逐步增长 history，且 **每步 tools 相同** 时，上一步已落盘的 `system + tools + messages[0:k]` 与侧车 `snapshot[0:end]`（`end ≤ k`）高度重合，是 **最有利** 的 hit 场景。

### 5.5 可行性评估

| 维度 | 结论 | 说明 |
|------|------|------|
| 工程可实现性 | **高** | 改 `runCompressionFlow` + 注入 `SystemPromptForSession` 与 **ToolsDefinitions**；pending/apply/fingerprint 可复用 |
| Cache 收益 | **中高（需实测）** | 机制对齐 DeepSeek；不能假设 100% hit |
| 摘要质量 | **优于现状** | 全文进 context，无 800 字截断 |
| 主 turn 行为 | **高** | 侧车请求不污染 history |
| apply 后 cache | **仍会断** | 前缀替换问题本方案 **不解决** |

### 5.6 有利 / 不利场景

**有利（hit 预期较高）**

- Tool loop 内连续 `StreamChat`：上一步已落盘相同 **system + tools + messages** 前缀。
- **Blocking** 压缩：同步跑完再主 turn，snapshot 与刚结束的 history 时间接近，且 **同一时刻取同一套 tools**。
- **Silent** 与主 turn 并行：二者共享 **更早轮次** 已落盘前缀；各自 messages tail 不同，符合「公共前缀落盘」模型（**tools 须与各自请求时刻的 Definitions 一致**）。

**不利（hit 可能偏低，需 metrics 验证）**

| 因素 | 影响 |
|------|------|
| 缓存落盘延迟（官方：秒级） | 刚结束的主 turn 立刻发压缩，首轮 miss 可能仍偏多 |
| system 动态段变化（skills / 侧车） | system 变 → 整段前缀 miss；侧车须与 **同一步** 主 turn 取同一 builder 结果 |
| **tools 变化**（load skill、enrich 变更） | tools 段前缀断档；侧车与主 turn 须 **同一次** `Definitions()` 快照 |
| thinking / `RequestExtra` 与主 turn 不一致 | 若进入续写前缀则 miss；侧车应 **复用主 turn 相同 RequestExtra** |
| silent 与主 turn 同刻并发 | 互相当轮无法互 hit；且并发间 tools/skill 若变化则共享前缀缩短 |
| 侧车带 tools 时模型发起 tool_calls | 行为风险，**不能** 为防 tool_call 而改 tool_choice 破坏前缀；靠 user 指令约束，响应侧只取 **纯文本 content** 作摘要 |

**量化直觉（80k 阈值，保守估计）**

- 现状：压缩一次 ≈ **80k miss**
- 本方案（乐观）：**60k～75k hit + ~0.2k miss**
- 本方案（悲观）：公共前缀检测后 **40k～60k hit**（仍远好于全 miss）

必须用 SSE `prompt_cache_hit_tokens` / `prompt_cache_miss_tokens` 做 **M1 对比实验**。

### 5.7 实现要点

1. **`Coordinator` 注入与主 turn 相同的前缀来源**  
   - `SystemPromptForSession(sessionID) string`（对接 `orch.SystemPromptForSession`）  
   - `ToolDefinitions() []ToolDefinition`（对接 `tools.Definitions()`，与 **同一步** `runOneStep` 一致）  
   二者与 messages snapshot 共同构成可命中前缀。

2. **`CompleteText` → `StreamChat`**  
   必须走 `adapterClient.StreamChat` → `PrepareOutboundMessages` → DeepSeek `MarshalChatRequestMessages`；**tools 字段非空且与主 turn 相同**。

3. **`selectCompressRange` 增加 tool 对闭合回退**  
   与 Python `_assistant_tool_pairs_complete` 对齐；cut 不稳定会直接损害 cache 与摘要质量。

4. **删除 `buildHumanBlock` 压缩路径**  
   「后续文本」保留在 session 中，不参与侧车 API input。

5. **压缩行为约束（不改前缀键）**  
   - **禁止** `tools=nil`、`tool_choice: none`、独立压缩 model（与主 turn 不同 model 则无法共享 cache）等与主 turn 前缀不一致的优化。  
   - 在 `user(summarySystemPrompt)` 中明确要求 **仅输出摘要正文、不要调用工具**。  
   - 若响应含 `tool_calls`，侧车逻辑 **丢弃 tool_calls、仅取 content** 或视为压缩失败重试；**不** 为消 tool_calls 改请求前缀。  
   - `max_tokens`、temperature 若 provider 证明不影响前缀键可单独收紧；`RequestExtra` 默认与主 turn 一致。

6. **可选 synthetic assistant**  
   非必须；仅 `user(summarySystemPrompt)` 更短 tail。若保留 assistant，用陈述句而非疑问句。

### 5.8 仍存在的边界（方案不解决）

1. **apply 后 prefix 重写**：`[start,end]` → 一条摘要 user，下一轮主 turn cache 仍重置（所有区间替换式压缩的共性）。
2. **silent 重复触发 / stale fingerprint**：主 turn 继续 append 时 snapshot 可能过期 → 仍需 **silent 冷却 / pending 去重**（§6.2）。
3. **角色叠加 / tool_calls 误触发**：agent system + tools 仍暴露完整能力；靠 user 指令约束摘要，响应侧忽略 tool_calls。
4. **非 DeepSeek provider**：无 prompt cache 时方案仍 **不退化**（至少去掉 80k 重复序列化），收益降为质量与一致性。

### 5.9 与现有流程的衔接

```text
MaybeHandle
  → selectCompressRange（+ tool 对回退）→ start, end
  → compressMsgs = clone(snapshot[0:end]) + user(summarySystemPrompt)
  → toolDefs = ToolsDefinitions()            // 与主 turn 同快照
  → StreamChat({ SystemPrompt, Tools: toolDefs, Messages: compressMsgs })
  → pending.Content = result.Content       // 仅取 assistant 文本，忽略 tool_calls
  → tryApply / apply（逻辑不变）

主 turn（同一步或下一步）:
  → StreamChat({ SystemPrompt, Tools: toolDefs, Messages: 完整 history })
```

---

## 6. 其他优化方向（补充）

### P0 — 低成本、与 §5 正交

#### 6.1 Silent **去重 / 冷却**

- 同一 session 在 pending 已存在或 **刚 apply 后 N 秒 / M tokens 内** 不重复 `startSilentTask`。
- 在 `[T_s, T_b)` 区间避免重复侧车压缩。
- 实现：`Coordinator` 增加 `lastCompressAt` / `lastAppliedFingerprint` 闸门。

#### 6.2 用 **真实 usage** 驱动阈值

- `messages_total_tokens` 维护为 SSE `usage.prompt_tokens`（或 hit+miss）而非仅 `EstimateMessageTokens`。
- 减少误触发。

#### 6.3 压缩 **独立模型**（与 §5 cache 目标冲突）

- 换 model / 关 thinking / 省略 tools 均可降本或简行为，但在 **续写前缀含 system+messages+tools** 的模型下 **无法** 与主 turn 共享 cache。  
- 若启用 §5，**不应** 再配 `compression.model` 或 `tool_choice: none`；降本只能走 silent 冷却、增量摘要等 **不改变侧车前缀三元组** 的手段。

### P1 — 中长期

#### 6.4 **增量 / 滚动摘要**

- 维护 running summary，仅压缩 delta：`旧摘要 + Δ → 新摘要`。
- 进一步降低侧车 input 与 apply 对 prefix 的扰动；与 §5 **可叠加**。

#### 6.5 主 turn **稳定 system 前缀**

- 静态段与易变段（skills、custom）隔离；减少 system 漂移导致的整段 miss。

### P2 — 架构级

#### 6.6 库内规则压缩 + 局部 LLM 摘要

#### 6.7 Hook：`turn.before_compress`（见 [agent-hooks.md](./agent-hooks.md)）

---

## 7. 建议实施路线（本分支）

| 阶段 | 内容 | 状态 |
|------|------|------|
| **M1 度量** | SSE/API `prompt_cache_*`；`last_compression` + 结构化日志 | ✅ 长 session 实测待做 |
| **M2 前缀对齐压缩** | §5 + §10 步骤 1–6 | ✅ |
| **M3 闸门** | `silent_cooldown.go`：pending 去重 + 60s/+4k tokens 冷却 | ✅ |
| **M4 增量摘要** | running summary + delta（可选） | 未开始 |
| **M5 主 turn 前缀稳定** | system 分层、skills 变更隔离 | 未开始 |

---

## 8. 相关代码索引

| 主题 | 路径 |
|------|------|
| 压缩协调 | `node/internal/compression/coordinator.go` |
| 侧车请求 | `node/internal/compression/sidecar.go` |
| 阈值与区间 | `node/internal/compression/plan.go` |
| 步前挂钩 | `node/internal/session/runtime_turn.go` |
| system 注入点 | `node/internal/session/runtime.go` → `orch.SystemPromptForSession` |
| 主 turn LLM / tools | `node/internal/turn/orchestrator.go` → `StreamChat` + `tools.Definitions()` |
| 压缩 LLM | `node/internal/compression/sidecar.go` → `Summarize`（`StreamChat`）；`llm/openai.go` 仍保留 `CompleteText` 供其他调用方 |
| 出站 adapter | `node/internal/llm/client_adapter.go` → `PrepareOutboundMessages` |
| Cache 统计 | `node/internal/llm/usage.go` |
| Token 估算 | `node/internal/llm/messageutil.go` → `EstimateMessageTokens` |
| 配置 | `shared/config/config.go` → `CompressionConfig` |
| 压缩与 session 状态 | [handbook/04-能力与策略.md](../handbook/04-能力与策略.md) §6 |
| 设计 Hook 点 | `docs/design/agent-hooks.md` |

---

## 9. 实现结构：局部重构（非推倒重来）

### 9.1 结论

**不必整体重构** `compression` 模块或把压缩并入 `turn`。新方案改的是 **侧车 LLM 请求形态**，不是 **何时压缩 / 如何 apply**。下列分层 **保留**；仅替换 CompleteText 专用路径并 **补全前缀依赖**。

| 层级 | 处置 | 说明 |
|------|------|------|
| `Coordinator` 状态机（task / pending / silent / blocking） | **保留** | 与 cache 方案正交 |
| `MaybeHandle` 挂钩（`runtime_turn` 步前） | **保留** | 触发时机仍合理 |
| pending + fingerprint + 区间 replace apply | **保留** | stale 仍校验 `messages[start:end+1]` |
| `shouldCompress` 阈值判定 | **保留** | M3 可换真实 usage |
| `runCompressionFlow` + `CompleteText` + `buildHumanBlock` | **替换 / 删除** | 与新侧车形态不兼容 |
| `Coordinator` 仅依赖 `llm.Client` | **不够** | 须传入与同一步主 turn 一致的 system + tools |

**不需要**：改动 session 队列模型、apply 语义、`ForceBlocking` / POST `/compress` 契约、将 compression 迁入 `turn` 包。

### 9.2 现状问题

```text
compression/
  coordinator.go   ← 协调 + LLM 调用 + CompleteText prompt 拼装耦合
  plan.go          ← 区间 + buildHumanBlock（CompleteText 专用）
  fingerprint.go   ← 仅 snapshot messages
```

`runCompressionFlow` 隐含：压缩 = 小 system + 巨型 user 字符串；snapshot 只需 `[]llm.Message`。新方案要求 **续写前缀三元组**（system + tools + messages 前缀）与主 turn 对齐。

### 9.3 目标包结构（M2 后，已实现）

```text
compression/
  coordinator.go      协调；evaluateCompression → sidecar.Summarize
  sidecar.go          SidecarPrefix、BuildSidecarChatRequest、Summarize
  plan.go             evaluateCompression、buildCompressionPlan、leadingSystemSkip
  apply.go            applyCompressionReplacement
  silent_cooldown.go  shouldStartSilent、冷却闸门（M3）
  metrics.go          attachCompressionUsageMetrics
  last_compression.go LastCompression、GET /context / 日志
  fingerprint.go      messages snapshot / 区间 fingerprint
```

**依赖方向**（避免环依赖）：

```text
session/runtime_turn → compression, turn
compression → llm, stream, tools
compression ↛ turn          // 前缀由 runtime 传入，compression 不 import turn
```

### 9.4 核心类型（已实现）

```go
type SidecarPrefix struct {
    SystemPrompt string
    Tools        []tools.ToolDef
}

// silent 任务启动时冻结 messages + prefix；End/SidecarAppend 由 compressionPlan 传入 runCompressionFlow
type SidecarInput struct {
    SidecarPrefix
    Messages      []llm.Message
    End           int
    SidecarAppend sidecarAppendMode
}
```

- **fingerprint** 针对 `Messages[leadingSystemSkip:end+1]`（生产路径 skip=0）；tools/system 由 `SidecarPrefix` 单独携带。
- **silent goroutine** 须冻结完整 snapshot + 当时 `SidecarPrefix`，不能仅拷贝 messages。

### 9.5 与主 turn 前缀单一来源（可选，M2 末或 M5）

在 `Orchestrator` 增加薄封装，供 `runOneStep` 与 `runtime_turn` 共用：

```go
func (o *Orchestrator) LLMStepPrefix(sessionID string) (system string, tools []tools.ToolDef)
```

避免 system / tools 两处各自组装导致漂移。

### 9.6 与 Python 栈差异

Python `SummaryContextCompressionRuntime` 摘要路径 **无 tools**；Go 为续写式 cache **必须带 tools**。此为 **有意的 Go 侧差异**，不为对齐 Python 而省略 tools。

---

## 10. 分步修改清单（M2）— 历史实施记录

> **状态**：步骤 1–8 与 §11 优化清单 **均已落地**（2026-06）。本节保留实施顺序与验收口径，供回溯与 code review；**现行行为以 §2 / §5 与代码为准**，文中旧 API 名（如 `selectCompressRange`、`CompleteText`）仅作对照。

按顺序实施；每步完成后跑 `go test ./node/internal/compression/... ./node/internal/session/...`。

### 步骤 1：plan — 前缀闭合与两种 apply 模式 ✅

**文件**：`node/internal/compression/plan.go`、`apply.go`

- `computePrefixClosure` O(n) 预计算 + `buildCompressionPlan` 情况一/二。
- 合法边界：tool（须整批闭合）、无 tc assistant、user→assistant。
- 非法 messages 序列 → noop；**无** `buildHumanBlock` / CompleteText 专用格式化。

**验收**：`plan_test.go` / `apply_test.go` 覆盖 tool 多轮、merge、非法序列；包内无 `buildHumanBlock`。

---

### 步骤 2：sidecar — 侧车请求组装与 StreamChat 摘要 ✅

**文件**：`node/internal/compression/sidecar.go`

- 定义 `SidecarPrefix`、`SidecarInput`（见 §9.4）。
- `BuildSidecarChatRequest(in SidecarInput, summaryUserPrompt string) llm.ChatRequest`：
  - `SystemPrompt` = `in.SystemPrompt`
  - `Tools` = `in.Tools`（**非 nil**，与主 turn 相同）
  - `Messages` = `clone(in.Messages[:end+1])` + 可选 synthetic assistant + `user(summaryUserPrompt)`
- `Summarize(ctx, client, req) (content string, usage llm.Usage, err error)`：
  - 调用 `client.StreamChat`（走 adapter 出站路径，**非 CompleteText**）
  - handler 只收 delta / usage，**不向 Hub 推送** assistant 流
  - 响应含 `tool_calls` 时 **忽略 tool_calls，仅取 `Content` 文本**；content 为空则失败
- 将原 `summarySystemPrompt` 常量移至 sidecar 或 `prompt.go`，作为 **user 正文** 引用。

**验收**：sidecar 单测断言 `ChatRequest` 含 tools、messages 前缀长度、末条 user 为摘要指令。

---

### 步骤 3：turn — 暴露与 runOneStep 一致的前缀 ✅

**文件**：`node/internal/turn/orchestrator.go`

- 新增 `ToolDefinitions() []tools.ToolDef`（或 `LLMStepPrefix(sessionID) (system, tools)`）。
- 实现与 `runOneStep` 内 `buildSystemPrompt` + `o.tools.Definitions()` **完全相同**。

**验收**：orch 单测或现有测试编译通过；与 `runOneStep` 无第二套 tools 来源。

---

### 步骤 4：coordinator — 接入侧车，扩展 MaybeHandle 参数 ✅

**文件**：`node/internal/compression/coordinator.go`

- `MaybeHandle` / `ForceBlocking` 增加参数 `prefix SidecarPrefix`（由 runtime 传入）。
- `runCompressionFlow` 改为：`evaluateCompression` / `buildCompressionPlan` 一次 → `SidecarInput` + `compressionPlan` → `Summarize`（**非 CompleteText**）→ `readyCompressions`。
- `startSilentTask`：冻结 `SidecarInput`（messages + prefix）与 `compressionPlan`；goroutine 内 `runCompressionFlow`。
- 删除 `buildHumanBlock` / `CompleteText` / 巨型 user prompt 拼装。

**验收**：`coordinator_test.go` 改为 mock `StreamChat`；断言 **未调用** `CompleteText`；blocking / silent / ForceBlocking / stale 用例仍通过。

---

### 步骤 5：session — 步前传入 SidecarPrefix ✅

**文件**：`node/internal/session/runtime_turn.go`、`runtime.go`

- 在 `MaybeHandle` / `ForceBlocking` 调用前：

```go
prefix := compression.SidecarPrefix{
    SystemPrompt: r.orch.SystemPromptForSession(r.session.ID),
    Tools:        r.orch.ToolDefinitions(),
}
r.compression.MaybeHandle(parent, r.session.ID, r.agentID, r.hub, &r.messages, prefix)
```

- `compressContext`（POST `/compress`）同步传入相同 `prefix`。

**验收**：集成路径编译通过；子 session 仍不压缩。

---

### 步骤 6：文档与测试收尾 ✅（本步）

**文件**：

- `node/internal/compression/README.md`、`REFERENCE.md`
- `docs/design/context-compression-cache-analysis.md`
- `node/internal/api/server_test.go`（compress API 仍兼容 `ForceResult`）

- 全量：`go test ./node/internal/compression/... ./node/internal/session/... ./node/internal/turn/...`
- 压缩路径无 `CompleteText` 调用（仅 test mock 实现 `llm.Client` 接口）。

**验收**：相关包测试绿；文档与实现对齐。

---

### 步骤 7（M1）：cache 度量 — 部分完成

- ✅ 压缩成功 SSE/API 携带 `prompt_cache_hit_tokens` / `prompt_cache_miss_tokens`（`metrics.go`）。
- ✅ 结构化日志 + `GET /context` 的 `last_compression` 暴露最近一次成功压缩 usage。
- ⏳ 真实长 session 对比改前 hit 比例（需线上/压测实测）。

---

### 步骤 8（M3）：silent 冷却 ✅

**文件**：`node/internal/compression/silent_cooldown.go`、`coordinator.go`

- `readyCompressions` pending 去重：有待写回摘要时不重复 `startSilentTask`。
- `lastCompressAt`（`silentCooldownState.lastAppliedAt`）+ token 增量闸门：apply 后 **60s 内** 且 **messages 估算 token 增量 < 4000** 时不重复 silent，避免 `[T_s, T_b)` 区间每条入站消息再开侧车。
- blocking 不受冷却影响；`CancelSession` 清除冷却状态。

---

### 依赖关系简图

```text
步骤1 plan
  ↓
步骤2 sidecar
  ↓
步骤3 orch 暴露前缀 ──→ 步骤4 coordinator
                          ↓
                       步骤5 runtime 传参
                          ↓
                       步骤6 测试 + 文档
                          ↓
                       步骤7 度量（M1）
                          ↓
                       步骤8 闸门（M3）
```

---

## 11. 后续优化清单（M2 步骤 1–8 完成后一并处理）

> **状态**：§11.1–11.2 P1–P9 已落地。

### 11.1 plan / apply 层

| # | 项 | 说明 |
|---|-----|------|
| P1 | **单次 `buildCompressionPlan`** | ✅ `evaluateCompression` 一次 plan + 向下传递 |
| P2 | **合并边界判定** | ✅ `isSelectableCompressEnd` |
| P3 | **`selectCompressRange` 收敛** | ✅ 删除；`compressionSlice` 内联 |
| P4 | **`compressionPlan.Start` 字段** | ✅ 删除（区间恒自 0） |
| P5 | **情况二冗余检查** | ✅ 仅 `isSelectableCompressEnd(..., "user")` |
| P6 | **`assistantToolPairsComplete`** | ✅ 测试改用 `prefixClosed` |

### 11.2 coordinator 层

| # | 项 | 说明 |
|---|-----|------|
| C1 | **删除 legacy CompleteText 路径** | ✅ 步骤 4 已完成 |
| C2 | **`ApplyMode==""` fallback** | ✅ 删除；`runCompressionFlow` 恒写入 `ApplyMode` |
| C3 | **`SidecarAppend` 接入 LLM** | ✅ `sidecar.go` / `BuildSidecarChatRequest` |

### 11.3 文档与注释

| # | 项 | 说明 |
|---|-----|------|
| D1 | **`plan.go` 顶部注释** | ✅ 见 `buildCompressionPlan` |
| D2 | **§5.1 情况一表述** | ✅ 已补 user→assistant 边界 |
| D3 | **§3 / 步骤 1 描述** | ✅ 已改为 `computePrefixClosure` |
| D4 | **`REFERENCE.md` / `README.md`** | ✅ 步骤 6 已对齐 |

### 11.4 边界行为（按需，非阻塞）

| # | 项 | 说明 |
|---|-----|------|
| P8 | **`shouldCompress` 返回值** | ✅ `compressDecision` 文档 + `HasPlan`；达阈值不可压时 `TriggerLevel` 保留档位 |
| P9 | **history 含 `system`** | ✅ 共识：生产不落库；`leadingSystemSkip` 防御 journal 异常写入 |

### 11.5 完成自检

- [x] 主路径（步骤 1–6）验收通过
- [x] 无 legacy / sidecar 双轨并存
- [x] 步骤 7 日志 / GET context（长 session 实测待做）
- [x] 步骤 8 silent 冷却
- [x] 本清单 P1–P6、C2 逐项勾选
- [x] `go test ./node/internal/compression/...` 绿

---

## 12. 结论

**M2 已落地**（`feat/compression-cache-optimization`）：侧车压缩与主 turn 共享 **system + tools + messages 前缀** 的 `StreamChat`；改前 `CompleteText` + `buildHumanBlock` 路径已删除。压缩当次 miss 主要集中在 ephemeral 尾部（摘要 user / 可选 assistant），不再整段 ~阈值 miss。

**仍存在的结构性限制**：apply 替换 `messages` 前缀后，主 turn 下一轮仍无法复用压缩前的 message 前缀 cache。步骤 8（silent 冷却）与 §6 增量摘要等用于降低重复侧车频率与 apply 扰动。

**改前问题**（对照 §2A / §3.1）：silent 阈值附近周期性 `CompleteText` 几乎 100% prompt cache miss；详见历史分析章节。
