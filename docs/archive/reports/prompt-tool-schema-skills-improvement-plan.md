# System Prompt、Tool Schema 与 Skills 加载优化计划

> 状态：阶段 0～3.1、3.3 的结构与确定性验证已落地；真实模型已完成需要 Skill 正文的任务专项 n=3 对照；缓存用量已贯通到生命周期与评估，后续隔离 Mimo 样本已观测真实 cache 字段；Skills 目录工具已实现为默认关闭实验，仍需可执行的多任务发现 A/B 决定是否启用
>
> 目标：在不重复注入工具定义、不重新引入动态尾部的前提下，提升模型对工具、skills 和任务完成条件的理解与执行质量。
>
> 参考方向：Codex 的端到端执行协议、DeepSeek Harness 的能力模块化与运行轨迹可追踪设计。

> **2026-08-23 更新**：本计划中早期“加载后把正文注入 system prompt”的描述已由
> Codex 式 Skill context 方案替代。当前正文在激活时作为独立 `name=skill` durable
> message 写入 history；system prompt 只保留 catalog 元数据和选择规则。历史实验数据
> 仍可作为旧架构基线，当前验收以 [`agent-quality.md`](../../design/agent-quality.md) 为准；原审计见 [`non-user-user-message-injection-audit-2026-08-23.md`](./non-user-user-message-injection-audit-2026-08-23.md)。

## 0. 本轮修改计划（2026-08-22）

本轮先固定结构和生效边界，再决定是否改变 Skills 目录的呈现方式。默认方案如下：

| 阶段 | 目标 | 当前状态 |
| --- | --- | --- |
| A. 输入分层 | 明确 system prompt、API tools、tool result/event 的职责，不重复注入工具定义 | 已完成 |
| B. Prompt/Tool Schema | 保留稳定的全局执行契约；工具描述只保留参数、工具专有约束和结果字段 | 已完成 |
| C. Skills 加载正确性 | 元数据与正文懒加载、可见性/同名消歧、加载诊断、hooks 同步和整组替换语义 | 已完成 |
| D. Context 生效边界 | 显式 load/unload/clear 在下一个模型 Step 更新 snapshot；活动 Turn 不被中途改写 | 已完成；活动 Turn 使用冻结 Catalog View |
| E. 新鲜度与缓存 | 避免 mtime/size 盲区，区分目录变化、正文变化和显式 context mutation 的缓存成本 | 已完成当前 provider 的真实 A/B；默认架构保持不变 |
| F. Skills 目录工具化 | 以 `list_available_skills` 做默认关闭的对照实验，只有质量不降且收益明确时才替换 system prompt 目录 | 已实现，默认关闭 |
| G. 效果验收 | 多任务、多轮、长上下文和真实 provider cache 字段 A/B | 已完成本轮限定样本；后续可扩展 provider/业务任务 |

本轮的三个明确决策：

1. 不把工具名称、参数 schema、完整返回格式再复制到 system prompt；工具定义仍通过 API `tools` 参数传入。
2. 暂时保留可用 Skills 的 `name/description` 目录在 system prompt 中。Skills 清单或工具清单变化造成的缓存失效是可接受成本，不为了节省缓存而牺牲模型选 Skill 的直接性。
3. 已加载 Skill 的正文只在显式加载后的下一个模型 Step 作为独立 `name=skill` durable context message 注入；不引入随工具循环不断追加的动态尾部，也不改写 system prompt。`list_available_skills` 已实现为默认关闭实验：只有 Agent snapshot 显式开启 `defaults.skills.catalog_tool_mode=true` 时才替代 system prompt 中的目录发现方式。

## 1. 先明确输入边界

一次模型请求中，三类信息职责不同：

```text
system / instructions
    全局角色、安全边界、执行流程、完成与停止条件

tools API 参数
    工具名称、参数 schema、工具专有行为和输出字段

tool result 消息 / 事件
    本次实际执行状态、错误、业务结果和证据
```

`tools` 不是 system message，但对于模型上下文和缓存来说，二者都属于模型请求输入。不能把完整工具名称、参数和返回说明再次写入 system prompt。

本计划遵循以下单一职责原则：

- system prompt 不重复工具定义。
- 公共执行规则只保留一份。
- 工具专有约束只放在对应的 API tool definition 中。
- 运行时 `tool_result.status`、`error.code`、`retryable` 仍是权威事实。
- system prompt 不随工具执行过程动态追加计划、结果或状态尾部。

