# 文档归档

归档材料仅用于理解历史决策、迁移数据或复现旧版本，不代表当前产品行为。当前入口统一从 [../README.md](../README.md) 开始。

## 归档范围

| 目录 | 内容 |
|---|---|
| `design/` | 已被 Workgroup、AgentRef 或当前 Node 架构取代的旧设计 |
| `releases/` | 旧版本 smoke、发布收口和验收记录 |
| `reports/` | 已完成的专项审计、实施计划和一次性验证报告 |
| `experiments/` | 已停止或结论已吸收到当前设计的实验 |
| 根目录文件 | 早期 A2A、Python Agent、旧安全/通信叙事 |

## 阅读规则

- 归档文档中的 API、版本号和目录路径可能失效；不要据此实现新功能。
- 若历史文档与代码冲突，以代码、测试、Schema/OpenAPI 和当前 [架构](../architecture.md) 为准。
- 过程文档迁入归档后，当前文档只保留结论、现行约束和验证入口。

## 现行替代入口

| 历史主题 | 当前入口 |
|---|---|
| 旧 Node/Client 架构 | [../architecture.md](../architecture.md) |
| 旧 Workgroup Placement/A2A | [../user/workgroups.md](../user/workgroups.md)、[../design/workgroup-d05-contracts.md](../design/workgroup-d05-contracts.md) |
| 旧版本验收 | [../development.md](../development.md) 与当前 [UI 回归清单](../design/ui-e2e-regression-checklist.md) |
| 旧兼容性矩阵 | [../user/operations.md](../user/operations.md) 和发布包说明 |

带日期的报告、实验和版本清单分别按 `reports/`、`experiments/`、`releases/` 归档；归档文件内部链接只用于追溯，不作为当前实现入口。
