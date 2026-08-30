# Agent 质量与效果基线

> **状态**：当前质量方向（v0.10.4）。这是原则和验收入口，不是某一次实验的结果；带日期的分析、A/B 记录和回归报告见 [`docs/archive/reports/`](../archive/reports/)。

## 1. 目标

在可控成本下优先保证模型“知道自己能做什么、能安全执行、能验证结果、能在中断后继续”。效果问题必须能归因到 prompt、tool schema、上下文、运行时状态或 UI 事件，而不是依赖模糊的 loading 状态。

## 2. 当前上下文契约

| 内容 | 传给模型的方式 | 变化边界 |
|---|---|---|
| 稳定行为规则、工作区约定 | system prompt | 仅在稳定配置/Hook/Agent 快照变化时重建 |
| Session 身份、主机快照和其他运行时状态 | request-only `ContextInjection` | 仅在目标 Session 的请求快照中注入，不写入历史 |
| 工具定义 | API `tools` 字段 | 工具清单或 schema 变化时接受前缀缓存失效 |
| Skill 目录 | context boundary 时写入 system prompt；实时变化由 `list_available_skills` 查询 | 新会话、压缩或其他上下文重建时刷新 |
| Skill 正文 | 独立 `role=user` ContextInjection/context message | `load/unload` 在下一个模型 Step 生效 |
| 用户输入、工具结果、成员产出 | 合法消息序列/结构化来源 | 只追加，不用动态 system prompt 伪造状态 |
| 当前 terminal、附件、焦点页面 | 带 provenance 的 Session ContextInjection | 仅注入目标 Session，生命周期明确 |

不引入“动态尾部”去承载实时状态。缓存命中率下降只有在 system/tools/历史前缀确实变化时才接受；UI 展示的状态不应改变模型输入。

## 3. 工具契约

- 工具名称、参数 schema、必填字段和副作用级别必须明确；工具描述解释何时使用、边界、结果格式和失败处理。
- 工具通过 API 定义发送，system prompt 只规定选择原则和安全约束，避免重复 schema。
- 返回值使用结构化 envelope：状态、摘要、stdout/stderr、截断信息、媒体引用和可重试性分开表达。
- 同一工具调用必须有稳定的 `tool_call_id`，结果只能配对一次；异步回调不得重复执行或重复展示。
- 危险工具由 policy/HITL 决定；审批卡片必须包含工具、参数摘要、风险和单项/批量范围。

## 4. Turn/Step 效果边界

一个 Turn 是对一个 human message 的完整编排链；一个 Step 是一次 LLM 请求及其紧邻的工具分流/结果闭合。每个 Step 固定自身的模型、system prompt、tools 和历史快照。

```text
human message
  → Step 0: LLM
  → tool_call / HITL
  → Step 1: tool result + LLM
  → …
  → turn_finished（仅 turn 完成）；HITL 等待由 hitl_required + turn_state 表达
```

取消、压缩、skill 变更和异步结果都必须在明确的队列/Step 边界处理；不能在流式输出中途悄悄替换 prompt 或工具定义。

## 5. 结果质量规则

模型完成任务前应：

1. 识别目标、约束和不可逆操作；
2. 选择最小足够工具集，避免无关探索；
3. 读取/执行后检查结构化状态和真实输出；
4. 失败时说明原因、保留证据，不把部分成功包装为完成；
5. 最终回答区分事实、推断、未验证项和下一步建议。

## 6. 验收矩阵

| 场景 | 通过条件 |
|---|---|
| 多轮长上下文 | 无环境变化/压缩时，累计缓存统计不出现非预期断崖 |
| skill 加载 | 正文只在加载后的 Step 注入；不重复进 system prompt |
| 工具审批 | 单项与批量语义清楚，卡片不重复，resume 只生效一次 |
| 异步工具 | started/result 顺序正确，断线恢复不重复执行或展示 |
| cancel/clear | 第一条新 human message 不丢失；旧结果不能污染新 generation |
| Workgroup | Supervisor 工具与成员工具隔离；成员 Session/Timeline 不串线 |
| terminal/browser | 目标 Session 可见输入输出，工具状态与 UI 权威事件一致 |
| 失败恢复 | accepted 未知副作用进入 `indeterminate`，不自动重做非幂等操作 |

## 7. 相关实现

| 主题 | 入口 |
|---|---|
| Prompt / tool snapshot | `node/internal/turn/` |
| Skill 加载 | `node/internal/skills/` |
| ContextInjection | `node/internal/turn/`、`node/internal/session/` |
| 事件与 UI 状态 | `node/internal/stream/`、`node/webui/frontend/src/stores/` |
| 压缩与缓存 | `node/internal/compression/`、[`context-compression-cache-analysis.md`](./context-compression-cache-analysis.md) |
| 工具规则 | [`../handbook/04-能力与策略.md`](../handbook/04-能力与策略.md)、[`../../node/internal/tools/REFERENCE.md`](../../node/internal/tools/REFERENCE.md) |