## 2. 当前 DAgents 的实际行为

### 2.1 System prompt 和 tools 的当前边界

当前 `node/internal/turn/prompt.go` 的静态提示词已经注明工具用法以当前 tool schema 为准，未把完整工具定义复制到 system prompt。

模型请求的工具定义由 `Registry.Definitions()` 提供。当前 `node/internal/tools/registry.go` 只为工具描述追加工具专属字段和验证提示；公共结果协议已经集中到 system prompt，不再复制到每个 tool definition。

当前 system prompt 的主要段落顺序为：

1. 静态角色、安全和任务执行契约。
2. 工作区目录约定。
3. 外置 CLI 目录说明。
4. 可用 skills 的 name/description 目录。

主机环境、Agent/session 标识、`prompt_context` 和自定义 prompt 作为 request-only
`ContextInjection` 进入当前模型请求；已加载 skills 正文作为独立的
`role=user`、`name=skill` durable context message 进入 history，不属于 system prompt。

这里的最后一段是当前 Turn 的 skills 目录快照，不是执行过程中的动态尾部；活动 Turn 内不会因为磁盘变化而中途改写。

### 2.2 Skills 目录扫描

`skills.Catalog` 在每个 session runtime 创建时绑定：

- skills 根目录。
- 全局启用开关。
- `max_in_prompt` 上限，默认值为 3。
- 可选的可见 skill 白名单。

`Catalog.List()` 使用所有 `SKILL.md` 的目录名、mtime、size 生成签名。签名未变化时复用内存缓存；发生新增、删除、mtime 或 size 变化时重新扫描。

这是 live Catalog 选择的低成本检测策略：目录列举不会读取全部正文，也不为每次模型 Step 计算全量 hash。human Turn 边界随后创建不可变 Catalog View，会对该边界可见的每个 `SKILL.md` 计算一次 SHA-256，作为活动 Turn 的正文版本护栏；这不是每个 model Step 的成本。极端情况下，目录签名仍可能只反映 stat 变化，下一 human Turn 会通过新的边界摘要重新确定版本。

当前重新扫描只读取每个 `SKILL.md` 的文件 stat 和 frontmatter。目录元数据渲染只使用 name/description；正文在 `SelectByName`、`load_skills` 或 token 估算需要时懒加载，并按目录签名缓存。冻结 View 的边界 hash 读取正文 bytes 但不解析/渲染正文，目的是阻止活动 Turn 中的显式加载静默跨版本。

### 2.3 `load_skills` 的当前行为

当前 `load_skills` 是整组替换语义，不是追加语义：

```text
load_skills([a, b])  → 当前 loaded = [a, b]
load_skills([c])      → 当前 loaded = [c]
load_skills([])       → 清空 loaded
```

加载时会发生以下事情：

1. 从 Catalog 按名称查找 skill。
2. 受 `max_in_prompt` 限制，超出部分不会进入 loaded 集合。
3. 更新 session 的 `loadedSkills` 并持久化。
4. 同步加载该 skill 的 hooks。
5. 对显式 skills 变更，session/hook 状态立即更新，并请求在下一个模型 Step 重建 context；重建时把已加载 skill 正文作为独立 skill context message 写入 history。未被显式加载触发的磁盘变化，普通观察路径仍延迟到下一次 human Turn。

`unload_skills` 和 `clear_skills` 会更新 session 状态，并移除对应 hooks。当前已加载 skill 文件受到 `LoadedSkillFileGuardHook` 保护，不能通过受保护的文件工具或可识别的写命令直接修改。

### 2.4 当前 Turn 内的生效边界

模型第一次请求前，`Orchestrator` 创建 `ModelContextSnapshot`，冻结：

- system prompt。
- tool definitions。
- runtime revision/digest。
- prompt/tool digest。

同一个 human Turn 的后续 tool continuation 会复用这个 snapshot。`RunToolMessageTurn` 不会重新构造 system prompt。

因此当前行为是：

```text
模型请求 1：看到旧的 system prompt
    ↓
模型调用 load_skills
    ↓
session loadedSkills 更新，hooks 立即同步
    ↓
模型请求 2：显式 skills 变更触发新的 context snapshot，看到 loaded skill 正文
    ↓
磁盘自动变化：普通观察路径在下一次 human Turn 边界应用
```

