# DAgents 非用户来源 `role=user` 消息审计

日期：2026-08-23
范围：Node 的 LLM request、durable session history、hydrate/transcript 三条链路。

## 1. 结论

`role=user` 在 DAgents 中并不等于“用户刚刚输入的消息”。当前同时存在三种语义：

1. **真实外部输入**：用户、触发器、A2A、父 Agent 发送的新任务；
2. **持久化的模型上下文事件**：压缩摘要、异步回调桥接、图像工具结果、已激活 Skill 正文；
3. **请求级临时上下文**：运行环境、Agent/session 身份、prompt sidecar。

如果只看 `role=user` 而不看结构化 `source/provenance`、兼容字段 `name` 和持久化边界，容易造成三个问题：

- 把内部上下文显示成用户说过的话；
- 把只应影响当前请求的动态信息写入历史，造成缓存和压缩污染；
- 把技能正文塞入 system prompt，导致稳定前缀失效和每一步重复传输。

本次已完成的结构调整是：

- Skills 清单仍可进入 system prompt，但只包含名称和描述；
- SKILL.md 正文在激活时作为独立的 `role=user`、`source=plugin/form=instructions` durable context message 写入历史，保留 `name=skill` 兼容字段；
- 出站请求只保留当前激活且正文 digest 匹配的 Skill 消息；
- hydrate/transcript 隐藏 Skill 正文消息；
- 运行环境和 prompt context 继续使用 request-only `source=runtime/form=snapshot`，保留 `name=context` 兼容字段且不落库。
- `llm.Message` 已增加结构化 `source` 与 `provenance`；`name` 暂作为旧历史、事件和 provider 兼容字段保留。
- 新消息构造时自动物化来源；旧 SQLite 消息在运行时通过 `name` 延迟推导，不进行一次性历史重写，避免无意义的缓存/摘要变化。

## 2. 当前消息分类

### 2.1 结构化来源模型

消息的模型角色与来源不再是同一个概念：

```go
Message{
    Role: "user",
    Name: "skill", // 兼容字段，不作为新的业务判断入口
    Source: &MessageSource{
        Kind: "plugin",
        Form: "instructions",
    },
    Provenance: &MessageProvenance{
        Producer:  "skills",
        Operation: "load",
        Reference: "writer",
    },
}
```

- `source.kind` 表示生产者类别，例如 `user`、`plugin`、`runtime`、`tool`；
- `source.form` 表示消息语义，例如 `request`、`instructions`、`snapshot`、`summary`；
- `provenance` 标识具体生产者、操作和引用对象；
- provider 请求不会发送这两个内部字段；SQLite history 和 raw journal 会保存它们。

旧消息没有结构化字段时，`EffectiveMessageSource` 会按旧 `name` 映射；未知 name 会被标为 `legacy`，不会默认为真人输入。

| 兼容 `name` / 结构化 source | 来源 | 是否真实用户意图 | 是否写入 history | 是否展示在 hydrate | 当前处理 | 结论 |
|---|---|---:|---:|---:|---|---|
| `human` | Web/API 人工输入 | 是 | 是 | 是 | 普通根 user 消息 | 保持 |
| `trigger` | 定时器、外部触发器 | 是，属于外部事件 | 是 | 是 | 作为新的根 user 消息 | 保持，并保留来源 |
| `a2a_inbox` | A2A/异步外部消息 | 是，属于外部事件 | 是 | 是 | 作为新的根 user 消息 | 保持，并保留来源 |
| `child_task` | 父 Agent 对子 Agent 的任务 | 是，属于父 Agent 输入 | 是 | 是 | 作为新的根 user 消息 | 保持；子 Agent 不应伪装成 human |
| `context` / `runtime + snapshot` | Node 运行环境、身份、prompt sidecar | 否 | 否 | 否 | 当前请求副本中插入当前根 user 前 | 已符合目标；禁止持久化 |
| `skill` / `plugin + instructions` | 已激活的 SKILL.md 正文 | 否 | 是 | 否 | 激活时独立 durable message；出站按当前激活版本过滤 | 本次已结构化 |
| `date` / `runtime + snapshot` | 内置当天日期 | 否 | 否 | 否 | 在模型请求快照创建时并入 `ContextInjection`；保留旧 Hook 标识但不再产生 history mutation | 已迁移；跨天只影响下一次请求快照 |
| `async_tool` | 异步工具完成回灌、外部回调桥接 | 否，但它承载真实执行事实 | 通常是 | 否 | 有合法 tool 尾部时写 assistant/tool；纯 assistant 尾部时使用 user bridge | 保持兼容，减少重复正文 |
| `compression` | 历史压缩摘要 | 否 | 是，替换旧区间 | 否 | 用 user-role 摘要维持合法对话序列 | 保持；应携带摘要 provenance |
| `compression_sidecar` | 压缩侧车请求的摘要指令 | 否 | 否 | 否 | 只存在于侧车 API 请求 | 已符合目标 |
| `tool_vision` | `read_image` 的多模态后续上下文 | 否 | 是 | 否 | 以 user 多模态消息保存图像和提示 | 保持，避免再次读取/丢失图片 |

其中 `trigger`、`a2a_inbox`、`child_task` 虽然不是当前操作者直接输入，但确实代表新的外部任务意图，不应和 `context`、`date`、`skill` 这类内部注入混为一谈。

## 3. Skill 正文的新生命周期

