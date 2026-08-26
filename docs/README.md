# DAgents 文档

**当前基线**：v0.10.4（2026-08-25）。本目录按“用法、架构、开发、参考、设计、研究、历史”分层；每层只回答一种问题。

## 从这里开始

| 你想做什么 | 入口 |
|---|---|
| 第一次运行 DAgents | [用户指南](user/README.md) → [快速开始](user/getting-started.md) |
| 理解 Node、Session、Turn、Workgroup | [架构](architecture.md) |
| 修改代码、运行测试、发 PR | [开发与验证](development.md) |
| 查 API、配置、工具、事件、Schema | [参考资料](reference/README.md) |
| 使用多 Node 工作组 | [工作组指南](user/workgroups.md) |
| 查看当前设计约束 | [设计文档](design/README.md) |
| 查看 DAgents 与 Codex / DeepSeek Harness 对比 | [对标分析](comparative-analysis/README.md) |
| 查看未来路线 | [Roadmap](roadmap.md) |

## 目录职责

| 目录 | 内容 | 是否作为当前行为依据 |
|---|---|---|
| [`user/`](user/README.md) | 已实现的安装、使用和运维路径 | 是 |
| [`architecture.md`](architecture.md) | 跨组件边界、生命周期、恢复和数据流 | 是 |
| [`development.md`](development.md) | 开发环境、测试、发布和文档规则 | 是 |
| [`reference/`](reference/README.md) | API、配置、工具、事件和 Schema 索引 | 是，字段以代码/Schema为准 |
| [`subsystems/`](subsystems/README.md) | Node/Manage 子系统和源码导航 | 是 |
| [`design/`](design/README.md) | 仍影响实现的设计决策和契约 | 仅标注为“已实现/冻结”的部分 |
| [`comparative-analysis/`](comparative-analysis/README.md) | 外部项目研究、实验和对标建议 | 否，不直接定义产品行为 |
| [`archive/`](archive/README.md) | 退役版本、旧方案、历史报告 | 否 |

## 真相来源

发生冲突时按以下顺序判断：

1. 当前代码、自动化测试和运行时行为；
2. JSON Schema / OpenAPI / 配置样例等机器可验证契约；
3. 本目录的架构、用户和参考文档；
4. 设计方案、对标分析和路线图；
5. CHANGELOG 只说明版本变化，不替代接口契约。

## 文档维护约定

- 当前文档不使用日期命名；日期只用于研究报告、实验记录和历史归档。
- 设计文档文首必须说明状态、范围、非目标、验证门槛和现行实现入口。
- 外部项目分析必须写明仓库链接、检查日期、事实与推测的区别。
- 版本验收清单归档后，不再出现在当前设计入口；持续验证使用 [开发与验证](development.md) 和当前 UI 清单。
- 代码同目录的 `README.md` 讲职责和协作，`REFERENCE.md` 讲符号与字段，避免把实现细节复制到总览。

旧路径仍保留为兼容导航：[`handbook/`](handbook/README.md)、[`architecture/`](architecture/README.md)。新内容应优先写入本页列出的当前入口。
