# Skills 加载与工具结果协议真实模型评估

日期：2026-08-22

## 测试目的

验证以下两个设计在真实模型上是否成立：

1. `load_skills` 后，当前 Turn 的下一个模型 Step 能看到 skill 正文。
2. 工具执行状态通过请求侧 `[TOOL_RESULT_METADATA]` 对模型可见，同时不污染 hydrate/UI 的原始 tool 正文。

## 方法

- 使用最终源码构建隔离的 Agent Node，主 Node 和现有 Agent 未受影响。
- 使用同一 Mimo 模型配置。
- treatment：启用 `skills` 工具组，并提供匹配的 skill 目录。
- control：不启用 `skills` 工具组，其他任务描述保持一致。
- 通过 hydrate、timeline 和生命周期事件读取结果，不通过 UI 文本猜测执行状态。
- cache 观测字段使用统一的 `llm.Usage` 归一化，并额外记录 `prompt_cache_available`；provider 未提供字段时不把未知状态当作 0% 命中。

## 结果

### `list_available_skills` 真实发现 A/B（修正配置后）

本轮重新创建隔离 Agent，确保 `tools.enabled_groups` 使用与现网相同的数组格式，且两组都启用
skills 工具。任务没有给出 Skill 名称或路径，只给出需要读取的证据目标；因此 treatment 必须自行
发现 Skill，control 只能使用 system prompt 中的目录。

| 场景 | system catalog：模型请求 / 工具调用 / input | list tool：模型请求 / 工具调用 / input | 结果 |
|---|---:|---:|---|
| README 验收 | 5 / 4 / 52,984 | 4 / 6 / 44,119 | 双方 `EVIDENCE_INCOMPLETE` |
| go.mod 检查 | 4 / 3 / 41,832 | 6 / 5 / 65,802 | 双方 `EVIDENCE_INCOMPLETE` |
| 设计文档验收 | 4 / 6 / 43,451 | 25 / 24 / 614,091 | control `EVIDENCE_INCOMPLETE`；treatment 达到模型循环上限后输出伪工具调用文本 |

观测事实：

- system catalog 组 3/3 调用 `load_skills`，0/3 调用 `list_available_skills`；list tool 组 3/3 先调用
  `list_available_skills`，再调用 `load_skills`。
- 两组 6/6 样本都观测到 provider cache 字段。该轮合计：system catalog 输入 138,267、命中
  77,376、未命中 60,891；list tool 输入 724,012、命中 595,584、未命中 128,428。
- list tool 组第 3 个场景出现明显长尾（25 个模型请求、24 次工具调用、3 次工具失败），因此不能
  把较高的累计 cache 命中比例解释为目录工具化收益；它很可能只是重复循环对已有前缀的命中。
- 隔离 Node 的工作区是运行时目录，不包含被请求的项目源码/文档，所以三组任务的质量结论主要是
  “证据不足”路径验证，不代表项目文档质量。

本轮结论：目录工具的发现语义已被真实模型执行，但额外模型步数和一次循环失控构成负面信号；在
没有修正工具调用稳定性并完成更多真实项目任务前，不替换默认 system catalog。

### 多轮、显式 mutation 与长上下文 A/B

为验证已加载正文是否跨人类 Turn 保持，以及显式 unload 是否制造可预期的 cache/context 断点，
另使用 3 组 system catalog Agent 与 3 组 list-tool Agent，执行同一流程：

1. 第一 Turn：不指定 Skill 名称，只要求完成最小 Skill 加载验收；
2. 第二 Turn：禁止工具调用，只复述上一轮已加载 Skill 的事实边界；
3. 在第二 Turn 结束后通过控制面显式 `unload quality-gate`；第三 Turn 要求重新判断并加载；
4. 第四 Turn 注入约 72,000 字符的固定长上下文负载，要求不调用工具，仅确认正文连续性。

| 阶段 | system catalog（3 样本） | list tool（3 样本） | 观察 |
|---|---:|---:|---|
| 第一 Turn 模型请求 | 2、4、2 | 3、3、3 | 默认组调用 `load_skills`；实验组固定为 `list_available_skills → load_skills` |
| 第二 Turn 模型请求 | 各 1 | 各 1 | 两组均无工具调用，snapshot 保留 Skill 正文 |
| 显式 unload 后第三 Turn | 2、3、2 | 各 3 | 初始 snapshot 不含正文；两组均重新加载，随后各产生 1 次 `model.context.changed` |
| 长上下文第四 Turn | 各 1 | 各 1 | 6/6 无工具调用；正文连续；没有新增 context mutation |

cache 汇总（所有样本均 `prompt_cache_metrics_observed=true`）：

