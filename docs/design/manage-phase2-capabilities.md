# Manage 后续能力规划（Phase 2+）

> **状态**：规划稿（2026-06-18）  
> **对齐**：[manage-architecture.md](./manage-architecture.md)、[manage-llm-skills-pageagent.md](./manage-llm-skills-pageagent.md)（PR #31 基线）  
> **读者**：Manage / Node 开发者、PR #31 及后续迭代作者

PR #31 交付 Manage 侧 **LLM 配置注册**、**Skills 精简分发**、**Blob API** 与 Console 管理页。本文定义 **Phase 2+** 四条主线，并要求在 PR #31 及后续实现中 **预留接口契约**，避免与「全量强推同步」绑死。

---

## 总原则

| 原则 | 说明 |
|------|------|
| **Manage 不跑 turn** | 存储、目录、调度、中转；推理与工具执行仍在各 Node |
| **Node 主动拉取** | Skills / 插件 / 版本信息由 Node **请求 + 选择**；Manage 做「能力市场」目录，非强制覆盖 |
| **契约先行** | API 路径、信封字段、幂等键在 Phase 2 前定稿；实现可分 PR |
| **与 `discovery_group` 一致** | 可见性、下载权、共享范围复用 Registry 分组语义 |

---

## 1. 能力市场（Capability Marketplace）

### 1.1 定位

Manage 作为企业 **Skill / Plugin（及远期扩展制品）目录**：

- Node **主动**拉取可用列表（catalog + manifest）
- 运维或 Agent 策略 **选择**安装哪些（云端 → 本地）、卸载/禁用哪些（仅云端登记或本地移除）
- Manage **不**默认把全部 published 包推送到每台 Node

与 PR #31 差异：现有 `GET /v1/skills/sync/manifest?since=N` 偏「增量通知」；Phase 2 需补 **显式选择与状态回写**。

### 1.2 制品类型（首版 skill，预留 plugin）

| `artifact_kind` | 说明 |
|-----------------|------|
| `skill` | zip，解压到 `{fs_root}/skills/{skill_id}/` |
| `plugin` | 预留：Node 扩展插件包（格式 TBD） |

### 1.3 预留 API（Manage）

| 方法 | 路径 | 调用方 | 说明 |
|------|------|--------|------|
| GET | `/v1/marketplace/catalog` | Node / Console | 分页目录；筛选 `kind`、`team`、`risk_level`、`discovery_group` |
| GET | `/v1/marketplace/catalog/{kind}/{artifact_id}` | Node / Console | 元数据 + 版本列表 |
| GET | `/v1/marketplace/manifest` | Node | 与 PR #31 `sync/manifest` 合并或别名；`?since=` + `?agent_id=` |
| POST | `/v1/marketplace/installs` | Node | 登记「本 Node 已选择安装」`{agent_id, kind, artifact_id, version}` |
| DELETE | `/v1/marketplace/installs/{install_id}` | Node / Admin | 云端移除登记（是否删本地由 Node 策略决定） |
| GET | `/v1/marketplace/installs?agent_id=` | Node / Console | 某 Node 已选清单 vs catalog 全集 |

**下载**：继续复用 `GET /v1/skills/catalog/{id}/versions/{ver}/download` 与 `GET /v1/blobs/{id}`；plugin 类型上线后增加平行路径。

### 1.4 Node 侧（Phase 2）

```text
Node  GET /v1/marketplace/catalog（或 manifest）
      → 展示/策略选择待安装项
      POST /v1/marketplace/installs + GET download
      → 校验 sha256 → 解压 → 标记 source: manage
      DELETE install / 本地 unload_skills
      → 可选同步云端登记
```

配置预留（`shared/config` `manage.marketplace`）：`enabled`、`auto_sync`（默认 **false**）、`allowed_kinds[]`。

### 1.5 PR #31 作者注意

- `skill_packages` / `sync/manifest` 信封保持扩展性（`kind`、`allowed_groups`、`install_policy` 字段可空）
- 勿假设「publish = 全员自动安装」

---

## 2. 版本发布中枢（Release Hub）

### 2.1 定位

Manage 登记 **DAgents 各组件**（`dagents-node`、`dagents-client`、`dagents-cli`、Manage 自身等）的 **发布版本元数据**；Node 启动或周期任务 **查询是否有新版本**，由运维 **确认是否升级**（非静默强制升级）。

