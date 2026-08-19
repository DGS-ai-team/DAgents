# Agent 项目对比分析

本目录用于持续记录 DAgents、DeepSeek Harness 和 OpenAI Codex 的技术分析。

## 分析对象

| 项目 | 关注点 |
|---|---|
| DAgents | 本项目自身的架构、能力、缺口和演进路线 |
| DeepSeek Harness | Agent Harness、插件化、能力组合和模型适配 |
| OpenAI Codex | Coding Agent、执行环境、PTY、沙箱和远程开发 |

## 文档约定

本目录的主要对标口径是：`DAgents Node + Web UI` 对标 DeepSeek Harness，DAgents 执行层对标 Codex，Manage 作为 DAgents 自有的企业控制面单独分析。

- `baseline-2026-08.md`：当前基线对比报告。
- 后续报告建议按 `YYYY-MM.md` 或 `YYYY-MM-DD.md` 命名。
- 每次更新至少包含：
  - 项目当前状态；
  - 新增功能和架构变化；
  - 对 DAgents 的启发；
  - DAgents 当前缺口；
  - 可执行的改进建议；
  - 适合向上游提交的贡献方向。

## 维护原则

1. 外部项目的结论应注明来源和检查日期。
2. 区分“已实现”“正在开发”“规划中”和“推测性建议”。
3. 优先记录可验证的接口、协议、目录结构和运行行为。
4. 建议应说明收益、成本、风险和落地优先级。

## 当前报告

- [2026-08-17：Bash 工具专项审查](./bash-tool-review-2026-08-17.md)
- [2026-08 基线对比报告](./baseline-2026-08.md)
- [2026-08-19：Runtime Snapshot 与上下文缓存分析](./runtime-snapshot-cache-analysis-2026-08-19.md)
# 最新增量报告

- [2026-08-17：2026-08-14 至 2026-08-17 项目变化](./delta-2026-08-17.md)