- 第二 Turn：system catalog 输入 33,382、命中 32,320；list tool 输入 34,465、命中 33,472。
- 显式 unload 后第三 Turn：system catalog 输入 82,218、命中 75,136；list tool 输入 110,239、命中 101,440。
- 长上下文第四 Turn：system catalog 输入 154,526、命中 36,416；list tool 输入 156,214、命中 38,080。

结论：Skill 正文在多轮中保持连续，显式 mutation 的 cache 断点位置符合设计；目录工具在后续
Turn 没有带来可测的 cache 优势，反而在重新发现阶段额外增加模型请求和输入 token。长上下文阶段
两组输入仅相差约 1.1%，命中率差异约 0.8 个百分点，不足以抵消首次发现的额外调用成本。

本轮是同一 provider、同一运行时和 3×3 样本的探索性 A/B；质量任务本身是受限的 Skill 边界
验收，不代表所有业务任务。它足以决定默认策略不切换，但不足以宣称任意 provider 上的普遍收益。

### 同一验收任务，3 组有效对照

| 指标 | treatment | control |
|---|---:|---:|
| 完成样本 | 3/3 | 3/3 |
| skills 工具调用 | 3/3 | 0/3 |
| `model.context.changed` | 3/3，各 1 次 | 0/3 |
| skill 正文进入第二 snapshot | 3/3 | 0/3 |
| 按任务契约输出 `QUALITY_GATE_OK` | 3/3 | 0/3 |
| 平均模型请求数 | 2 | 1 |
| 平均工具调用数 | 1 | 0 |
| 平均 input tokens | 约 5,088 | 约 1,121 |

control 的低 token 消耗来自没有加载工具和正文，不能直接视为更优；它没有完成需要 skill 正文的任务。

### 跨任务补充对照

- 变更验收任务：treatment 加载 `change-verifier`，因没有真实测试证据而按 skill 要求输出 `EVIDENCE_INCOMPLETE`；control 无 skills 工具并输出伪工具调用文本。
- 故障诊断任务：treatment 加载 `incident-report`，输出 `INCIDENT_REPORT_OK`，并按要求区分事实、推断和未知项；control 无 skills 工具并输出伪工具调用文本。

### 工具结果隔离

- treatment 最终答案实际引用了 `[TOOL_RESULT_METADATA]` 中的 `status=succeeded`。
- 同一 Agent 的 hydrate tool result 正文不包含 `[TOOL_RESULT_METADATA]`。
- 说明 metadata 进入模型请求副本，但没有进入前端/历史展示正文。

### Cache 观测链路 fixture 验证

在没有可用真实 cache provider 的前提下，使用 OpenAI-compatible SSE fixture 注入：

```json
{
  "prompt_tokens": 100,
  "prompt_cache_hit_tokens": 80,
  "prompt_cache_miss_tokens": 20
}
```

已验证该数据可以从 HTTP/SSE 解析进入 `llm.Usage`，再进入 `StepUsage`/`TurnUsage`、
`model.usage.recorded` 生命周期事件，并在 SQLite 重启回放后保留。另有无 cache 字段和
明确 0 命中的 fixture，验证未知状态不会被质量评估聚合成 0% 命中。该 fixture 证明
采集链路可用，但不替代真实 provider 的缓存行为测试。

## 结论

当前策略 B（显式 skills 变更后在下一个模型 Step 替换 context snapshot）在本次真实模型样本中有效，建议保留。模型侧 status 适配也已验证可见且不破坏原始正文。

## 限制与后续

- 样本主要来自一个验收任务，补充任务各 1 组；不能代表所有任务、模型和 provider。
- 首轮 provider 样本没有返回可用的 prompt-cache hit/miss 字段，因此首轮只能记录 input tokens 和请求次数，不能做真实缓存命中率结论。
- 后续隔离 Node 的 Mimo 样本已观测到 `prompt_cache_hit_tokens`、`prompt_cache_miss_tokens` 和 `prompt_cache_metrics_observed=true`；这证明采集链路已接收到真实 provider 字段，但首轮显式路径任务并不是 `list_available_skills` 的有效对照，不能直接据此宣布目录工具化带来缓存收益。
- 另一次不指定 Skill 名称/路径的语义发现任务中，6 个样本均未形成可执行的真实工具调用链（部分输出了伪工具调用文本，部分等待 HITL）；因此该轮对目录发现方案的质量比较为 `inconclusive`，不是成功或失败结论。
- 下一轮应扩展到文件修改验证、远程 Linux 操作、异步回灌和失败恢复，并记录 provider cache 字段、完成率、重试次数和 token 成本。
- `list_available_skills` 工具化仍暂缓，直到多任务 A/B 证明 skill 选择准确率不下降且缓存收益明确。
