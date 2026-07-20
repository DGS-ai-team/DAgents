# 技术文档

> **唯一正文入口** → **[handbook/README.md](./handbook/README.md)**  
> **v0.8 架构重构** → **[design/agent-instance-model.md](./design/agent-instance-model.md)**

---

## 快速入口

| 需求 | 文档 |
|------|------|
| **v0.8 Agent 实例 / 沙箱 / node_id** | [design/agent-instance-model.md](./design/agent-instance-model.md) |
| 新人 / 联调 | [handbook/00-导读.md](./handbook/00-导读.md) → [01-愿景与架构.md](./handbook/01-愿景与架构.md) |
| 改 Node 内部 | [handbook/02-Agent-Node-核心.md](./handbook/02-Agent-Node-核心.md) |
| HTTP/SSE | [handbook/03-API与Client.md](./handbook/03-API与Client.md)（将改为 Web UI-only） |
| 工具 / 压缩 / policy | [handbook/04-能力与策略.md](./handbook/04-能力与策略.md) |
| Manage / A2A | [handbook/05-Manage与A2A.md](./handbook/05-Manage与A2A.md)（后续重构） |
| 打包 / 案例 | [handbook/06-运维与案例.md](./handbook/06-运维与案例.md) |
| 内置工具全表 | [handbook/附录/内置工具参考.md](./handbook/附录/内置工具参考.md) |
| 术语 | [handbook/附录/术语表.md](./handbook/附录/术语表.md) |

---

## 专题（与手册互补）

| 路径 | 说明 |
|------|------|
| [architecture/](./architecture/) | Node 内部结构、HTTP 契约、打包兼容 |
| [design/](./design/) | 设计实录与专题分析 |
| [future/](./future/) | 尚未完全落地的远期方案 |
| [manage-communication.md](./manage-communication.md) | Manage / Node / Client 通信 |
| [roadmap.md](./roadmap.md) | 产品路线图 |
| [cases/](./cases/) | 案例索引 |

---

## 模块级文档（与代码同目录）

| 路径 | 说明 |
|------|------|
| `node/internal/*/README.md` | Go Node 包说明 |
| `node/internal/*/REFERENCE.md` | API / 字段参考 |
| `shared/config/REFERENCE.md` | 配置校验细节 |
| `manage/README.md` | Manage 运维 |
| `packaging/agent-client/README.md` | 安装与 Agent Card |
