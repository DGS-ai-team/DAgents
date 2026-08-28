# DAgents Runtime Snapshot 与上下文缓存分析

> 检查日期：2026-08-19
>
> 范围：DAgents Node + Web UI；对比 OpenAI Codex 和 DeepSeek Harness。
>
> 重点：工具、Prompt、记忆、Skill、MCP 和执行通道变化时的生效边界，以及对 DeepSeek 前缀缓存的影响。
>
> **历史说明**：本文记录 2026-08-19 的分析结论。之后 prompt sidecar、主机身份和已加载 Skill 正文已改为 request-only `ContextInjection`；涉及它们进入 system prompt 的段落仅保留作历史对照。当前实现以 [`docs/architecture/go-node-internals.md`](../architecture/go-node-internals.md) 与 [`docs/design/agent-quality.md`](../design/agent-quality.md) 为准。

## 1. 结论

当前 DAgents 的 Agent snapshot 主要是“Agent 启动配置快照”，还不是完整的 Turn 级运行时快照。工具、策略、Prompt、记忆、Skill、MCP 和 Linux 通道分别采用了不同的生效策略。

需要同时区分两个边界：

1. **语义生效边界**：模型从什么时候开始看到新配置；
2. **缓存失效边界**：配置变化会让多少历史上下文变成 cache miss。

“下一 Turn 生效”可以解决同一任务内的运行时一致性，但不能自动解决缓存失效范围过大的问题。如果修改发生在 system prompt 前部，长历史会整体落入缓存未命中区间。

推荐的总体方向是优先保持完整上下文连续性，同时控制不必要的 system/tools 变化：

```text
完整 system prompt + 完整 tools schema + 连续历史
                         ↓
                  下一个 Turn 整体更新
```

同时将模型可见上下文与执行安全检查分离：

```text
ModelContextSnapshot  在完整 Turn 内固定
ExecutionGuard        每次工具执行前检查最新状态
```

## 2. Turn、Step 与 LLM Request 的定义

本报告中的 **Turn** 指一次完整的 human message 到最终 assistant 回复完成的任务回合，而不是一次 LLM 请求。

```text
Human
  → LLM Request / Step 1
  → Tool Call
  → LLM Request / Step 2
  → Tool Call
  → LLM Request / Step 3
  → Final Assistant
```

DAgents 中：

- `RunHumanMessageTurn` 处理用户消息并执行一个模型 step；
- `RunToolMessageTurn` 在工具结果返回后继续执行一个模型 step；
- 多个 step 通过 `tool_result` 队列组成一个完整 Turn；
- `toolLoopCount` 统计同一 human message 下的工具循环。

Codex 也将一个 Turn 定义为包含多个 Step 的交互单元；DeepSeek Harness 同样区分“一个模型请求的 Step”和“由多个 Step 组成的 Turn”。参考：[Codex app-server README](https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md)、[DeepSeek Harness Architecture](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/architecture.md)。

因此，本报告中的缓存策略是：

- 每个 LLM Request 都应尽量复用前一个请求的完整前缀；
- 同一个完整 Turn 内的多个 Step 使用同一套 `ModelContextSnapshot`；
- 必要的 system/tools/Skill 变化导致缓存失效是可接受成本；
- 配置变化默认在下一个完整 Turn 生成新的模型上下文，并避免重复触发。

## 3. DAgents 当前生效机制

### 3.1 Agent snapshot

Agent 的 `config_snapshot_json` 存储在 `agents.db`，由 [snapshot.go](../../node/internal/agentruntime/snapshot.go) 解析，由 [build.go](../../node/internal/agentruntime/build.go) 构造 per-agent Registry 和 TurnOptions。

当前主要包含：

- 内置工具组；
- LLM active profile、最大工具循环数、多模态开关；
- Skill 可见列表；
- hooks 配置；
- Prompt context 开关和长期记忆 scope；
- MCP bindings。

`ensureAgentRuntime` 使用 `agents.updated_at` 的 UnixNano 作为配置 revision；版本变化时会 Release 旧 runtime 并重新 Build。[agents_api.go](../../node/internal/api/agents_api.go:587)

### 3.2 独立存储的运行时信息

以下信息不完全属于 snapshot：

