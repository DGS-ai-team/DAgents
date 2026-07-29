# DAgents 项目手册

**版本**：与代码同步（当前发布 **v0.8.5**；架构见 [agent-instance-model.md](../design/agent-instance-model.md)）  
**定位**：本仓库**唯一**技术文档入口——由核心到外围，写到**能跟读源码**的深度。专题长文见 `docs/architecture/`、`docs/design/`；本手册为导航与正文主干。

> **注意**：v0.8 起为「单 Node 多 Agent + 仅 Web UI」。手册中若仍出现 Session 中心、TUI Client、进程级 `agent_id` 等旧叙述，以 [agent-instance-model.md](../design/agent-instance-model.md) 与现网 API 为准。  
> **进行中**：同组远端 Node 上放置 Agent（Placement）与屏幕旁观 —— 见 [remote-agent-placement.md](../design/remote-agent-placement.md)（独立分支大改通信面）。

---

## 如何阅读

| 你是谁 | 推荐路径 | 预计时间 |
|--------|----------|----------|
| **新人 / 联调** | [00-导读](./00-导读.md) → [01-愿景与架构](./01-愿景与架构.md) → [03-API与Client](./03-API与Client.md) §3.6 快速上手 | 1–2 小时 |
| **改 Node 内部** | [02-Agent-Node-核心](./02-Agent-Node-核心.md) §1→§4 → `node/internal/*/REFERENCE.md` | 半天 |
| **做 A2A / Manage** | [01](./01-愿景与架构.md) §1.2 → [05-Manage与A2A](./05-Manage与A2A.md) → [cases/a2a-manage-docker](../../cases/a2a-manage-docker/README.md) | 2–4 小时 |
| **发布 / 运维** | [03](./03-API与Client.md) §3.5 → [06-运维与案例](./06-运维与案例.md) | 1 小时 |
| **查工具 / 配置 / 事件** | [附录](./附录/) | 按需 |

---

## 目录（核心 → 外围）

### Part 0 · 导读

| 章 | 文件 | 内容 |
|----|------|------|
| 0 | [00-导读.md](./00-导读.md) | 手册约定、术语速览、四条读者路径、与源码目录对照 |

### Part I · 愿景与架构

| 章 | 文件 | 内容 |
|----|------|------|
| 1 | [01-愿景与架构.md](./01-愿景与架构.md) | 项目定位、三组件模型、仓库拓扑、架构决策、当前能力 |

### Part II · Agent Node 核心

| 章 | 文件 | 内容 |
|----|------|------|
| 2 | [02-Agent-Node-核心.md](./02-Agent-Node-核心.md) | 单次 LLM → LLM loop → 队列与消息来源 → session 隔离 |

### Part III · API 与 Client

| 章 | 文件 | 内容 |
|----|------|------|
| 3 | [03-API与Client.md](./03-API与Client.md) | HTTP/SSE 契约、HITL resume、多 Client（终端 + `/ui/`）、转录展示、配置与同包发布 |

### Part IV · 能力与策略

| 章 | 文件 | 内容 |
|----|------|------|
| 4 | [04-能力与策略.md](./04-能力与策略.md) | 工具 registry、policy、skills、triggers、子 Agent、压缩、LLM 适配 |

### Part V · Manage 与 A2A

| 章 | 文件 | 内容 |
|----|------|------|
| 5 | [05-Manage与A2A.md](./05-Manage与A2A.md) | 注册、Agent Card、A2A Task/inbox、ComplianceExecutor、Caller HITL 中继 |

### Part VI · 运维与案例

| 章 | 文件 | 内容 |
|----|------|------|
| 6 | [06-运维与案例.md](./06-运维与案例.md) | 开发栈、打包安装、OS 兼容、案例索引、安全与观测 |

### 附录

| 文件 | 内容 |
|------|------|
| [附录/术语表.md](./附录/术语表.md) | 全书术语定义 |
| [附录/内置工具参考.md](./附录/内置工具参考.md) | 内置工具全量参考 |
| [附录/SSE事件速查.md](./附录/SSE事件速查.md) | SSE 事件类型与字段 |
| [附录/配置项参考.md](./附录/配置项参考.md) | YAML / 环境变量 |
| [附录/重大设计变更实录.md](./附录/重大设计变更实录.md) | 架构级优化背景与落地 |
| [附录/路线图与远期方案.md](./附录/路线图与远期方案.md) | 未完全落地设计索引 |
| [GitHub Issues](https://github.com/DGS-ai-team/DAgents/issues) | 已知缺陷 / 排查项（以 GitHub 为准，仓库内不重复维护 Issue 正文） |

---

## 章节结构约定

每一章（除附录）统一四段：

1. **本章回答什么问题** — 读完后应能做什么  
2. **核心概念** — 架构图 / 表格 / 时序  
3. **源码与配置索引** — 概念 → 目录 / 文件 / 配置键  
4. **延伸阅读** — 模块 `REFERENCE.md`、案例、测试

---

## 维护约定

| 变更类型 | 更新位置 |
|----------|----------|
| 架构边界 / ADR | [01-愿景与架构](./01-愿景与架构.md) §1.4 |
| Node 内部协作 | [02-Agent-Node-核心](./02-Agent-Node-核心.md) + `node/internal/*/README.md` |
| HTTP/SSE 契约 | [03-API与Client](./03-API与Client.md) + `node/internal/api/` |
| 新内置工具 | [附录/内置工具参考](./附录/内置工具参考.md) + `node/internal/tools/` |
| Manage / A2A | [05-Manage与A2A](./05-Manage与A2A.md) |
| 版本发布 | 根 [CHANGELOG.md](../../CHANGELOG.md)；手册首页版本号 |

**不要**在 handbook 外重复维护同主题正文；模块级 API 细节仍与代码同目录维护 `REFERENCE.md`。

---

## 相关入口

| 路径 | 说明 |
|------|------|
| [../../README.md](../../README.md) | 项目概览、快速开始、徽章 |
| [../../node/README.md](../../node/README.md) | Go Agent Node 模块索引 |
| [../../client/README.md](../../client/README.md) | Go Client |
| [../../app/cli/README.md](../../app/cli/README.md) | Python Textual TUI |
| [../../manage/README.md](../../manage/README.md) | Manage 控制面 |
| [../../cases/README.md](../../cases/README.md) | 集成案例 |
