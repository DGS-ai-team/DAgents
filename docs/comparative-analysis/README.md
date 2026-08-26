# 对标分析

本目录记录 DAgents 与两个开源项目的可验证对比，不定义 DAgents 的现行 API。报告必须区分：上游事实、对 DAgents 的推论、建议方案和已落地结果。

## 对标对象

| 项目 | 上游入口 | 主要借鉴面 |
|---|---|---|
| OpenAI Codex | [openai/codex](https://github.com/openai/codex) · [docs](https://github.com/openai/codex/tree/main/docs) | coding-agent 用户/开发者文档、执行环境、审批、上下文约束、可验证贡献流程 |
| DeepSeek Harness | [deepseek-ai/deepseek-harness](https://github.com/deepseek-ai/deepseek-harness) · [architecture](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/architecture.md) | 分层架构、Session/Turn/Step、子系统参考、插件/能力 seam、模型可见上下文 |
| DAgents | 本仓库 [架构](../architecture.md) | Node + Web UI、Workgroup、Node→Manage WS、本地优先治理 |

## 组织原则

报告按主题和日期维护，日期表示观察窗口，不表示当前代码状态：

- 先写“检查日期、代码/上游 commit、事实证据”；
- 再写“差异、收益、成本、风险和 DAgents 可落地项”；
- 结论落地后，更新当前架构/参考文档，并把临时计划或验证记录归档；
- 不把外部项目的内部命名直接复制到 DAgents，不以推测替代接口、目录和运行行为。

## 当前报告

| 报告 | 用途 |
|---|---|
| [baseline-2026-08.md](baseline-2026-08.md) | 三个项目的总体基线 |
| [delta-2026-08-17.md](delta-2026-08-17.md) | 上游增量观察 |
| [bash-tool-review-2026-08-17.md](bash-tool-review-2026-08-17.md) | Bash/终端工具对比 |
| [runtime-snapshot-cache-analysis-2026-08-19.md](runtime-snapshot-cache-analysis-2026-08-19.md) | Snapshot、上下文和缓存 |
| [execution-optimization-checklist-2026-08-19.md](execution-optimization-checklist-2026-08-19.md) | 执行层优化清单 |

后续新增报告建议采用 `YYYY-MM-DD-topic.md`，并在本页登记；已被实现吸收的报告迁入 `docs/archive/reports/`。
