# 跨 Node 会话持久化（远期）

> **非现网**：当前 Go Node 使用本地 SQLite（`node/internal/store/`）；见 [handbook/02-Agent-Node-核心.md](../handbook/02-Agent-Node-核心.md) §4、[handbook/04-能力与策略.md](../handbook/04-能力与策略.md) §6。

远期若需 **多 Node / 集中 session store**（如 Redis、对象存储），应在三组件模型下设计：

- Client 仍只连本地 Node；**不**引入 Node-to-Node 直连
- 持久化层与 Manage 租户/审计字段对齐
- 迁移路径从单节点 SQLite 导出 JSONL 起步

具体契约待 Phase 2+ 立项后另文撰写。