当前实现只在显式 `load/unload/clear` 成功改变 loaded 集合时创建新的 context segment；不会中途改写正在进行的模型请求，也不会把执行计划或结果追加到动态尾部。Skill 正文通过独立 context message 在下一模型 Step 可见，稳定 system prompt 不变。`model.context.changed` 会进入生命周期日志，重启回放后仍保留新 snapshot。

### 2.5 外部 skill 文件变化

磁盘上的 skill 变化不会中途改写活动 Turn：

- `Catalog.Revision()` 在新的 human Turn 边界被观察。
- 变化通过 `skills/changed` 通知。
- 当前 Turn snapshot 保持不变。
- 下一次 human Turn 创建新的 Catalog View；模型请求按当前 loaded set 生成独立 Skill context message。

在人类消息正常进入下一轮、没有显式重新加载的情况下，这部分边界是正确的，应继续保留。当前实现已由 human Turn 边界的不可变 Catalog View 提供强隔离：如果外部进程恰好在活动 Turn 中修改了 `SKILL.md`，模型随后调用 `load_skills` 会得到 `catalog_changed`，不会静默读取新版本；下一 human Turn 才获得新的 Catalog View。代价是每个 human Turn 边界对可见 Skill 做一次内容摘要，后续可用文件系统事件/可信内容索引优化，但不能牺牲版本一致性。

事件和诊断应明确区分：

- `detected`：磁盘发现变化。
- `applied_boundary`：将在下一次 human Turn 应用。
- `active_snapshot`：当前模型请求仍使用的版本。

活动 Turn 的 Catalog view 已绑定到 human Turn 边界：活动 Turn 内的 `load_skills` 只从该 view 解析名称、frontmatter 和正文；下一 human Turn 才切换到新的磁盘 revision。正文首次读取会校验边界摘要，发生外部修改时返回 `catalog_changed`，不把新正文静默混入当前上下文。这样工具 Hook、Skill 正文和模型上下文使用同一个版本。

### 2.6 当前一次请求的完整 Skills 流程

```text
Agent 配置
  └─ 工具组包含 skills
       ├─ Registry.Definitions() 暴露 load/unload/clear_skills API tools
       └─ session runtime 创建 skills.Catalog(root, enabled, max_in_prompt)

human Turn 开始
  ├─ observeSkillCatalogChange：比较 revision，变化时发布 skills/changed
  ├─ 构造 system prompt：静态契约 + 可用 name/description 目录
  ├─ 为已加载正文准备独立 name=skill context message
  └─ 创建 ModelContextSnapshot：冻结 system prompt、tools、digest

模型调用 load_skills
  ├─ Catalog 按 logical name/目录名解析并做可见性、同名、容量校验
  ├─ 按目录读取正文；同步当前 loaded skills 的 hooks
  ├─ tool result 返回 requested/loaded/rejected/boundary/hooks 状态
  ├─ session 状态立即持久化
  └─ loaded 集合真的变化时，安排下一模型 Step 重建 context

下一模型 Step
  └─ 使用稳定 system prompt + 完整 Skill context message；历史消息连续保留
```

当 `defaults.skills.catalog_tool_mode=true` 且 skills 工具组可见时，human Turn 的差异仅为：

```text
system prompt：稳定的 Skills 选择指引，不内嵌完整目录
API tools：额外暴露 list_available_skills（默认 limit 10，硬上限 20）
模型调用 list_available_skills
  └─ 返回可见 name/目录名/description 元数据，不读取正文、不修改 loaded 集合、不触发 context refresh
模型随后调用 load_skills
  └─ 仍按原有 next_model_step 边界注入正文
```

该工具是虚拟的 model-facing tool，不加入默认 Registry schema，也不加入 builtin allowlist；只有常规 `load_skills` 已可见且 runtime 实验开关开启时才追加，避免子 Agent/受限工具集意外获得目录发现能力。

因此，当前“加载成功”不是“当前正在执行的模型请求立刻拥有正文”，而是：

- session/hook 状态：立即生效；
- Skill 正文对模型可见：下一个模型 Step；
- 外部磁盘变化：普通观察路径为下一 human Turn；
- 工具结果和生命周期事件：立即记录，作为 UI、审计和恢复的权威事实。

## 3. 主要问题与优化判断

### P0（已修复）：`load_skills` 的状态生效和模型可见性不一致

当前实现已经把这两个边界拆开：session/hook 状态立即更新，模型上下文在下一个模型 Step 更新；磁盘自动变化在下一 human Turn 观察。结果中仍必须持续明确这三个边界，否则会产生以下风险：

