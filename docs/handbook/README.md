# DAgents 项目手册

**版本**：与代码同步（当前发布 **v0.9.5**）  
**定位**：本仓库**唯一**技术文档入口——架构、接口、配置、联调与源码导航。根目录 [README](../../README.md) 面向「能做什么」的产品介绍；具体怎么配、怎么测、契约是什么，以本手册为准。

> **架构要点**：人机入口为 Node 内嵌 **Web UI**（`/ui/`）；跨机协作走 **Workgroup**；工具边界靠工具组、审批策略与工作区路径（无独立沙箱进程）。产品概览见根 [README](../../README.md)，工作组见 [07-Workgroup协作](./07-Workgroup协作.md)。

---

## 如何阅读

| 你是谁 | 推荐路径 | 预计时间 |
|--------|----------|----------|
| **先看产品再动手** | 根 [README](../../README.md) → [00-导读](./00-导读.md) → [07-Workgroup协作](./07-Workgroup协作.md)（若要用协作） | 30–60 分钟 |
| **新人 / 联调** | [00-导读](./00-导读.md) → [01-愿景与架构](./01-愿景与架构.md) → [附录/配置项参考](./附录/配置项参考.md) | 1–2 小时 |
| **用工作组** | [07-Workgroup协作](./07-Workgroup协作.md) → [workgroup 产品规范](../design/workgroup-and-node-gateway.md) | 1–2 小时 |
| **改 Node 内部** | [02-Agent-Node-核心](./02-Agent-Node-核心.md) → `node/internal/*/REFERENCE.md` | 半天 |
| **做 Manage / 契约** | [05-Manage与A2A](./05-Manage与A2A.md) → [workgroup-d05-contracts](../design/workgroup-d05-contracts.md) | 2–4 小时 |
| **发布 / 运维** | [06-运维与案例](./06-运维与案例.md) → [v0.9.1 清单](../design/v0.9.1-smoke-checklist.md) | 1 小时 |
| **查工具 / 配置 / 事件** | [附录](./附录/) | 按需 |

---

## 目录（核心 → 外围）

### Part 0 · 导读

| 章 | 文件 | 内容 |
|----|------|------|
| 0 | [00-导读.md](./00-导读.md) | 手册约定、术语速览、读者路径 |

### Part I · 愿景与架构

| 章 | 文件 | 内容 |
|----|------|------|
| 1 | [01-愿景与架构.md](./01-愿景与架构.md) | 产品定位、仓库拓扑、架构决策 |

### Part II · Agent Node 核心

| 章 | 文件 | 内容 |
|----|------|------|
| 2 | [02-Agent-Node-核心.md](./02-Agent-Node-核心.md) | LLM loop、队列、Agent 隔离 |

### Part III · API 与 Client

| 章 | 文件 | 内容 |
|----|------|------|
| 3 | [03-API与Client.md](./03-API与Client.md) | HTTP/SSE、HITL、Web UI、配置 |

### Part IV · 能力与策略

| 章 | 文件 | 内容 |
|----|------|------|
| 4 | [04-能力与策略.md](./04-能力与策略.md) | 工具、policy、skills、triggers、压缩 |

### Part V · Manage 与协作

| 章 | 文件 | 内容 |
|----|------|------|
| 5 | [05-Manage与A2A.md](./05-Manage与A2A.md) | Registry、控制面 |
| 7 | [07-Workgroup协作.md](./07-Workgroup协作.md) | **工作组用户向说明（预览）** |

### Part VI · 运维与案例

| 章 | 文件 | 内容 |
|----|------|------|
| 6 | [06-运维与案例.md](./06-运维与案例.md) | 开发栈、打包、案例、安全 |

### 附录

| 文件 | 内容 |
|------|------|
| [附录/术语表.md](./附录/术语表.md) | 术语 |
| [附录/内置工具参考.md](./附录/内置工具参考.md) | 内置工具 |
| [附录/SSE事件速查.md](./附录/SSE事件速查.md) | SSE |
| [附录/配置项参考.md](./附录/配置项参考.md) | 配置 |
| [附录/Prometheus观测.md](./附录/Prometheus观测.md) | Manage `/metrics` |
| [附录/路线图与远期方案.md](./附录/路线图与远期方案.md) | 远期索引 |

---

## 相关入口

| 文档 | 说明 |
|------|------|
| [../README.md](../README.md)（docs 地图） | handbook / design / architecture 索引 |
| [../design/v0.9.1-smoke-checklist.md](../design/v0.9.1-smoke-checklist.md) | v0.9.1 预览验收 |
| [AGENTS.md](../../AGENTS.md) | Cloud / 代理环境启动注意 |
| [CHANGELOG.md](../../CHANGELOG.md) | 版本记录 |