| 信息 | 存储/来源 | 当前生效方式 |
|---|---|---|
| 工具策略 | `agent_policy` | `SetSessionPolicy` 原地热更新 |
| Soul/User/Custom 内容 | Prompt sidecar 表 | 保存后当前 runtime 不主动刷新 |
| 长期记忆 | 长期记忆表 | 首次交互、压缩或上下文清理时重新读取 |
| 会话历史、已加载 Skill、HITL | `sessions.db` | runtime 重建时恢复 |
| Skill 文件 | `skills/*/SKILL.md` | mtime + size 延迟扫描 |
| Linux channel | `linux_channels.db` | 新命令/终端打开时动态查询 |
| MCP catalog | MCP DB + MCP manager | 配置变化后重载绑定 Agent |

### 3.3 当前主要问题

1. `UpdatedAt` 同时承担元数据更新时间和配置 revision，无法表达具体变化类型。
2. Runtime 重载是“先 Release、后 Build”，失败时可能丢失旧 runtime。
3. Prompt sidecar 更新不会主动刷新当前 Prompt reader。
4. `long_term_scope` 的 API 更新没有同步写回 Agent snapshot。
5. Skill 正文变化会影响 system prompt；现有 Skill 文件保护 hook 可以限制加载期间的修改，能够降低模型运行过程中反复改写已加载 Skill 的概率，但不能消除外部修改造成的缓存失效。
6. MCP binding 修改存在显式 reload，但 revision 语义并不统一。
7. 当前 Orchestrator 持有 live tools、policy、Prompt 和 Skill catalog，没有统一的 Turn 级不可变上下文快照。

## 4. 上下文缓存的破坏路径

