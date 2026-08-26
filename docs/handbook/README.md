# DAgents 项目手册（兼容入口）

**版本**：与代码同步（当前发布 **v0.10.4**）

当前文档入口已整理为：

- [用户指南](../user/README.md)：安装、启动、工作组和运维；
- [架构](../architecture.md)：跨组件行为、Session/Turn/Step、恢复与数据流；
- [开发与验证](../development.md)：构建、测试、发布和文档规则；
- [参考资料](../reference/README.md)：API、配置、工具、事件和 Schema。

本目录保留原有中文章节路径，方便旧链接和习惯使用者继续查阅。章节只承担主题索引和补充说明，不应重新定义与新入口不同的版本或协议。

## 章节索引

| 章节 | 主题 | 当前入口 |
|---|---|---|
| [00 · 导读](00-导读.md) | 读者路径、术语、源码导航 | [docs README](../README.md) |
| [01 · 愿景与架构](01-愿景与架构.md) | 产品边界和组件关系 | [架构](../architecture.md) |
| [02 · Agent Node 核心](02-Agent-Node-核心.md) | Session、Queue、Turn、Step | [架构](../architecture.md) + 子系统 README |
| [03 · API 与 Client](03-API与Client.md) | Node API、SSE、Web UI | [参考资料](../reference/README.md) |
| [04 · 能力与策略](04-能力与策略.md) | Tools、Policy、Skills、Compression | [内置工具参考](附录/内置工具参考.md) |
| [05 · Manage 与协作面](05-Manage与A2A.md) | Registry、Workgroup、WS | [工作组指南](../user/workgroups.md) |
| [06 · 运维与案例](06-运维与案例.md) | 启动、测试、发布、诊断 | [开发与验证](../development.md) |
| [07 · Workgroup 协作](07-Workgroup协作.md) | 工作组用户操作 | [工作组指南](../user/workgroups.md) |

## 附录

| 文档 | 用途 |
|---|---|
| [内置工具参考](附录/内置工具参考.md) | 工具描述、参数和审批边界 |
| [配置项参考](附录/配置项参考.md) | 配置键和默认值 |
| [SSE 事件速查](附录/SSE事件速查.md) | Node 事件 |
| [Prometheus 观测](附录/Prometheus观测.md) | Manage 指标 |
| [术语表](附录/术语表.md) | 统一词汇 |
| [重大设计变更实录](附录/重大设计变更实录.md) | 历史索引，非现行规范 |

修改接口或运行行为时，先同步代码/Schema，再更新新入口，最后视需要调整本目录兼容章节。