```text
skills catalog
  只提供 name + description
       │
       ├─ 预加载/控制面加载
       │       └─ 下一次模型请求前，正文插入当前根 user 之前
       │
       └─ 模型调用 load_skills
               └─ tool result 之后插入 skill context message

durable history
  assistant(tool_call) → tool(load_skills) → user(source=plugin/instructions)

outbound request
  system prompt（稳定前缀）
  + 当前 history
  + request-only context
  + 当前激活且 digest 匹配的 skill message
```

Skill 消息使用如下结构，便于模型识别来源，也便于运行时做版本去重：

```xml
<skill_instructions>
<name>pdf</name>
<path>skills/pdf/SKILL.md</path>
<content_digest>...</content_digest>
<instructions>
完整 SKILL.md 正文
</instructions>
</skill_instructions>
```

正文变化时不改写旧 history。下一次 Catalog Turn view 读取新正文并生成新 digest；出站过滤器只保留当前激活版本。卸载 Skill 后，旧正文仍可作为审计历史保存，但不会继续进入模型请求。

## 4. 需要继续优化的项目

### 4.1 当天日期 `date`：已迁移

当天日期现在作为 request-only `ContextInjection` 的一个稳定段落生成，不再作为 user 消息写入 durable history。迁移后的行为是：

- 保留 `inject_today_date_enabled` 配置开关；
- 每次新建模型上下文快照时读取一次 `YYYYMMDD`，并与运行环境、prompt sidecar 一起放入同一个 context 消息；
- 当前 Turn 的后续 Step 复用同一个快照，不会重复追加日期或在 Turn 中途切换日期；
- hydrate、压缩、SQLite history 和 raw journal 不会接触新生成的日期消息；旧版本遗留的 `name=date` 消息仍按兼容规则识别。

请求内容示例：

```text
name=context
## 当前日期
- 当前日期：2026-08-23
```

保留配置开关，但让 Hook 只保留兼容注册名，不再写入 history。这样日期变化只影响 request context，不污染持久化对话，也不会改变稳定 system prompt 前缀。

### 4.2 异步回调 `async_tool`：优先级 P2

现有逻辑已经较谨慎：

- 如果历史尾部存在对应 assistant tool call，则使用标准 `assistant → tool` 序列；
- 如果尾部是纯 assistant，不能直接追加 tool result，因此使用 user bridge 保持 OpenAI 消息序列合法；
- bridge 中携带 job_id、status、来源 tool call 和结果正文。

建议保持该兼容策略，但后续可将 bridge 的正文进一步统一为结构化事件，减少“原始用户文本 + tool 正文”的重复，并在 history metadata 中记录 `synthetic=true`、`source=async_tool`。

### 4.3 压缩摘要 `compression`：优先级 P2

压缩摘要必须持久化，否则重启后无法恢复被压缩的语义；使用 user role 是为了在 Chat Completions 序列中保持合法，因此不建议简单改成 system message。

后续可补充：

- 摘要覆盖区间指纹；
- 生成模型和生成时间等内部 metadata；
- 标记摘要是“可替代的历史投影”，避免 UI 或后续审计把它当成真人输入。

当前 `name=compression` 已足以让 hydrate 隐藏它，并让压缩逻辑区分来源。

### 4.4 图像工具上下文 `tool_vision`：优先级 P2

图像正文需要作为后续模型请求的一部分保留；当前以 user 多模态消息保存，能够让重启、重试和 provider adapter 继续获得图片。

暂不建议移除或改成普通 tool result，原因是不同 OpenAI 兼容服务对 tool result 的多模态支持不一致。可后续增加统一的 `source_tool_call_id` metadata，避免与人工图片消息混淆。

### 4.5 Hook 的 `history_append/history_insert`：优先级 P1

当前任意 Hook 可以通过 mutation 写入 user/assistant/tool/system 消息。建议增加来源元数据或约束：

- 需要写入模型上下文但不是用户输入的 Hook 消息，必须使用受控 `name`；
- 不允许 Hook 直接伪造 `name=human`；
- request-only 信息使用独立的 `ContextInjection`，不要使用 history mutation；
- hydrate 过滤规则应以“来源类型”而不是正文关键词为准。

## 5. 缓存和压缩影响

新的 Skill 方案不会让正文 token 消失：Skill 激活后正文仍会随 durable history 参与后续请求。但它避免了更昂贵的 system prompt 前缀变化：

- Skill 未激活或正文变化，不会重写稳定 system prompt；
- Skill 激活只从新增独立 context message 的位置开始影响缓存；
- Skill 卸载不会改写旧 history，而是从下一次 outbound request 中过滤；
- 压缩后如果活动 Skill 正文不再存在，下一次模型 Step 会按当前 Catalog view 重新注入。

这与已经落地的 request-only `ContextInjection` 是两种不同语义：运行环境不持久化，Skill 正文需要持久化；不能把两者简单合并成同一种注入。

## 6. 验证清单

- system prompt 不包含已加载 SKILL.md 正文；
- skills catalog 仍只包含名称和描述；
- 预加载 Skill 正文位于当前根 user 之前；
- `load_skills` 激活的正文位于 tool result 之后；
- 同一正文不会重复写入 history；
- Skill 正文更新后旧版本不被改写，出站只保留当前 digest；
- unload/clear 后旧正文不再发送给模型；
- hydrate/transcript 不展示 `source=plugin/form=instructions`；
- ContextInjection 仍不写入 history；
- 异步回调、压缩摘要、图像工具消息保持原有恢复和 provider 兼容性。