DeepSeek 官方文档说明，后续请求只有完整匹配已持久化的缓存前缀单元才能命中；API 返回 `prompt_cache_hit_tokens` 和 `prompt_cache_miss_tokens` 供观测。[DeepSeek 上下文缓存文档](https://api-docs.deepseek.com/zh-cn/guides/kv_cache/)

### 4.1 system prompt 修改

当前 system prompt 由 [prompt.go](../../node/internal/turn/prompt.go:77) 重新拼接。若稳定规则、Prompt sidecar、长期记忆或 Skill 正文都处于同一个 system prompt 中，则变更可能导致：

```text
旧请求：[SYSTEM][TOOLS][H1...Hn][当前消息]
新请求：[SYSTEM'][TOOLS][H1...Hn][新消息]
```

如果 `SYSTEM` 在前部发生变化，`H1...Hn` 之后的大段历史都可能成为 miss。历史越长，成本和首 token 延迟越高。

### 4.2 tools schema 修改

`Registry.Definitions()` 在每次模型请求前生成工具定义。Skill 元数据还会附加到 `load_skills` description 中。[registry.go](../../node/internal/tools/registry.go)

因此以下变化可能导致 tools 前缀变化：

- Agent 工具组增删；
- MCP 工具增删或 description/schema 变化；
- Skill 新增、删除或 frontmatter description 变化；
- 工具描述、参数顺序或序列化顺序变化。

特别是 Skill 元数据直接嵌入 `load_skills.description`，单个 Skill description 的变化可能影响整个 tools JSON。该问题在历史实验 [skills-context-cost-analysis.md](../archive/experiments/skills-context-cost-analysis.md) 中已记录为 SK1。

### 4.3 历史追加

正常的用户消息、assistant 消息和 tool result 都是追加到历史尾部。只要 system 和 tools 不变，前面的上下文可以继续命中，新增部分自然产生 miss，这是正常且成本较低的行为。

### 4.4 记忆和 Skill 变化

记忆或 Skill 内容属于模型上下文的一部分，发生语义变化时直接重建完整 system prompt 是正确的；由此造成的缓存失效应被视为必要成本，而不是通过拆分上下文来规避。

优化重点应放在减少无意义的变化：

- 不把时间戳、runtime revision 等易变诊断字段写入 system prompt；
- 保持 Prompt section、Skill 顺序和工具序列化稳定；
- 同一个 Turn 内固定 Prompt 和 Skill snapshot；
- Skill 文件保护 hook 限制加载期间的修改；
- 将频繁变化的外部状态留在工具结果或会话事件中，不反复改写基础 Prompt。

### 4.5 上下文压缩

压缩会重写历史，是天然的缓存边界。DAgents 已有的压缩设计要求侧车请求复用主 Turn 的 system、tools 和历史前缀，见 [context-compression-cache-analysis.md](../design/context-compression-cache-analysis.md)。后续仍应保证压缩请求固定使用相同的：

- StableSystem；
- ToolSchema；
- LLM model；
- RequestExtra；
- runtime generation。

## 5. 推荐的运行时模型

### 5.1 ModelContextSnapshot

在完整 Turn 开始时生成并固定：

```text
ModelContextSnapshot
├── SystemPrompt
├── ToolDefinitions
├── MCP catalog revision
├── Skill revision
├── Prompt revision
├── Memory revision
├── LLM profile
└── prompt/tool digest
```

同一个 Turn 内的所有 LLM Step 使用相同快照。

### 5.2 ExecutionGuard

工具执行前使用最新状态检查：

```text
ExecutionGuard
├── 最新 policy
├── 工具是否仍允许执行
├── Linux channel 是否启用
├── credential 是否有效
├── HITL/审批状态
└── 并发、超时和资源限制
```

例如用户在当前 Turn 中禁用了某个 Linux channel：

- 不需要立刻修改 tools schema；
- 当前或下一次工具调用执行前被拒绝；
- 返回明确的 tool result；
- 下一个 Turn 再使用新的模型工具上下文。

这样可以同时满足安全性和缓存稳定性。

## 6. 完整上下文连续性与缓存边界

本报告不推荐为了缩小缓存失效区间而把记忆、Skill 或运行时信息拆到历史尾部。这样虽然可能保留部分前缀缓存，但会引入新的上下文连续性问题：

```text
[修改前的 system/tools]
[旧历史]

[修改后的 system/tools]
[旧历史]
[当前消息]
```

当 system、tools 或模型可见 Skill 内容发生真正的语义变化时，长历史缓存大范围失效是合理代价。此时更重要的是保证新请求的完整上下文一致，而不是只追求局部 cache hit。

优化方向应改为“减少不必要变化”和“控制变化频率”：

- 保持 Prompt section 的顺序和序列化稳定；
- 不把时间戳、revision、调试信息写入模型可见 Prompt；
- 同一 Turn 内不重新生成不同版本的 system/tools；
- 对配置更新做合并和去重，避免短时间内多次重建；
- 让必要变化在下一个 Turn 一次性生效；
- 为每次失效记录原因和失效 token 数。

`runtime_revision`、时间戳和 digest 只用于日志、SSE 和指标，不应直接拼入 system prompt，否则会因为每次配置写入而主动破坏缓存。

## 7. Skill 与 MCP 的缓存优化

### 7.1 Skill

Skill 清单和 Skill 正文都是模型可见上下文的一部分。清单或正文发生真正变化时造成缓存失效是可以接受的，不建议为了缓存而引入通用 dispatcher 或牺牲工具 schema 的表达能力。

仍应保持以下约束：

1. Skill 清单排序和描述序列化稳定；
2. 同一个 Turn 固定 Skill snapshot；
3. Skill 文件保护 hook 限制已加载 Skill 在加载期间被修改；
4. 外部修改在下一 Turn 统一生效，不在当前 Turn 中途替换正文；
5. 记录 Skill 变化造成的 cache miss，而不是把它误判为系统故障。

### 7.2 MCP

MCP 工具目录变化会生成新的 ToolSchema，下一 Turn 生效，并接受一次必要的缓存 miss。不建议默认改成通用 `mcp_call` dispatcher 来规避缓存，因为这会牺牲 MCP 工具的细粒度参数 schema和模型调用质量。

应优化的是：

- 工具名称和 schema 序列化稳定；
- server catalog 变化合并后一次性应用；
- 同一 Turn 内固定 MCP ToolSchema；
- 通过 revision 和 cache miss 指标区分必要变化与意外漂移。

## 8. 对比 Codex 与 DeepSeek Harness

### Codex

Codex 的 `thread/settings/update` 会把配置变化排队到 loaded thread 的下一次 Turn，不会立即破坏当前 Turn；MCP reload 也采用 next active turn 语义；Skill 变化通过 `skills/changed` 通知，并由客户端重新读取。[Codex app-server README](https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md)

对 DAgents 的启发：

- 配置变化与当前 Turn 解耦；
- 变化通过 revision/notification 表达；
- 只在下一 Turn 更新模型可见上下文；
- 不要把安全策略热更新误认为必须重建 tools schema。

### DeepSeek Harness

DeepSeek Harness 的 Cordis 配置树支持插件组合、可撤销注册和配置 HMR；当 patch 读取、解析或 Loader 失败时保留 last-good tree。[DeepSeek Harness app-boot](https://github.com/deepseek-ai/deepseek-harness/blob/master/packages/boot/app-boot/README.md)

对 DAgents 的启发：

- runtime 重建应采用先构建、后交换；
- 失败时保留旧 runtime；
- 能力注册应有 scope，避免工具、Prompt、MCP 和 hooks 相互污染；
- 配置树的变化应产生可观测事件。

## 9. 落地建议

### P0

1. 增加独立的 `runtime_revision` 和 `runtime_digest`，不再使用 `UpdatedAt` 作为唯一 revision。
2. 每个完整 Turn 创建不可变 `ModelContextSnapshot`。
3. 将 policy、Linux channel 和 credential 检查抽为独立 `ExecutionGuard`。
4. Prompt/记忆/Skill/MCP 变化默认下一个 Turn 生效。
5. Runtime 重建改为先 Build 新 runtime，成功后再交换；失败保留旧 runtime。
6. 记录 `runtime_generation`、`prompt_digest`、`tool_digest`、`prompt_cache_hit_tokens` 和 `prompt_cache_miss_tokens`。

### P1

1. 增加 `runtime/config-changed`、`skills/changed`、`memory/changed` 和 `mcp/catalog-changed` 事件。
2. 修复 Prompt sidecar 保存后当前 runtime 不刷新的问题。
3. 修复 `long_term_scope` 没有写回 Agent snapshot 的问题。
4. 为压缩侧车固定 ModelContextSnapshot，防止压缩请求与主请求前缀不一致。
5. 对 Skill 清单、Skill 正文和 MCP catalog 做稳定排序、规范化序列化和变化去重。
6. 保留 Skill 文件保护 hook，并补充外部修改检测和下一 Turn 应用逻辑。

### P2

1. 为工具 schema、Skill、MCP catalog 建立独立 revision。
2. 为 Prompt、Skill、MCP 和压缩边界增加结构化事件和回放能力。
3. 增加缓存命中率与配置变更原因的关联分析。
4. 对配置变更做 debounce/合并，避免连续修改造成多次缓存冷启动。

## 10. 验收指标

建议至少观测以下指标：

| 指标 | 目的 |
|---|---|
| `prompt_cache_hit_rate` | 观察整体缓存效果 |
| `prompt_cache_miss_tokens` | 观察一次配置变化造成的失效规模 |
| `cache_break_reason` | 区分 system、tools、history、compression、model 变化 |
| `stable_prompt_digest` | 判断稳定前缀是否意外漂移 |
| `tool_schema_digest` | 判断工具 schema 是否意外变化 |
| `runtime_generation` | 关联同一 Turn 内是否混用了不同 runtime |
| `context_revision` | 关联 Prompt/Memory/Skill/MCP 变化 |

重点验收场景：

1. 长历史中修改长期记忆，确认缓存失效范围可观测且只发生一次；
2. 当前 Turn 中禁用 Linux channel，工具执行被拒绝但 tools schema 不中途变化；
3. 下一 Turn 工具集变化后，模型使用新 schema；
4. Skill description 修改不会导致整个 tools schema 变化；
5. runtime Build 失败时旧 runtime 和旧缓存前缀仍可继续工作；
6. 压缩侧车与主 Turn 使用同一套完整 Prompt/Tool generation。

## 11. 本次设计决策修订

根据对上下文连续性的进一步讨论，本报告明确采用以下决策：

1. **不采用动态尾部 context 方案**。记忆、Skill 和运行时信息继续作为完整模型上下文的一部分，避免上下文语义割裂。
2. **接受必要的缓存失效**。工具清单、Skill 清单、MCP catalog 和 Skill 正文发生真实语义变化时，缓存失效是合理成本。
3. **优化无意义变化**。通过稳定排序、规范化序列化、revision 去重、Turn 内 snapshot 和配置合并，避免没有语义变化却触发 cache miss。
4. **利用现有 Skill 保护 hook**。限制已加载 Skill 在加载期间被修改，减少运行时反复变化；对外部修改仍采用下一 Turn 生效。
5. **安全策略独立热更新**。policy、Linux channel 和 credential 的实时禁用不要求中途修改模型 tools schema，而是在工具执行边界拒绝。
