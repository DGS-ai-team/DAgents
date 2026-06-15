# 工具链上下文成本优化 — 完整分析

> 分支：`feat/tool-context-cost-optimization`  
> 范围：Go Agent Node 全内置工具（`node/internal/tools`、`node/internal/turn` 工具结果写回、编排 dispatch）  
> 与 [context-compression-cache-analysis.md](./context-compression-cache-analysis.md) 的关系：**压缩降 history 体量**；本专题降 **tool loop 内无效 LLM 往返、tool 结果膨胀、schema 前缀扰动** — 二者 **正交、应叠加**。计费动机与 cache 策略见 **§1.2**。

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

### 1.2 计费模型：为何要做上下文管理、为何要抬 cache 命中率

本专题与 [context-compression-cache-analysis.md](./context-compression-cache-analysis.md) 的出发点相同：**Agent 的账单 = 多次 LLM 请求的 token 消耗 × 单价**。以生产常用的 [DeepSeek-V4-Pro](https://api-docs.deepseek.com/zh-cn/quick_start/pricing) 为例（2026-06 公示价）：

| 计费项 | V4-Pro 单价 | 倍率（相对命中 input） |
|--------|-------------|------------------------|
| 输入（[缓存命中](https://api-docs.deepseek.com/zh-cn/guides/kv_cache)） | **0.025 元** / 百万 tokens | 1× |
| 输入（缓存未命中） | **3 元** / 百万 tokens | **120×** |
| 输出 | **6 元** / 百万 tokens | 240× |

扣费规则：`费用 = token 消耗量 × 模型单价`（见 pricing 页）。因此 **同一 token 被 replay 时，命中与未命中的单价可差两个数量级** — 这是「既要管上下文、又要抬 cache 命中率」的直接经济原因。

#### 1.2.1 上下文管理要解决的三个账单项

| 账单驱动 | 机制 | 本分支对策（工作流） |
|----------|------|----------------------|
| **多轮 LLM 往返** | 每次 tool loop 都是一次完整 `StreamChat`；低信息调用（status 轮询、重复 read）仍产生 **output** 与新一轮 input | **WS1** 引导 async 回灌；**WS6** 拦截重复 tool call；**WS2** 可选 status wait |
| **tool 结果写入 history** | 大段 bash/grep/read 正文进入 messages，**放大后续每轮 input**；单条过长还推高压缩频率 | **WS3** `tool.after_each` 落盘 + history 摘要（token budget） |
| **tools schema 前缀漂移** | `load_skills` enrich 等使 **整段 tools JSON 每轮 miss**，前缀 P（常 **数千～上万 token**）按 3 元/M 重付 | **WS4** enrich 瘦身、稳定 `Definitions()` |

**核心**：上下文管理不是「省磁盘」，而是 **减少无效 turn、压低每条 message 进 history 的体积、让可缓存前缀尽量稳定** — 三者共同决定 input miss 量、input hit 量、以及 output 轮数。

#### 1.2.2 为何要抬 cache 命中率

[Prompt Cache / KV cache](https://api-docs.deepseek.com/zh-cn/guides/kv_cache) **不改变** turn 次数，也 **不减少** 必须新增的 assistant/tool tail；它只降低 **与上一轮 input 相同的前缀** 的计费单价（3 元/M → 0.025 元/M）。

典型 Agent 请求前缀 **P** = system + **全量 tools schema** + 用户任务句（全工具启用时常 **~10k tokens** 量级）。每多一轮 tool loop，P 都会在下一请求中 **再次出现**。若前缀稳定且可命中：

- 多付的主要是 **每轮新增 tail**（miss，3 元/M）和 **completion**（6 元/M）；
- 若前缀因 enrich 漂移、tools 列表变化而 **整段 miss**，则 P 在每轮按 **3 元/M** 重计 — 与压缩专题中的 cache 断档 **同质**，且在本专题中由 **轮询 / 多步 tool** 放大。

**结论**：抬 cache 命中率与「减少轮数、缩小 tool 结果」**叠加** — 前者降 replay **单价**，后者降 replay **量**与 **output**；只做其一，账单仍可能偏高。

#### 1.2.3 量化示例：分页 read 与 cache 的乘数效应

下列为 **§3.2.3** 的货币化动机摘要（完整路径对比见该节）。场景：读完 **10000 汉字**（≈6000 tokens），工具单页 **2000 字**（≈1200 tokens/页），固定前缀 **P ≈ 10000 tokens**（[token 粗算](https://api-docs.deepseek.com/zh-cn/quick_start/token_usage)：汉字 ×0.6）。

| 路径 | LLM 往返 | 各次 input 中文档 token 累计计数 |
|------|----------|----------------------------------|
| **A. 顺序 5 页** | 6 次 | **18000**（三角累加） |
| **B. 1 次读完** | 2 次 | **6000** |

在 V4-Pro 价下（符号：O_r=每轮 read 的 completion≈150，O_f=收尾≈500，M=tool 头≈80）：

| 场景 | A（5 页） | B（1 次） | 差额 |
|------|-----------|-----------|------|
| **① 高 cache hit**（前缀 P 按 0.025 元/M，仅新增 tail miss） | **≈ 0.061 元** | **≈ 0.053 元** | **+15%**（~0.008 元/次） |
| **② 全 miss**（前缀漂移 / 无 cache，各次 **全额 input** 按 3 元/M） | **≈ 0.252 元** | **≈ 0.083 元** | **≈ 3.0×**（~0.17 元/次） |

从单价可读出的 **设计动机**：

1. **单次 toy 任务**差额可很小（分～角），但 **长会话 × 十数轮 tool × 重复轮询** 会线性放大；观测到的「单次任务十数次 LLM」即此结构。
2. **cache 把「replay 计数」与「replay 账单」拆开**：18000 vs 6000 的 input **计数差**在命中价下货币影响微弱，在 **全 miss** 下接近 **3×** — 故 **WS4 稳 schema** 与压缩侧 cache 策略 **必须同做**。
3. **分页本身**（必要防线）带来的主要是 **多轮 completion** 与延迟；**WS3** 管「单页仍过长」，**WS6** 管「不该再读/再 poll」，而非取消分页。
4. **轮询**（如 10 次 `background_job_status`）在 ② 类假设下，每轮重付 P+history，成本远高于 ① — **WS1+WS6** 的优先级由此而来。

```text
优化总公式（本分支）：
  账单 ≈ Σ( input_miss × 3‰ + input_hit × 0.025‰ + output × 6‰ )
  降账单 → 少 turn（WS1/WS6/WS2）+ 小 tool 写入（WS3）+ 稳前缀多 hit（WS4）+ 可观测（WS5）
  （‰ = /10⁶；与 §1.2.3 表内用法一致）
```

### 1.3 Agent turn 的成本模型（轮询场景）

Go Node 的 LLM 调用以 **turn loop** 为单位：每次模型输出 tool_calls → 执行工具 → 将 **assistant + tool** 消息 append 到 `history` → 再次 `StreamChat`。

- **每一次** 低信息密度 tool 调用（如瞬时 `background_job_status`），只要模型 **主动发起新一轮推理**，就是一次完整 LLM 请求（§1.2.1）。
- 请求 input 含 **整段 history** + tools schema；上下文较长时单次 input 可达数万 token。
- [Prompt Cache](https://api-docs.deepseek.com/zh-cn/guides/kv_cache) 命中时，重复前缀按 **0.025 元/M** 计（§1.2.2）；**新增 assistant/tool** 与 **completion** 仍按 miss / 输出价计费，多次轮询 **线性放大** output 与 tail miss。

### 1.4 优化总目标

```text
在不大改 Agent 能力边界的前提下：
  1. 减少「低信息密度」的 LLM 往返（轮询、重复 read、不必要的 status）→ 降 turn 与 output
  2. 控制写入 history 的 tool 结果体积（tool.after_each 落盘摘要）→ 降 tail miss 与压缩压力
  3. 稳定 tools schema 前缀（减少 enrich 漂移）→ 抬 cache 命中率，降 P 的 miss 重付
  4. 可度量：每任务 tool_turns、tool_result_chars、status_poll_count（WS5）→ 验证落在 §1.2.3 ① 而非 ②
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
| read_file / grep / search_replace / glob | offset/limit + **token** 上限；**编码检测+缓存**（§3.2.2 阶段 2）；**阶段 3** spill | 顺序分页 multi-turn（§3.2.3，设计取舍） |
| search_replace diff | 输出 diff 摘要 | 大 diff 仍 long |
| agent_invoke / agent_discover | 对端全文 / 发现列表 JSON | **§3.2.4** 落盘 + 摘要（已落地） |

#### 3.2.0 tool_result 压缩全链路（现网）

| 阶段 | 位置 | 作用对象 | 行为 |
|------|------|----------|------|
| **Tool 清洗** | `bash_compress.sanitizeBashStream` | bash stdout/stderr | 去 ANSI、合并重复行；**不再**在此截断长度 |
| **Tool 分页/上限** | `fs_*` | read/grep/glob 等 | **token** 窗口（DeepSeek 粗算）+ 行/命中分页 + 翻页 hint |
| **Job preview** | `job_registry` | status 终态 preview | 2000 bytes；完整靠 async 回灌 |
| **`tool.after_each`** | `hooks.ToolResultPackageHook` → `toolresult.Package` | 配置内工具（**bash + fs + a2a**） | 超长：落盘 + history 头尾摘要 + `read_file` hint |
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
  → 若估算 tokens > hooks.tool_result.spill_threshold_tokens（默认 12000）：
       落盘 fs_root/tool_outputs/<session>/<tool_call_id>.txt（目录固定）
       ForHistory = 头 + ...（已省略约 N tokens，完整输出已写入 "path"，请 read_file 分页）... + 尾
  → async 回灌同路径（ForClient 全文进 SSE，ForHistory 摘要进 tool message）
```

> **`spill_threshold_tokens` 语义**：`tool.after_each` 对下方 **`tools` 列表中每个工具** 单独判定；超过阈值才落盘摘要。**不是** bash_run 专用，**也不是**整段 session history 的总 token 上限。fs 组另有工具内单页上限（read/grep 等默认 3000 tokens，见 §3.2.2），两层分工：工具内控单页体积，Hook 控写入 history 的最终体积。

**配置**（`config.yaml`）：

```yaml
hooks:
  tool_result:
    enabled: true
    spill_threshold_tokens: 12000   # 单条 tool 结果触发落盘摘要的阈值
    tools:                          # 作用于列表内每个工具
      - bash_run
      - read_file
      - grep_file
      - grep_files
      - search_replace
      - glob_files
      - agent_invoke
      - agent_discover
tools:
  bash_compress:
    enabled: true   # 仅清洗，不截断
```

**代码索引**：`toolresult/`、`hooks/builtin_tool_result.go`、`turn/tool_router.go`（`splitToolResult`）、`turn/tool_result_messages.go`（async）。

#### 3.2.2 WS3 · fs 组

**阶段 1（已落地）**：`read_file` / `grep_*` 接入 `tool.after_each` 落盘摘要；单页 **3000 tokens** 预算。

**阶段 2（已落地）**：**路径级文件编码检测 + 缓存** — 减少错编码乱码导致的无效重读（见下），**不做** read 结果 hint / short_circuit 去重。

**问题**：错编码读出的乱码整段进入 history（**miss 价**），模型再换 `encoding` 重读 → 又多一轮 tool + LLM；比「注意力丢失后再读同一页」更浪费 token。

**方案**（`node/internal/tools/fs_encoding*.go` + `Registry` 路径缓存）：

| 步骤 | 行为 |
|------|------|
| **选用优先级**（未传 `encoding`） | ① 路径缓存（**mtime 一致**）→ ② 字节检测 → ③ `tools.file_encoding` / 平台默认 |
| **显式 `encoding` 参数** | 最高优先；成功后 **写入缓存**（path + mtime） |
| **检测** | UTF-8 BOM → `utf8.Valid` 打分 → gb18030/gbk 试解码 + `textDecodeScore`；取最高分；与配置同为 gb 族时对齐为配置标签 |
| **解码** | `decodePathFileContent`：**禁止** gbk/gb18030 失败时静默 fallback utf-8 |
| **读结果 header** | `文件编码` + `编码来源`（参数/缓存/检测/配置）；短文本用人均分判乱码，疑似时 `编码提示` |
| **写/替换/grep** | `readTextLinesAt` / `resolveWriteEncoding` 同路径默认沿用缓存编码，读写一致 |
| **失效** | 文件 **mtime** 变化 → 丢弃该 path 缓存，重新检测 |
| **不做** | 同参数 read 结果缓存；path 级 HITL；read dedup hint |

```text
read_file（无 encoding）
  → Stat mtime → 查 pathEncodingCache
  → miss：读 raw bytes → detectEncoding → decode（禁止 gbk 失败时静默 utf-8 兜底）
  → header 标明编码与来源；成功后 rememberPathEncoding
write_file / search_replace / grep_*
  → resolvePathEncoding 同路径默认取缓存
```

**状态**：阶段 1–3 均已落地（编码：`fs_encoding_detect.go`、`fs_path_encoding.go`；spill 默认工具含 `search_replace`、`glob_files`）。

**阶段 3**：`search_replace` / `glob_files` 接入 `tool.after_each`；工具内 token 上限（replace 整段 2000 tokens、glob 单页 3000 tokens）。

#### 3.2.4 WS3 · a2a 组（已落地）

**工具**：`agent_invoke`（对端 `result_text`）、`agent_discover`（peer 列表 JSON）。

**锚点**：与 bash/fs 相同 — `tool.after_each` + `toolresult.Package`；超长写入 `fs_root/tool_outputs/`，history 头尾摘要 + `read_file` hint；SSE 仍全文。

```text
agent_invoke / agent_discover Execute
  → ForClient = 全文 → SSE
  → tokens > spill_threshold_tokens → 落盘 tool_outputs/<session>/<tool_call_id>.txt
```

**说明**：现网 Go Node 仅上述两个 A2A 工具（`manage.enabled` 时注册）；无工具内截断，与 `bash_run` 一致。**WS2**（子 Agent status long-poll）本分支**不做**。

#### 3.2.3 分页读取 vs 单次读完：成本对比

> **货币量化与优化动机**见 **§1.2**（V4-Pro 单价、cache 命中 vs 全 miss、与本分支 WS 的对应关系）。本节聚焦 **机制** 与 **设计取舍**。

**场景**（[token 粗算](https://api-docs.deepseek.com/zh-cn/quick_start/token_usage)）：任务需消费完整 **10000 汉字** 文档；工具单页上限 **2000 字**（≈1200 tokens/页）。

| 路径 | LLM 往返（典型） | 写入 history 的正文总量 | 各次请求 **input 中累积出现的文档 token** |
|------|------------------|-------------------------|------------------------------------------|
| **A. 分 5 次读**（顺序分页，依赖 `next_line_offset`） | **6 次**（5 轮 tool + 1 轮收尾） | 6000 tokens（5×1200） | **18000**（三角累加：0+1200+…+6000） |
| **B. 1 次读完**（假设工具允许单页 10000 字） | **2 次**（1 轮 tool + 1 轮收尾） | 6000 tokens | **6000**（仅最后一轮 input 含全文） |
| **C. 5 次并行 read**（模型已知各段 offset，同一轮 `tool_calls`） | **2 次** | 6000 tokens | **≈6000**（与 B 接近；多 4 组 tool_call / tool 头开销） |

**结论（为何分页有额外成本）**：

1. **Turn 次数**：顺序分页每读一页多 **1 次完整 `StreamChat`**（重放 system + tools schema + 迄今全部 messages）。文档正文在 history 里只存一份，但 **每一轮后续推理的 input 都会再次计入此前各页** → 文档 token 在 input 侧呈 **三角累加**，5 页时约为单次的 **3 倍**（18000 / 6000）。**全 miss 时** 该计数差接近 **3× 账单**（§1.2.3）；**高 cache hit 时** replay 按命中价计，差额主要来自 **多 4 轮 completion**（§1.2.3 ①）。
2. **Completion**：每多一轮 read，模型还要生成 **assistant + tool_calls**（规划下一页），约数百 tokens × 额外 4 轮（输出 **6 元/M**）。
3. **Prompt Cache**：前缀（system、tools、早期 messages）可命中 cache，**降低重复 input 的单价（120×）**，但 **不减少轮数**；新增 tail 仍按 miss 计，且 **completion 线性随轮数增加**（§1.2.2）。
4. **WS3 spill**：若单次 10000 字（6000 tokens）仍低于 `spill_threshold_tokens`（默认 12000），**history 体积与 5 次分页相同**；spill 主要帮助「单页或 grep 块」本身超长，**不能消除分页带来的多轮往返**。

**设计含义**：

- **工具层分页**（2000～3000 字/页）是正确默认：防止单条 tool message 撑爆上下文、逼模型用 `line_offset` 精读。
- **不应**为省轮数而取消分页改「一次读 10000 字」——在真实大文件场景下单次 read 会触发 spill 或更糟的截断；且单页过大反而降低模型精读质量。
- fs 组 WS3 的重点是：**单页仍超长时落盘 + 摘要**、**抑制重复 read**；分页本身的 multi-turn 成本靠 **减少无效重读**、**并行 offset（C，仅当模型已知区间）**、以及正交的 **context 压缩** 缓解，而非取消分页。

**手算附录**（建模符号 P、D、M、O_r、O_f 及逐轮 miss 表，与 §1.2.3 费用表一致）：

| 符号 | 含义 | 取值 |
|------|------|------|
| **P** | 每轮固定前缀：system + tools schema + 用户任务句 | **10000 tokens** |
| **D** | 单页文档正文 | 1200 tokens |
| **M** | tool 消息头 | 80 tokens |
| **O_r** / **O_f** | read 轮 completion / 收尾 | 150 / 500 tokens |

```text
顺序 5 页：input 侧文档 token 合计 ≈ 1200 × (1+2+3+4+5) = 18000
单次读完：input 侧文档 token 合计 ≈ 6000
路径 A 增量 miss input ≈ 10000 + 5×1430 = 17150；路径 B ≈ 16230
各次请求 input 计数（全 miss 场景）：A ≈ 6P+21450，B ≈ 2P+6230
```

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
| **WS3** | Tool 结果 budget | `tool.after_each` 落盘 §3.2.1–§3.2.4（**含 a2a**） | P1 | §3.2 | **已落地** |
| **WS2** | Status 工具统一 wait | `temporary_agent_status` 等 | P1 | §3.1 | **本分支不做** |
| **WS4** | Schema 前缀稳定 | enrich 瘦身、description | P2 | §3.3 | 未开始 |
| **WS5** | 度量 | poll_count、tool_turns | P1 | §6.1 | **已落地**（基础） |

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

## 6.1 WS5：工具链上下文度量（已落地 · 基础）

**锚点**：`node/internal/turn/context_metrics.go`；在每次 `done` SSE 与结构化日志输出 **`tool_context_metrics`**。

**统计窗口**：单次用户任务（`human_message` 入队 → `turn_complete=true`）；新 `human_message` 时重置。

| 字段 | 含义 |
|------|------|
| `tool_loops` | 本任务内 LLM↔tool 循环次数（`runOneStep` loop） |
| `tool_calls` | 工具调用次数（含 status 类） |
| `tool_calls_by_name` | 按工具名计数 |
| `status_poll_count` | `background_job_status` + `temporary_agent_status` |
| `history_result_tokens` / `history_result_chars` | 写入 history 的 tool 结果体积（DeepSeek 粗算） |
| `spill_count` | `tool.after_each` 落盘次数 |
| `read_file_calls` / `read_file_path_repeats` | 同任务内重复读同 path（第二次起计 repeat） |
| `encoding_source_detect` / `encoding_source_cache` | 从 read/grep 结果 header 解析 |
| `encoding_garbled_hints` | 含 `编码提示:` 的次数 |

**输出**：

- SSE `done` 事件：`tool_context_metrics` 对象（HITL 暂停时亦附带快照）
- 日志：`turn context metrics`（`slog` 结构化字段）

**未做（后续）**：Prometheus 导出、跨 session 聚合仪表盘。

---

## 6. WS2–WS6 概要（待展开）

| WS | 要点 |
|----|------|
| **WS6** | 仅 **`rule`+auto** 路径：60s 内同名同参 → 标准 HITL 审批原因；详见 [tool-before-hook-duplicate-approval.md](./tool-before-hook-duplicate-approval.md) |
| **WS2** | **本分支不做**（子 Agent status long-poll 留后续） |
| **WS3** | 可配置 `hooks.tool_result`；**bash + fs + a2a 已落地** §3.2.1–§3.2.4 |
| **WS4** | skills enrich 外置或缩短；enabled_groups 减面 |
| **WS5** | `tool_context_metrics` on `done` SSE + 结构化日志；见 §6.1 |

---

## 7. 建议实施顺序

| 阶段 | 内容 |
|------|------|
| ~~**T1**~~ | ~~WS1~~ **已完成**（文案 + status 保持现状 + WS6） |
| ~~**T1b**~~ | ~~WS3 bash~~ **已完成**（§3.2.1） |
| ~~**T3a**~~ | ~~WS3 fs spill（read/grep）~~ **已完成**（§3.2.2 阶段 1） |
| ~~**T3b**~~ | ~~WS3 fs 编码检测 + 路径缓存~~ **已完成**（§3.2.2 阶段 2） |
| ~~**T3c**~~ | ~~WS3 fs 阶段 3~~ **已完成**（`search_replace` / `glob_files` spill + token 上限） |
| ~~**T2**~~ | ~~WS5 基础度量~~ **已完成**（§6.1） |
| ~~**T4**~~ | ~~WS3 a2a 落盘~~ **已完成**（§3.2.4）；**WS2 不做** |
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
| WS5 度量 | `node/internal/turn/context_metrics.go` |
| fs 编码检测 + 缓存 | `node/internal/tools/fs_encoding_detect.go`, `fs_path_encoding.go` |
| UX：写审批信任链（设计稿） | [ux-agent-owned-file-approval.md](./ux-agent-owned-file-approval.md) |

---

## 9. 结论

本分支以 **「减少低信息密度 LLM 往返 + 控制 tool 写入体积 + 稳定 tools 前缀」** 为纲。**WS1 + WS6 + WS3（bash/fs/a2a）+ WS5（基础）** 已落地；**WS2 本分支不做**。**下一步**：**T5 — WS4** schema 前缀稳定。写审批减负见 [ux-agent-owned-file-approval.md](./ux-agent-owned-file-approval.md)。
