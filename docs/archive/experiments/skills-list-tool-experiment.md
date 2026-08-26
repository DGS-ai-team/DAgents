# `list_available_skills` 实验设计

## 目标与非目标

目标是评估“Skills 目录放在 system prompt”与“Skills 目录通过显式工具查询”两种方案，重点观察：

- Skill 选择准确率；
- 首次加载所需模型步数；
- 无关任务的 prompt token 与 cache 命中；
- catalog 修改是否影响稳定的 tools/system 前缀。

本设计不改变默认行为，不把 Skill 正文放入工具 schema，也不取消 `load_skills` 的显式加载和下一模型 Step context mutation 语义。

## 工具契约

实验工具名称：`list_available_skills`

建议参数：

```json
{
  "query": "可选的名称或用途关键词",
  "limit": 10,
  "cursor": "可选分页游标"
}
```

约束：

- `limit` 默认 10，硬上限 20；非法值归一化，不允许模型请求无界目录。
- `query` 只匹配 Skill name、directory name 和 description，不读取正文。
- 结果按 canonical directory name 排序，保证同一 catalog revision 下字节稳定。
- 应用 Agent 的 `visible` allowlist 后再搜索和返回，不能通过查询泄露不可见 Skill。
- 返回结果只包含元数据，不包含 `SKILL.md` 正文：

```json
{
  "status": "succeeded",
  "catalog_revision": "...",
  "query": "writer",
  "skills": [
    {
      "skill_name": "writer",
      "directory_name": "writer",
      "description": "Write documents"
    }
  ],
  "has_more": false,
  "next_cursor": ""
}
```

工具返回沿用统一的 tool result status/error 协议；空结果必须是 `skills: []`，不能用自然语言表示“没有找到”。

## System Prompt 与默认开关

实验关闭时保持现网：system prompt 尾部注入可用 Skill 的 name/description，模型匹配后调用 `load_skills`。

实验开启时：

1. system prompt 不注入完整 Skills 目录，只保留一条稳定指引：“需要选择 Skill 时先调用 `list_available_skills`，再调用 `load_skills`”。
2. `list_available_skills` 作为普通 API tool definition 发送给模型。
3. 查询结果进入 tool message/history，下一次模型 Step 可使用；Skill 正文仍只由 `load_skills` 加载。
4. catalog revision 只出现在查询结果和诊断事件中，不拼入工具 description，避免 catalog 修改改变 tools schema。

该开关必须按 Agent/runtime snapshot 固定，不能在活动模型请求中途切换。默认关闭，避免未经 A/B 证明就改变现网 Skill 选择行为。

## 缓存与质量假设

预期收益：

- catalog description 修改不再改变 system prompt 尾部；
- catalog 变化不改变 tools schema 字节；
- 目录查询结果只在真正需要 Skill 的任务中进入历史。

预期代价：

- 首次使用 Skill 通常多一次模型工具调用；
- 查询结果会增加一次 tool message/history tail；
- 如果 query 选择质量下降，模型可能加载错误 Skill 或完全不加载。

因此不能只看 cache hit rate。实验必须通过 `eval.CompareAB` 同时比较：任务成功率、Skill 选择/加载率、验证覆盖率、工具失败、步骤数、token、成本和 cache 观测完整度。

## 验收与回滚

只有满足以下条件才考虑替换默认目录注入：

1. 双方至少 3 个共享场景，正式评估建议 12 个以上；
2. treatment 质量指标不下降，且 Skill 选择准确率不下降；
3. 无关任务的 prompt token 或 cache 成本有明确收益；
4. provider 实际返回 cache 字段，且双方所有样本均 `cache_observed=true`；
5. context mutation、异步回调、取消和失败恢复场景无回归。

任一质量指标回退时关闭实验开关，恢复 system prompt catalog；回滚不改变已持久化的 `loaded_skills`，只影响后续 Skill 发现方式。

## 当前状态

实验路径已实现但默认关闭。现网默认仍使用 system prompt Skills catalog；只有 Agent snapshot 显式设置 `defaults.skills.catalog_tool_mode=true` 且 skills 工具组可见时，才会把目录发现切换为 `list_available_skills`。

实现约束：

- 查询工具只返回元数据，不读取或返回 `SKILL.md` 正文；不修改 loaded 集合，也不触发 context refresh。
- 默认 limit 为 10，硬上限为 20；结果按目录名/逻辑名稳定排序并支持不透明分页 cursor。
- 工具只在 `load_skills` 已经出现在当前工具集时追加；受限工具集和子 Agent 不会凭实验开关单独得到目录发现能力。
- 配置开关固定在 runtime snapshot 内，活动 Turn 中不会切换。

当前不启用替代路径的理由仍然成立：修正 Agent 配置后，真实 Mimo 已执行完整的 `list_available_skills` → `load_skills` 链路，且所有样本都有 cache 字段；但 3 组语义任务中 list tool 组有 1 组在 25 个模型 Step 后输出伪工具调用，工具失败数和输入 token 也显著增加。说明“能发现”已成立，“质量与成本值得替换”尚未成立。因此当前结论仍是默认关闭，先修正工具调用稳定性并扩展真实项目任务，再进行正式 cache A/B。采集步骤见 [`prompt-tool-schema-skills-cache-ab-runbook.md`](./prompt-tool-schema-skills-cache-ab-runbook.md)。