- 模型以为 skill 已可用，实际当前请求看不到正文。
- 当前 Turn 的安全 Hook 已经改变，但模型还没有看到对应的操作约束。
- 任务需要 skill 才能完成时，可能出现“加载成功但没有使用”的假完成。

主路径已通过确定性测试和一次真实模型专项 A/B；活动 Turn 内 Catalog 视图冻结已落地。剩余工作是用真实 provider cache 字段和更广任务集验证“目录工具化”是否值得承担额外模型 Step。

### P1：公共工具结果描述存在重复

当前公共结果契约通过工具描述 suffix 追加到多个工具。建议把它拆成：

- system prompt：只保留一次全局行为规则，例如调用后检查、不要猜测、必须验证。
- tool definition：只保留当前工具的输出字段、证据含义和专有失败规则。
- runtime：继续以结构化事件状态为权威。

这不是把工具定义“再写一遍 system prompt”，而是删除重复的公共工具文本，并将少量全局执行规则集中到 system prompt。

### P1（主路径已修复）：Skills 目录扫描超出元数据需求

展示可用 skills 目录时只需要 name/description。当前实现已改为：

- 目录扫描阶段只读取 frontmatter 和文件 stat。
- skill 正文在 `load_skills` 成功后按需读取。
- snapshot 诊断区分 loaded 集合 digest 和已加载正文 digest。

已补充：目录元数据扫描、正文读取、缓存命中、边界摘要和 token 估算均有独立耗时/计数，并通过 context 诊断暴露；当前 `EstimateCatalogStats` 为计算最大正文 token 仍会读取全部正文，但只在 context 诊断路径执行，不进入每个模型 Step 的热路径。

### P1：活动 Turn 的 Catalog 新鲜度和版本一致性

当前 `Catalog` 的缓存签名是目录名、`SKILL.md` 的 mtime 和 size。它能低成本发现大多数新增、删除和修改，但存在两个边界：

- 同一文件系统时间精度内，内容改成相同 size 时可能无法发现变化；
- `load_skills` 与 system prompt 构造共用 live Catalog，外部修改可能在活动 Turn 的显式加载路径提前生效。

已按以下顺序落地：

1. 在 human Turn 开始时生成不可变 Catalog view，记录 revision、可见定义和正文边界 digest。
2. 活动 Turn 内的 `load_skills`、正文渲染和 hooks 路径统一使用该 view；外部新 revision 只发 `skills/changed`，不侵入当前 Turn。
3. 对正文读取增加内容 digest 校验，仅在边界刷新和显式加载路径执行，不把 hash 放入每个 model Step。
4. 在 snapshot/lifecycle 中保留 revision、loaded-set digest、body digest 和 `applied_boundary`，便于确认缓存断点来自哪一类变化。

剩余优化：若 Turn 边界的全量正文 hash 在大目录上成为可观测瓶颈，可引入文件系统 watcher 或持久化 content index 作为候选优化；在此之前保留当前实现，因为“活动 Turn 不跨版本”优先级高于边界扫描的少量 IO。

### P1：加载结果对模型不够明确

当前返回的 `loaded_skills` 已补充说明：

- 哪些请求名称成功匹配。
- 哪些名称不存在或不可见。
- 哪些因上限被截断。
- 正文何时对模型可见。
- hooks 是否同步成功。

当前返回结构化结果：

```json
{
  "action": "set_loaded_skills",
  "requested": ["writer", "reviewer"],
  "loaded_skills": ["writer"],
  "rejected": [{"name": "reviewer", "reason": "not_visible"}],
  "model_context_applied_boundary": "next_model_step",
  "hooks_status": "synchronized",
  "hooks_loaded": ["writer-hook.so"],
  "hooks_failed": []
}
```

如果请求没有改变 loaded 集合，`model_context_applied_boundary` 为 `unchanged`，不会制造无意义的 context/cache 断点。

### P2（已完成）：name、目录名和 hooks 路径需要强校验

当前 skill 可以从 frontmatter 读取 `name`，而正文和 hooks 路径使用目录名；`Definition` 已同时保存：

```text
directory_name
skill_name
```

后续渲染、正文读取、hooks 加载、文件保护分别使用正确的标识；同一 Skill 的两个合法别名会 canonicalize 去重，多个目录声明同一逻辑 name 会返回 `ambiguous`，避免静默选错。

### P2：整组替换语义需要更明确

整组替换本身可以保留，但必须在工具描述、结果和测试中明确。否则模型连续调用：

