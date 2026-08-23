# Agent Quality Evaluation

`node/internal/eval` 是 DAgents 的确定性 Agent 质量评测基础包。它不直接调用模型，也不依赖 Session Runtime；模型、Fake LLM 或真实 Turn 的适配器只需要将权威生命周期事实转换为 `eval.Trace`，即可复用同一套场景和评分规则。

## 当前内容

- `DefaultScenarios()`：首批 17 个稳定 ID 的黄金场景，覆盖指令遵循、工作区、bash、Linux 远程执行、失败恢复、异步回调、取消、HITL、Skills（含目录版本边界和同名消歧）和完成验证。
- `EvaluateScenario`：对单个场景执行确定性断言。
- `EvaluateSuite`：对一组 Trace 生成任务成功率、断言通过率、验证覆盖率、取消栅栏违规数，以及 Step、工具失败、重试、耗时、token、成本和缓存观测聚合值；缓存未被 provider 返回时必须标记为未观测，不能当作 0% 命中。
- `CompareAB`：在相同场景集合上比较 treatment/control，按质量优先、成本单独报告的规则给出 `treatment_supported`、`control_supported` 或 `inconclusive`；双方没有完整 cache 观测时不比较命中率。
- 缺少 Trace 时显式判定为失败，避免评测因为数据缺失而虚假通过。

## 运行

```text
go test ./node/internal/eval -count=1
```

后续接入真实运行时适配器时，应从 TurnCoordinator、生命周期事件和工具执行事实构造 Trace，不要从前端文本或模糊状态推断。

真实 A/B 运行建议：先用相同 scenario ID 生成两组 Trace，再调用 `CompareAB`。少于 3 个双方都有结果的场景只返回指标，不给方向性结论；质量提升可以接受额外 token，但必须在报告中保留成本差值。
