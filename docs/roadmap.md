# Roadmap（路线图）

从 **产品/能力** 视角看 DAgents 已落地什么、接下来优先做什么。版本事实以 **[CHANGELOG.md](../CHANGELOG.md)** 为准；技术细节见 **[handbook](./handbook/README.md)**。**0.x** 仍允许契约调整；本文随里程碑更新。

---

## 1. 产品定位

> 本地优先的企业 Agent 控制台：内网可部署、高风险可审批、能力可沉淀，而不是通用工作流画布。

四个关键词：

- **本地**：内网与私有模型、本机工具与文件边界。
- **治理**：策略、审批、审计。
- **复用**：节点登记、工作组协作、能力发现。
- **沉淀**：排障经验 → 可版本化的 skills / 脚本。

根 README 讲「能做什么」；本文件讲阶段优先级。

---

## 2. 整体阶段

| 阶段 | 状态 | 说明 |
|------|------|------|
| **0.9.5** | **已发布** | 工作组成员「配置中」卡住修复（websockets + Dialer 重连）；发版 CI 并行与 Rocky8 缓存（tag `v0.9.5`）。 |
| **0.9.4** | **已发布** | Linux `dagents init` 命令行首配；首配门闸下就绪探测改认 `/health`（tag `v0.9.4`）。 |
| **0.9.3** | **已发布** | WebUI SSE/切 Agent 稳定性、Linux 安装对齐、模板 soul/custom、移除 Tauri Setup（tag `v0.9.3`）。 |
| **0.9.2** | **已发布** | Manage Docker 补丁：镜像内打包 `member_tool_catalog.json`（tag `v0.9.2`）。 |
| **0.9.1** | **已发布（预览）** | Workgroup 可演示；验收见 [v0.9.1-smoke-checklist.md](./design/v0.9.1-smoke-checklist.md)。正式版前最后一个大预览（tag `v0.9.1`）。 |
| **0.9.x** | **进行中** | 开箱体验、本地助手、Manage + 工作组收口。 |
| **1.0** | **目标** | 企业本地闭环相对稳定：安装/Web UI、治理与审计、工作组、触发器与 Skill 控制面的核心契约冻结。 |

更早的 0.2.x–0.8.x 交付记录见 **CHANGELOG**，此处不重复。

---

## 3. 已实现能力（概要）

### 3.1 本机助手

- **Agent Node**（`node/`）+ 内嵌 **Web UI**（`/ui/`）：多 Agent、对话、HITL、设置、工作组入口。
- HTTP/SSE：`/v1/agents/{agent_id}/...`、消息与 resume、流式事件（[agent-node-api.md](./architecture/agent-node-api.md)）。
- 工具组 + 审批策略（`.runtime/policy/`）+ 工作区 `fs_root`；无独立沙箱进程。
- Skills、触发器（interval / fire_at / schedule）、临时子 Agent、浏览器伴生任务。
- 上下文压缩与 Prompt Cache 对齐（见 handbook 附录 / compression 包）。

### 3.2 Manage 与工作组

- **Manage**：Registry、Console、Skills/LLM/Releases/Cases、**Workgroup** Leader。
- Node `manage.enabled` 时登记（**`node_id`**）；工作组经 Dialer / 反代，不 Node 互直连。
- 成员工作区工具：共享 catalog（默认 fs；bash 需显式勾选）。说明：[07-Workgroup协作](./handbook/07-Workgroup协作.md)。

### 3.3 分发与运维

- Windows / Linux 安装包；可选桌面托盘（Tauri / Go 双轨）。
- 可选 Release Hub 自更新；Manage 可 Docker 部署。
- 案例目录：[`cases/`](../cases/)。

---

## 4. 后续优先级

按「能否支撑企业本地可治理闭环」排序。

### Phase A · 开箱与本地闭环（对齐 0.9.1 → 1.0）

目标：内网机器上尽快完成「启动 → 对话 → 审批 → 看结果 → 有审计」。

| 项 | 现状 | 下一步 |
|----|------|--------|
| 一键启动 Node + Web UI | 已有安装包 / `go run` | 首次诊断（端口、模型、policy、runtime） |
| 首配与 HITL | 已有首配页与审批流 | 端到端 demo 导览打包 |
| 企业默认 compose | 部分（cases） | 可选 Manage 的一键 compose |
| 文档入口 | README + handbook | 继续去掉过时对照（进行中） |

### Phase B · Registry 与控制台企业化

目标：节点目录可治理，而不是「能 ping 通就行」。

- 身份与能力模型：`node_id`、owner/team、capabilities、risk、心跳与版本。
- Console：在线状态、能力标签、分页筛选、管理员全局视图（鉴权 + 审计）。
- 工作组 ACL / 协作过程可观测（Timeline、RunHistory 已有预览能力，继续打磨）。

基线见 [manage-architecture.md](./design/manage-architecture.md)；能力市场等见 [manage-phase2-capabilities.md](./design/manage-phase2-capabilities.md)。

### Phase C · 治理、审批与审计

- Policy 只读/受控编辑 UI（现网以本地 policy 文件 + API 为主）。
- 审批与审计时间线：谁请求、何工具、何结果、可导出。
- 触发器 / skill 发布纳入同一治理面。

### Phase D · 触发器控制面（企业化）

现网已有 Node 侧 interval / fire_at / schedule + 工具与调度器。仍缺：

- Webhook / 指标阈值 / Registry 事件等来源。
- 幂等去抖、死信、并发与审批策略产品化。
- 触发器管理与审计 UI。

边界不变：触发器不能绕过工具审批与 policy。

### Phase E · Skill Library 生命周期

现网 skills 偏「加载进会话」。目标：

- 元数据、版本、审批、发布/禁用、使用统计。
- 从会话沉淀 candidate skill。
- 与触发器、能力市场联动（Manage catalog，Node 主动选择安装）。

### Phase F · 运维向场景与加固

- 多节点诊断、带审批的修复、incident timeline、runbook skill（强 demo，非通用画布）。
- 企业身份（角色 → 后续 LDAP/OIDC）。
- 模型路由与成本记账。
- Manage HA / 共享存储。
- 关键 API 自动化覆盖（审批、审计、触发器、工作组、Directory）。

**不做主线**：通用可视化工作流画布、类 Dify 应用商店、过早的大规模多模型抽象。

---

## 5. 已知缺口（短表）

| 项 | 说明 |
|----|------|
| 首次启动一键诊断 | 端口 / 模型 / Manage / policy / runtime |
| 触发器企业来源与 UI | Webhook、指标、死信、审计页 |
| Skill 审批与版本库 | 超越目录加载 |
| Node Prometheus | Manage 已有 `/metrics`；Node 端点待做 |
| Manage HA | 现网偏单实例 SQLite |
| 真机老环境 E2E | cases 有导览；归档清单见 `docs/archive/` / architecture |

---

## 6. 如何参与

- Issues 提缺陷与需求（注明版本与环境）。
- 安全见 [SECURITY.md](../SECURITY.md)。
- **以实现 / CHANGELOG 为准**；本文冲突时欢迎 PR 修正。

**最后更新**：2026-08-18 — 准备发布 v0.9.12（终端执行、MCP 接入与工作组协作增强）；v0.9.1 为 Workgroup 预览主叙事。