```text
load_skills([a])
load_skills([b])
```

可能误以为最终同时加载了 a、b，实际只剩 b。

控制面 API 的单 skill load/unload 保持“追加/移除单项”语义，但现在也返回 `requested`、`rejected`、
`changed`、hooks 状态以及 `next_human_turn` / `next_model_step` 边界，前端不需要根据 loaded 数组变化猜测是否生效。

### P2：hooks 状态还需要从“同步路径完成”升级为“逐项注册结果”

本轮已完成首层修正：结果中同时返回 `hooks_status`、`hooks_loaded`、`hooks_failed`；插件加载失败会返回 `partial` 和失败 skill/原因，避免把“skill 正文已生效”和“skill hooks 全部注册成功”混为一谈。后续仍可把失败粒度从 skill 细化到每个插件文件。

### P1：工具 status 的模型可见边界（已补齐）

当前 `tool_result` SSE/生命周期事件始终带有统一 `status`、`error` 和 `retryable`，这是 UI、审计和运行时的权威事实。模型继续请求时，OpenAI messages 中的 `tool` 消息现在也会在原始正文前获得紧凑的 `[TOOL_RESULT_METADATA]`，因此不再依赖模型读取 SSE 包络；正文仍保持原格式。

已落地的模型侧结果适配层：

- 给模型可见的 tool message 提供稳定、低开销的 status/error 元数据。
- 保持前端 SSE、hydrate transcript 和历史正文兼容，不把 UI 展示格式强行改成新 envelope。
- 对 JSON 工具避免二次嵌套；对纯文本工具提供明确的元数据段。
- 在 system prompt 中区分运行时事件和模型消息中的 status，并通过 `MessageToAPIPayload` 防止内部字段泄露到 provider payload。

仍需用真实模型观察元数据增加的 token/cache 代价，但不再存在“模型无法看到统一 status”的协议缺口。

## 4. 目标设计

### 4.1 Prompt 与 Tool Schema 分层

目标结构：

```text
System prompt
  - 角色与安全边界
  - 任务执行闭环
  - 全局停止条件
  - 全局工具结果处理行为

API tools
  - 每个工具的用途
  - 参数 schema
  - 副作用与权限边界
  - 工具特有输出字段
  - 工具特有验证、重试和异步规则

Runtime events/tool results
  - status
  - error
  - retryable
  - rejected
  - 业务证据
```

不把工具名称、参数表、完整返回格式复制到 system prompt。

### 4.2 Skills 的两阶段加载语义

建议将 skill 加载拆成两个明确阶段：

```text
catalog available
    ↓ load_skills
session requested/validated
    ↓ context mutation boundary
model-visible loaded skill
```

磁盘自动变化仍只在下一次 human Turn 应用；显式 `load_skills` 则需要决定下一步采用哪种策略：

#### 策略 A：保持当前 Turn snapshot 不变（不采用）

- `load_skills` 结果明确返回 `applied_boundary: next_human_turn`。
- 当前 Turn 不使用 skill 正文。
- 成本和上下文连续性最好，但模型质量较弱。

#### 策略 B：在显式 skills 变更后创建新的模型上下文段（已采用）

- `load_skills` 成功后，当前 Step 正常结束。
- 下一次模型请求重新构造 system prompt 和 snapshot。
- 仍然保留完整历史，不引入动态尾部。
- 只在显式 `load/unload/clear` 这种受控边界发生 system prompt 变化。
- 接受该次 skills 正文导致的缓存断档。

考虑到项目目标是提升 agent 质量，当前采用策略 B；磁盘自动变化仍遵守下一次 human Turn 边界，只有显式 skills 工具变更才触发 context segment replacement。

### 4.3 Skills 元数据的长期方向

当前可用 skills 目录位于 system prompt 尾部。短期先保持功能不变，避免同时改变模型选 skill 的行为。

中期评估将目录元数据从 system prompt 移到显式 `list_available_skills` 工具结果：

- system prompt 更稳定。
- catalog 修改不会改变工具 schema 或 system 前缀。
- 目录结果进入 tool result/history，而不是每个请求的固定前缀。
- 代价是首次选 skill 可能增加一次工具调用。

该方案必须通过质量和成本 A/B 后再实施，不能只按缓存成本单方面决定。

## 5. 分阶段实施清单

### 阶段 0：事实、观测和回归基线