### 2.2 预留 API（Manage）

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/v1/releases/latest` | `?component=dagents-node&platform=linux-amd64` → `{version, published_at, release_notes_url, assets[]}` |
| GET | `/v1/releases` | Admin 列表历史版本 |
| POST | `/v1/releases` | Admin 登记新版本（或 CI webhook 写入） |
| GET | `/v1/releases/check` | Node 批量检查：`?components=node&current=0.5.1` → `[{component, current, latest, upgrade_available}]`（Phase 2；版本以 Node 为准，无独立 client 组件版本） |

**资产**：`assets[]` 指向 GitHub Release URL 或企业内网镜像地址；Manage **可不托管二进制**，只做 **索引与策略**（哪些环境允许自动提示/禁止降级）。

### 2.3 Node 侧（Phase 2）

- 启动或 `dagents version --check` 调 `/v1/releases/check`
- 结果写日志 / TUI 提示；**升级动作**仍走安装包脚本（`install.sh` / Windows installer），不由 Manage 远程执行

### 2.4 存储预留

表 `release_channels` / `release_artifacts`（或 `schema_meta` 扩展）；与 Skills 表独立。

---

## 3. 复杂 Workflow（多 Agent 执行计划）

### 3.1 定位

在现有 **单条 A2A Task**（`agent_invoke` 一问一答）之上，Manage 作为 **多 Agent 枢纽**，支持 **结构化执行计划（Workflow）**：

- 不只一段自然语言 `content`，而是 **有向步骤图 / 阶段列表**
- 指定 **哪些 Agent、什么顺序、每步输入/验收条件、最终目标**
- Manage 负责 **状态机、依赖、重试、汇总**；各步仍由各 Node 执行 turn

### 3.2 与现有 A2A 关系

| 现有 | Workflow |
|------|----------|
| `POST /v1/a2a/tasks` 单 Task | `POST /v1/workflows` 创建计划实例 |
| caller → callee 一步 | 多步：discover → invoke → 等待 → 下一步 |
| `status: completed/failed` | `workflow_run` + `step_runs[]` 细粒度状态 |

### 3.3 预留数据模型（草案）

```yaml
workflow_id: "incident-diagnosis-v1"
steps:
  - id: collect-metrics
    agent_selector: { discovery_group: ops, capability: metrics }
    task_template: { kind: invoke, content: "..." }
    acceptance: { type: json_schema, schema: {...} }  # 或 llm_judge / human_gate
  - id: analyze-logs
    depends_on: [collect-metrics]
    agent_selector: { agent_id: log-agent-1 }
    input_from: { step: collect-metrics, field: result_text }
final_goal:
  description: "输出根因与修复建议"
  acceptance: { type: human_approve }
```

### 3.4 预留 API（Manage）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/workflows/definitions` | Admin 注册计划模板 |
| GET | `/v1/workflows/definitions/{id}` | 模板详情 |
| POST | `/v1/workflows/runs` | 启动一次运行（实例化模板 + 上下文） |
| GET | `/v1/workflows/runs/{run_id}` | 运行状态、各 step 结果 |
| POST | `/v1/workflows/runs/{run_id}/steps/{step_id}/resume` | HITL / 失败后继续 |
| POST | `/v1/workflows/runs/{run_id}/cancel` | 取消 |

Console：Workflow 设计器 / 运行追踪页（远期）；首版可 YAML + API。

### 3.5 实施顺序建议

1. **M2+** 巩固单 Task 可观测  
2. **M6** 线性 Workflow（无分支）  
3. **M7** 分支、并行、人工验收节点  

PR #31 无需实现；A2A `task_id` / audit 字段保持可关联 `workflow_run_id`（可选扩展字段）。

---

## 4. 资源中转站（Shared Artifacts）

### 4.1 定位

Agent 间共享文件（日志片段、报告、配置脱敏导出）时，**上传到 Manage Blob**，通过 **分享引用** 让其他 Agent 拉取——避免 Node 直连、避免把大文件塞进 A2A `content`。

与 PR #31 **Platform Blob API** 直接延续。

### 4.2 预留 API（在 Blob 之上）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/artifacts` | multipart 上传 + 元数据 `{name, owner_agent_id, allowed_groups[], ttl_seconds, content_type}` |
| GET | `/v1/artifacts/{artifact_id}` | 元数据 + 下载 URL（或 302 到 blob） |
| GET | `/v1/artifacts` | 列表；`?shared_with_agent_id=` / `?discovery_group=` |
| POST | `/v1/artifacts/{id}/share` | 追加分享给 `agent_id` 或 `discovery_group` |
| DELETE | `/v1/artifacts/{id}` | 所有者或 admin 删除 |

**A2A 引用**：Task `content` 或 `attachments[]` 可带 `{ type: artifact_ref, artifact_id, sha256 }`，callee Node 向 Manage 下载。

### 4.3 安全

- 上传：Node token + `owner_agent_id` 校验  
- 下载：`allowed_groups` 与 Registry 交集  
- TTL 与配额：`MANAGE_BLOB_MAX_BYTES`、每 agent 配额（Platform §4.4 Quota）

PR #31 已有 `POST/GET /v1/blobs`；Phase 2 增加 **artifact 元数据层**（表 `shared_artifacts`），与裸 blob 区分。

---

## 5. 里程碑映射

| ID | 能力 | 依赖 PR #31 | 说明 |
|----|------|-------------|------|
| **M3b** | 能力市场 API + Node 可选安装 | Skills catalog、Blob | 扩展 manifest / installs |
| **M4b** | Release Hub | — | 新表 + `/v1/releases/*` |
| **M6** | Workflow 线性版 | A2A Task M2 | 新模块 `manage/workflows/` |
| **M3c** | Shared Artifacts | Blob API | `manage/artifacts/` |

---

## 6. PR #31 合并检查（接口预留）

合并 PR #31 时建议确认：

- [ ] `sync/manifest` 信封可扩展（`catalog_version` + `items[]` 字段不写死仅 skill）
- [ ] `skill_packages` payload 可增 `allowed_groups`、`artifact_kind` 而不破坏迁移
- [ ] Blob `blob_id` 与 artifact 分层文档化（避免与 §4 重复造轮）
- [ ] A2A Task payload 文档预留 `attachments[]` / `workflow_run_id` 可选字段
- [ ] 不在 PR #31 实现「publish 即全员安装」

---

**维护**：能力落地后更新 [manage-architecture.md](./manage-architecture.md) 对应章，并在 [CHANGELOG.md](../../CHANGELOG.md) 记版本。
