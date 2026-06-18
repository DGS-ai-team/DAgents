# 技术文档

> **文档已收敛** — 完整项目手册见 **[handbook/README.md](./handbook/README.md)**（唯一正文入口）。
>
> 本目录下 `architecture/`、`design/`、`future/` 等子目录**仅保留跳转桩**；旧路径对照见 [handbook/附录/旧文档迁移对照表.md](./handbook/附录/旧文档迁移对照表.md)。

---

## 快速入口

| 需求 | 手册章节 |
|------|----------|
| 新人 / 联调 | [handbook/00-导读.md](./handbook/00-导读.md) → [01-愿景与架构.md](./handbook/01-愿景与架构.md) |
| 改 Node 内部 | [handbook/02-Agent-Node-核心.md](./handbook/02-Agent-Node-核心.md) |
| HTTP/SSE / Client | [handbook/03-API与Client.md](./handbook/03-API与Client.md) |
| 工具 / 压缩 / policy | [handbook/04-能力与策略.md](./handbook/04-能力与策略.md) |
| Manage / A2A | [handbook/05-Manage与A2A.md](./handbook/05-Manage与A2A.md) |
| 打包 / 案例 | [handbook/06-运维与案例.md](./handbook/06-运维与案例.md) |
| 内置工具全表 | [handbook/附录/内置工具参考.md](./handbook/附录/内置工具参考.md) |
| 术语 | [handbook/附录/术语表.md](./handbook/附录/术语表.md) |

---

## 模块级文档（与代码同目录，继续维护）

| 路径 | 说明 |
|------|------|
| `node/internal/*/README.md` | Go Node 包说明 |
| `node/internal/*/REFERENCE.md` | API / 字段参考 |
| `shared/config/REFERENCE.md` | 配置校验细节 |
| `manage/README.md` | Manage 运维 |
| `packaging/agent-client/README.md` | 安装与 Agent Card |

---

## 归档

已移除的 Python Agent API 等：`archive/`（历史只读）。