- [x] 增加 `load_skills` 同一 human Turn 内的回归测试，确认下一个模型 Step 的正文可见。
- [x] 增加 system prompt digest、tool digest、catalog revision、loaded skill digest 的请求级记录。
- [x] 记录 skill 变化的 detected/applied boundary。
- [x] 记录 requested/loaded/rejected/truncated/hook status。
- [x] 在现有质量评测中增加 skill 选择、加载后使用、卸载后不再使用三个场景。

### 阶段 1：工具描述去重

- [x] 明确 system prompt 只放全局执行行为，不放工具定义。
- [x] 将 `ResultDescriptionSuffix()` 的公共文本从逐工具重复追加改为单一全局协议。
- [x] `ResultDescriptionSuffixForTool()` 只保留工具专有字段和专有规则。
- [x] 将公共结果协议集中到 system prompt；工具定义不再重复公共协议。
- [x] 审核 `externaltools` prompt catalog 与 API tools 是否存在重复，删除真正重复部分（结论：它是 bash 可发现性目录，不是 API tool schema；没有公共结果契约重复，因此不改动）。
- [x] 更新 tool schema 快照测试和 prompt token 估算测试。

### 阶段 2：Skills Catalog 与 Definition 优化

- [x] 将目录元数据扫描与正文读取拆开，正文按需加载。
- [x] Definition 同时保存目录名和逻辑 skill name，加载时按目录名读取正文和 hooks。
- [x] `load_skills` 结果补充请求、成功、拒绝、截断、边界和 hooks 状态。
- [x] 移除 `load_skills` schema 对 System Prompt 全局“匹配时必须加载”规则的重复描述，保留工具专有的替换语义和生效边界。
- [x] 技能结果的集合字段稳定返回数组；无 hook 时 `hooks_loaded=[]`，无失败时 `hooks_failed=[]`。
- [x] 对 `max_in_prompt` 超限从静默截断改为可诊断结果。
- [x] 对 frontmatter name 与目录名两个合法别名按 canonical directory 去重，避免同一 skill 重复占用 prompt 配额或重复注册 hooks。
- [x] 对多个目录声明相同逻辑 name 的情况返回 `ambiguous`，并在目录元数据中展示目录名，避免模型静默选错 Skill。
- [x] 在 context snapshot 诊断中区分 loaded skill 名称集合 digest 与实际正文 digest，不把正文写入生命周期元数据。
- [x] 完善整组替换语义的工具描述和模型评测。
- [x] 为 Agent 控制面 skills load/unload API 补充拒绝诊断、hooks 状态和模型上下文生效边界。

### 阶段 3：显式 Skill Context Mutation

- [x] 实现策略 B：显式 load/unload/clear 后在下一个模型 Step 创建新 context snapshot。
- [x] 保证普通磁盘变化观察只在下一次 human Turn 应用，活动 snapshot 不被中途重写。
- [x] 冻结活动 human Turn 的 Catalog view，避免显式 `load_skills` 在活动 Turn 内读取外部新 revision；正文边界摘要不匹配时返回 `catalog_changed`。
- [x] 保证 snapshot 恢复时包含 system prompt、tool schema、skills revision 和正文 digest。
- [x] 通过确定性质量场景验证边界、加载诊断和卸载行为。
- [x] 通过真实模型 A/B 验证质量、cache miss、token、工具步数和任务完成率；已完成首轮 n=3、目录发现 n=3 和多轮/长上下文 3×3 探索，provider cache 字段均可采集。结果支持保留默认 system catalog，后续仅做扩展性观测。

### 阶段 3.1：模型侧工具结果状态

- [x] 确认模型请求实际只能读取 history/tool message，不能读取 SSE 包络中的 `status`。
- [x] 设计并实现不破坏正文格式的模型侧 status/error 元数据适配层。
- [x] 覆盖成功、拒绝、失败、空结果、历史回放和 provider payload 隔离；异步结果沿用正文中的 status，待真实回归继续观察 token/cache 代价。
- [x] 将模型侧 metadata 的 token 开销纳入统一 `EstimateMessageTokens`，避免 compression/context 预览低估请求输入。
- [x] 将 provider 返回的 cache hit/miss、reasoning tokens 和“是否实际返回 cache 字段”写入 `StepUsage`/`TurnUsage`、生命周期事件和质量评估聚合。

### 阶段 3.2：Cache Observability

