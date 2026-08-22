# Skills / Tool Context Cache A/B 验证手册

## 目的

验证以下两类上下文变化的质量收益与缓存成本：

1. 当前策略：system prompt 内保留可用 Skills 的 `name/description` 目录，正文通过 `load_skills` 在下一个模型 Step 生效。
2. 候选策略：将可用 Skills 目录改为显式查询工具（仅在单独实验分支启用），比较首次选择质量、额外工具步数和 cache 成本。

工具定义不复制到 system prompt。两组实验必须使用相同的工具 API schema、模型、历史任务和运行时版本；只有 Skills 目录注入方式不同。

## 前置条件

- 使用隔离 Node/runtime，不影响用户正在运行的 Node。
- treatment/control 使用同一 provider、model、temperature/推理开关和工具策略。
- provider 的 usage 必须返回以下任一完整字段组：
  - DeepSeek：`prompt_cache_hit_tokens` + `prompt_cache_miss_tokens`；
  - OpenAI-compatible：`prompt_tokens_details.cached_tokens`，并能由 `prompt_tokens` 推导 miss。
- 如果 provider 没有返回 cache 字段，仍可评估质量和 token，但 `cache_comparable` 必须为 false，不能给出缓存收益结论。

## 最小场景集合

至少使用 3 个双方都有结果的相同 `scenario_id`；正式结论建议 12 个以上，并覆盖：

| 场景 | 关注点 |
|---|---|
| 普通无 Skill 任务 | 目录常驻对无关任务的 token/cache 影响 |
| 单 Skill 任务 | 选择准确率、`load_skills` 调用和正文可见性 |
| 多轮 Skill 任务 | loaded 正文在后续 human Turn 是否稳定保留 |
| 显式 unload/clear | context mutation 是否只在下一个模型 Step 生效 |
| Skill 目录修改 | 活动 Turn 是否保持 snapshot，下一 human Turn 是否发现变化 |
| 同名/不可见 Skill | 是否返回 `ambiguous` / `not_visible`，不静默选错 |
| 工具失败恢复 | status/error 是否进入模型侧 tool message，历史正文是否保持原格式 |

## 采集方式

每个样本记录为 `eval.Trace`，只从权威运行事实构造，不从 UI 文本猜测：

- `TurnCoordinator` 生命周期事件：Turn/Step、context snapshot、context epoch、结束原因；
- `model.usage.recorded`：prompt/completion token、reasoning token、cache hit/miss 及 `prompt_cache_available`；
- 工具执行事件：工具名、status、error、retryable、执行次数和结果；
- hydrate/transcript：确认最终回答、Skill 正文是否进入正确 snapshot、tool 正文没有内部 metadata；
- provider 原始 usage：保留一份脱敏样本，便于确认字段确实来自 provider。

实验结束后调用 `eval.CompareAB`。缺失 Trace 必须保留为失败样本，不得静默丢弃。

## 结论门槛

1. treatment 的任务成功率、断言通过率、验证覆盖率不能下降。
2. 取消违规数和工具失败数不能增加。
3. 质量提升可以接受更多 token 或一次额外工具调用，但必须报告成本差值。
4. 只有双方所有样本都观测到 cache 字段时，才允许比较 cache hit rate。
5. context mutation 只能发生在显式 Skills 变更后的下一个模型 Step；磁盘自动变化不得中途改写活动 Turn。
6. 未达到最小样本或存在未观测 cache 时，结论必须是 `inconclusive`。

## 当前状态

Node 当前活动 provider 为 `mimo-v2.5-pro`。首轮真实模型 Skills A/B 已验证当前策略能提升需要 Skill 正文的任务完成质量；后续隔离 Node 样本已返回 `prompt_cache_hit_tokens`、`prompt_cache_miss_tokens`，因此 cache A/B 的采集前置条件已满足。但已完成的首轮任务使用了显式 Skill 路径，不是目录发现方案的有效对照；目录发现语义任务又未形成完整真实工具链，当前仍不能宣布 `list_available_skills` 带来缓存收益。

本轮已补充 3×3 多轮实验：第一 Turn 发现/加载、第二 Turn 无工具复用、控制面 unload 后第三 Turn 重新加载、第四 Turn 长上下文复用。所有样本均观测到 cache 字段；第二和第四 Turn 两种目录呈现方式的成本接近，unload 后重新发现时 list-tool 组额外增加一次目录查询。因此当前默认方案保持不变，目录工具继续默认关闭。

相关实现：

- `node/internal/eval/ab.go`
- `node/internal/llm/usage.go`
- `node/internal/turn/lifecycle.go`
- `docs/design/prompt-tool-schema-skills-ab-report-2026-08-22.md`