- [x] 统一解析 OpenAI `prompt_tokens_details.cached_tokens` 与 DeepSeek `prompt_cache_*_tokens`。
- [x] 区分“provider 未返回 cache 指标”和“明确返回 0 命中”，避免把未知状态误报为 0% 命中。
- [x] SSE、生命周期事件、回放 projection 和质量评估均保留 cache 观测字段。
- [x] 在实际 provider 返回 cache 字段的环境中完成 3×3 探索性长上下文、多轮和 context mutation A/B；结果证明边界正确，但不支持启用目录工具。

### 阶段 3.3：Catalog 新鲜度与成本控制

- [x] 为 human Turn 建立不可变 Catalog view，并让 `load_skills`、正文渲染和 hooks 同步使用同一 view。
- [x] 增加同 size/同 mtime 修改的内容新鲜度回归测试，确认 body cache 不返回旧正文。
- [x] 将 metadata scan、body load、token estimate 分别计时并通过 context 诊断暴露；全量正文估算只在 context 诊断路径执行，不进入普通模型 Step 的 prompt 构建热路径。
- [x] 在不改变模型可见行为的前提下，分别验证 Catalog revision、Skill 正文和 loaded set 的 context/cache 边界：正文变化在活动 Turn 返回 `catalog_changed`，Catalog revision 在下一 Human Turn 生效，loaded set 只在下一个模型 Step 更新 snapshot。

### 阶段 4：可选的 skills catalog 工具化

- [x] 设计 `list_available_skills` 的返回上限、截断、搜索和权限过滤（见 [`skills-list-tool-experiment.md`](../experiments/skills-list-tool-experiment.md)）。
- [x] 实现默认关闭的实验开关和 deterministic contract test；实验模式不得改变默认 system prompt/catalog 行为。
- [x] 对比“system prompt 内置目录”和“显式 list 工具”两种方案；已完成修正配置后的真实 Mimo 发现 A/B，但出现额外 Step、工具失败和一组循环长尾，结论暂为不启用。
- [x] 已按门槛评估目录工具：虽然发现链路可执行，但存在额外模型 Step、工具失败和循环长尾，且长上下文 cache 无明确收益；因此门槛未满足，明确保持 system prompt 目录并关闭实验开关。

## 6. 验收标准

### 正确性

- 模型不会把 `load_skills` 的 session 状态变化误认为正文已经可见。
- 加载成功、加载失败、名称不可见、超限截断均有明确结果。
- active Turn 不会因磁盘 skill 文件变化而被中途改写。
- 已加载 skill 文件保护和 hooks 状态与模型可见上下文一致。

### 上下文与缓存

- system prompt 不包含工具定义副本。
- 工具公共结果说明不在每个工具中重复增长。
- 可观测到 prompt/tool/skills digest 和每次 context mutation 的边界。
- 明确记录显式加载造成的缓存断档，而不是隐式发生。

### Agent 效果

- skill 选择准确率不下降。
- 加载 skill 后实际使用率提升。
- 工具调用成功但任务未完成的假完成率下降。
- 任务完成率提升不能以无界增加模型调用次数为代价。

## 7. 当前建议

阶段 0～3 的确定性实现已经落地；默认 runtime 仍保留现有 system prompt catalog，同时已把新工具做成可回滚、默认关闭的实验路径，等待真实模型 A/B 数据。

当前已获得的首轮问题验证是：

> 对于需要 skill 正文才能完成的任务，在 `load_skills` 后立即创建新的模型上下文段，真实模型完成率提升是否足以覆盖缓存断档成本？

当前 n=3 任务专项结果支持保留策略 B；虽然后续隔离 Mimo 样本已经观测到 provider cache 字段，但现有样本主要是显式 Skill 路径，不足以比较目录发现方案，暂不推进 `list_available_skills` 工具化。

本轮在 Catalog View 落地后维持同一决策：当前 system prompt catalog 已经让真实模型稳定完成 Skill 选择和加载；`list_available_skills` 至少会增加一次模型工具步，在没有真实 cache 收益证据前不值得以额外复杂度换取理论上的前缀稳定性。因此保留默认关闭，只允许通过 runtime snapshot 显式开启实验。

## 8. 本轮落地验证

- `go test ./node/... ./client/... ./shared/config/...`：通过。
- 默认确定性质量场景：17 个；skills 相关场景覆盖加载边界、目录版本边界、加载诊断、同名消歧、卸载边界和加载后验证；全部评估测试通过。
- 全量回归：`go test ./node/... ./client/... ./shared/config/... -count=1` 通过；Web UI `46` 个测试文件、`278` 个测试通过，生产构建通过（仅保留既有 chunk size warning）。
- 模型侧工具结果协议测试：成功、失败、拒绝、空正文、JSON 正文、历史 journal 持久化、provider payload 隔离和真实 Step 请求均有覆盖。
- skills 结构化 trace 评测：相关场景全部通过，criterion pass rate 为 1.0。
- 生命周期回放：新增 `model.context.changed` 回放测试，确认重启恢复后仍使用新 prompt/context snapshot。
- 真实模型任务专项 A/B（旧 system-body 基线，隔离 Node、最终源码构建、Mimo 配置）在“skills 验收报告”上完成 3 组有效对照：3/3 treatment 调用 `load_skills`、产生 1 次 `model.context.changed`、第二 snapshot 含完整 skill 正文，最终均以 `QUALITY_GATE_OK` 开头；3/3 control 无 skills 工具调用，未完成技能契约。treatment 平均 2 次模型请求、1 次工具调用、约 5,088 input tokens；control 平均 1 次请求、0 次工具调用、约 1,121 input tokens。当前独立 Skill context 实现的行为以本报告更新和专项审计为准。
- 额外跨任务对照覆盖“变更验收”和“故障诊断”：两个 treatment 都完成 skills 工具调用和 context mutation；故障诊断按正文输出 `INCIDENT_REPORT_OK`，变更验收在缺乏真实测试证据时按 skill 要求输出 `EVIDENCE_INCOMPLETE`；两个 control 均无 skills 工具调用并产生伪工具调用文本。该结果证明当前边界在多个任务上可工作，并支持策略 B，但样本仍小、只使用一个模型配置，不能替代全面效果结论。
- 真实模型新增的工具 metadata 也已被模型实际读取：treatment 最终答案引用了 `[TOOL_RESULT_METADATA]` 中的 `status=succeeded`，而 hydrate 中保留的 tool 正文不含该标记，证明请求侧适配与历史/UI 隔离成立。
- Catalog View 回归已覆盖 Catalog 单元、Turn/Session 集成和 race：活动 Turn 中对 `SKILL.md` 做同大小甚至同 mtime 的外部修改时，模型侧 `load_skills` 得到 `catalog_changed`，不会读取新正文；下一 human Turn 观察到新 revision 后，system prompt 更新 metadata，Skill 正文在激活时进入独立 context message。
- `list_available_skills` 已覆盖配置解析、默认关闭、API tool schema、受限工具集不泄露、稳定搜索/分页、元数据-only 返回和“不触发 context refresh”；默认模式的 system prompt 与 Registry tools 保持不变。
- 详细样本、方法与限制见 [`prompt-tool-schema-skills-ab-report-2026-08-22.md`](./prompt-tool-schema-skills-ab-report-2026-08-22.md)。
- 因此当前结论是“结构、确定性行为、模型侧 status 协议、Catalog 成本诊断和一组 n=3 真实任务对照已完成”；仍需可执行的多任务目录发现、多轮长上下文和 context mutation cache A/B 后再决定是否进一步改造 skills catalog。
- 额外发现：技能工具结果的空集合字段已固定为数组。
- 额外修正：工具 status/error 已通过请求侧 metadata 适配层进入模型可见的 tool message；历史和前端仍保留原始正文，避免把状态协议重复注入工具定义或 UI 展示。
- cache 观测链路已补齐：OpenAI/DeepSeek cache 字段统一归一化，`prompt_cache_available` 区分未知与明确零命中；`StepUsage`/`TurnUsage`、`model.usage.recorded`、SSE 和质量评估均可读取命中/未命中 token。后续隔离 Node 的 Mimo 样本已确认 provider 会返回 cache 字段（例如 `prompt_cache_hit_tokens` / `prompt_cache_miss_tokens`），因此真实 cache A/B 已具备前置条件；此前报告中的“未观测”仅适用于首轮样本，需修正为“首轮未观测”。
- 质量评估新增 `eval.CompareAB`：相同场景不足 3 个、双方样本不完整或 cache 未全量观测时只输出差值和 `inconclusive`，不会自动推动策略变更。
- 真实 cache A/B 的执行步骤、权威事件采集和结论门槛见 [`prompt-tool-schema-skills-cache-ab-runbook.md`](../experiments/prompt-tool-schema-skills-cache-ab-runbook.md)。本轮已补齐同一 provider 的多轮、显式 mutation 和长上下文探索性样本；下一步只需在新增 provider 或真实业务任务集上持续观测，不再改变当前默认架构。
