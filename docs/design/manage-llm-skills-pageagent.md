# Manage：LLM 配置注册中心 + Skills 分发（精简）+ PageAgent Console 集成

> 状态：设计稿（2026-06-17）  
> 对齐：[manage-architecture.md](./manage-architecture.md) §4.3（Skills）、§4.2.2（Platform Blob）；[three-component-model.md](./three-component-model.md)  
> 范围：**首个 PR 仅 Manage 侧（Python/FastAPI）+ Console 前端，不改 Go Node。**

## 1. 背景与动机

当前 LLM 配置（provider/base_url/model/key）只存在于**每个 Node 的本地 `config.yaml`**，多 Node 部署时需逐台维护、无法集中复用。Skills 同理：`.runtime/skills/` 为各 Node 本地手工维护，`manage/skills/` 仅有空 `__init__.py`，`manage-architecture.md` §4.3 已规划但未落地。

本设计在 Manage 增加两个**可被多 Node / 外部复用**的注册中心，并把 [alibaba/page-agent](https://github.com/alibaba/page-agent)（浏览器端 `{model, baseURL, apiKey}` 驱动的网页操作 Agent）嵌入 Manage Console，使其 LLM 直接**复用 LLM 配置注册中心**里的条目——从而让 Console 本身可被自然语言操作。

## 2. 范围与边界

**本 PR 交付（Manage 侧，纯 Python + Console 静态前端）：**
- A. LLM 配置注册中心：CRUD API + Console 管理页 + `resolve` 端点
- B. Skills 注册/分发（精简版）：Platform Blob API + skill 包上传/发布/目录/下载 + Console 管理页
- C. PageAgent 嵌入 Console：命令栏 → 取 LLM 配置 → `new PageAgent(cfg)` → 操作 Console

**显式不在本 PR（留 Phase 2，独立跟进）：**
- Go Node 自动消费 LLM 配置（`node/internal/manage` 拉取 + `turn` 的 `llm.settings` 热更）。本 PR 仅提供注册中心 + API，Node 暂按 id 取用、不自动同步。
- Go Node 的 Skills 自动同步（心跳 `skills_catalog_version` → 拉取 → 解压到 `fs_root/skills/`）。本 PR 提供 Manage 侧 `sync/manifest` + download **API 契约**，Go 端实现留 Phase 2。
- Skills 多级审批工作流（draft→pending_review→approved→published）。精简版仅 `draft`/`published` 两态、单步发布。

> 更新(实现期)：Console UI 与 PageAgent 集成已移出本 PR，延后到后续 PR；本 PR 仅交付 Manage 后端 API。

## 3. 设计原则

- **不改既有契约**：复用 `SQLiteDatabase`、`platform/auth`（`authenticate`/`require_admin`）、`BlobStore`、`build_X_router` 装配模式与 `discovery_group` 语义。
- **Manage 不参与 LLM turn loop**（对齐 manage-architecture.md §0）：Manage 只**存储/分发配置**，不代跑 turn。PageAgent 在浏览器侧跑，Manage 仅做配置出口（C 选择 key 直返浏览器，见 §7 安全）。
- **每模块自洽**：`manage/llm/` 与 `manage/skills/` 各含 `models.py` / `store.py` / `routes.py`，边界清晰、可独立测试，与现有 `manage/registry/`、`manage/a2a/` 结构一致。

## 4. 模块 A：LLM 配置注册中心（`manage/llm/`）

### 4.1 数据模型（新表 `llm_configs`）

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | str (pk) | 稳定 ID，缺省由 `name` 归一化或 uuid 生成 |
| `name` | str | 展示名（唯一） |
| `provider` | str | `openai` / `deepseek` / `qwen` / `vllm`（与 Node 配置取值一致） |
| `base_url` | str | OpenAI 兼容根地址（含 `/v1`） |
| `model` | str | 模型名 |
| `api_key` | str | **明文存储**（契合 §7 的本地/局域网信任 + key 直返浏览器） |
| `reasoning_effort` | str? | 可选：`high`/`max` |
| `thinking` | str? | 可选：`enabled`/`disabled` |
| `is_default` | bool | 是否默认配置（全局至多一个为 true，置位时清除其它） |
| `allowed_groups` | list[str] | 可选：限定可见/可用的 `discovery_group`；空=全部可用 |
| `created_at` / `updated_at` | int (unix) | |

存储扩展 `manage/storage/sqlite.py` 的 `_init_schema`：`CREATE TABLE IF NOT EXISTS llm_configs (...)`，并登记 `schema_meta` 版本号。

### 4.2 store（`manage/llm/store.py` → `LLMConfigStore`）

构造接收 `SQLiteDatabase | None`（`db.enabled` 为 false 时退化为内存 dict，与 `AgentRegistryStore` 行为一致）。方法：`create / list / get / update / delete / get_default / set_default`。`set_default` 原子清除旧默认。

### 4.3 API（`manage/llm/routes.py` → `build_llm_router`）

| 方法 | 路径 | 鉴权 | 说明 |
|------|------|------|------|
| POST | `/v1/llm/configs` | `require_admin` | 创建 |
| GET | `/v1/llm/configs` | authenticate | 列表（`api_key` 字段**掩码**为 `sk-***` 仅显示尾 4 位） |
| GET | `/v1/llm/configs/{id}` | authenticate | 详情（掩码） |
| PUT | `/v1/llm/configs/{id}` | `require_admin` | 全量更新 |
| DELETE | `/v1/llm/configs/{id}` | `require_admin` | 删除 |
| GET | `/v1/llm/configs/{id}/resolve` | authenticate | **返回明文** `{model, baseURL, apiKey}`（PageAgent 兼容形）；C 与 Node(Phase 2) 的复用落点 |
| GET | `/v1/llm/configs/default/resolve` | authenticate | 默认配置的 resolve |

> `resolve` 把内部字段映射为 PageAgent 的驼峰形：`base_url→baseURL`、`api_key→apiKey`、`model→model`。列表/详情掩码，`resolve` 才出明文——避免目录页泄露 key。

装配：`manage_app.py` 的 `create_app()` 内 `app.include_router(build_llm_router(llm_store, audit))`，并 `db` 共用。

## 5. 模块 B：Skills 注册/分发（精简版）

### 5.1 Platform Blob API（`manage/platform/blob.py` 现有 `BlobStore` + 新路由）

`BlobStore` 已存在但无路由。新增最小 Blob 路由（A2A 与 Skills 共用，对齐 §4.2.2）：

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/v1/blobs` | multipart 上传 → `{blob_id, sha256, size}` |
| GET | `/v1/blobs/{id}` | 下载（Node token） |
| HEAD | `/v1/blobs/{id}` | 元数据 |
| DELETE | `/v1/blobs/{id}` | admin |

落盘目录由 `MANAGE_BLOB_DIR` 配置（`BlobStoreConfig.from_settings`）；`MANAGE_BLOB_MAX_BYTES` 限单文件。

### 5.2 数据模型（新表 `skill_packages`）

字段对齐 manage-architecture.md §4.3.1：`skill_id, version(semver), name, description, owner, team, risk_level(low/medium/high), required_tools[], required_scopes[], blob_id, status(draft|published), created_at, updated_at`。主键 `(skill_id, version)`。

### 5.3 store（`manage/skills/store.py` → `SkillPackageStore`）

`create(draft) / publish / list_catalog(published) / get(skill_id) / get_version / sync_manifest(since)`。`catalog_version` 为单调自增整型（每次 publish +1），供 §5.5 同步契约。

### 5.4 API（`manage/skills/routes.py` → `build_skills_router`）

| 方法 | 路径 | 鉴权 | 说明 |
|------|------|------|------|
| POST | `/v1/skills/packages` | `require_admin` | 直收 multipart：zip + 元数据字段；内部存入 BlobStore 得 `blob_id`，落库 status=draft |
| POST | `/v1/skills/packages/{skill_id}/versions/{ver}/publish` | `require_admin` | 单步 draft→published |
| GET | `/v1/skills/catalog` | authenticate | 已发布列表 |
| GET | `/v1/skills/catalog/{skill_id}` | authenticate | 元数据 + 版本列表 |
| GET | `/v1/skills/catalog/{skill_id}/versions/{ver}/download` | authenticate | 下载 zip（302 到 blob 或直传） |
| GET | `/v1/skills/sync/manifest?since=N` | authenticate | 增量清单 `[{skill_id, version, sha256, download_url}]`（Node 同步契约，Go 端 Phase 2 消费） |

### 5.5 Node 同步契约（仅定义，Go 实现 Phase 2）

```
heartbeat 响应（Phase 2 增字段）→ { skills_catalog_version: N }
Node  GET /v1/skills/sync/manifest?since=N-1 → 增量项
      下载 → 校验 sha256 → 解压到 {fs_root}/skills/{skill_id}/（带 source: manage 标记）
```
本 PR 不动 heartbeat 响应体与 Go Node；契约写入文档供 Phase 2 实现。

## 6. 模块 C：PageAgent 嵌入 Console（`manage/console/static/`）

Console 为纯静态页（FastAPI `StaticFiles` 挂 `/console`）。新增三部分：

1. **LLM 配置管理页**：表格 + 新建/编辑/删除表单，调 §4.3 API；列表显示掩码 key。
2. **Skills 页**：上传 zip、列表、单步发布，调 §5.4 API。
3. **自然语言命令栏**（核心）：
   - 顶部下拉选择一个 LLM 配置（默认取 `is_default`）。
   - 输入任务 → 前端 `GET /v1/llm/configs/{id}/resolve` 取 `{model, baseURL, apiKey}` → `new PageAgent({...cfg, language:'zh-CN'})` → `await agent.execute(task)`。
   - PageAgent 直接操作当前 Console DOM（如"给 node-a 分配 a2a-lab 组""发布名为 X 的 skill"）。
   - **PageAgent 引入**：vendor `page-agent@1.7.1` 的 IIFE 产物到 `manage/console/static/vendor/page-agent.iife.js`（来源 `registry.npmmirror.com/page-agent/1.7.1/files/dist/iife/page-agent.js`，China 可达），`<script>` 引入；不依赖运行期外网 CDN。版本号（1.7.1）记录在 Console README，升级时同步替换。

## 7. 安全

- `api_key` 明文存于 SQLite，`resolve` 明文返回浏览器——**仅适用于本地/局域网信任部署**（与 DAgents 本地运行时定位一致；用户已确认）。
- 列表/详情端点对 `api_key` 掩码，降低 Console 目录页/截图泄露面。
- 所有**写**端点（configs/skills 的 POST/PUT/DELETE/publish、blobs 写/删）走 `require_admin`；读端点 `authenticate`。
- 文档显著标注：生产/公网部署须改为"Manage 反代 LLM 调用、key 不出服务端"（Phase 2 可选增强），不要直接暴露公网。

## 8. 存储变更汇总（`manage/storage/sqlite.py`）

`_init_schema` 新增三表：`llm_configs`、`skill_packages`、`blobs`（blob 元数据：`blob_id, sha256, size, content_type, created_at`；字节落盘 `MANAGE_BLOB_DIR`）。`schema_meta` 升版本。无破坏性迁移（仅新增表）。

## 9. 测试（stdlib `unittest`，`tests/`）

- `tests/test_manage_llm.py`：configs CRUD、`is_default` 唯一性、列表掩码 vs `resolve` 明文、`allowed_groups` 过滤、鉴权（admin vs 普通）。
- `tests/test_manage_skills.py`：blob 上传/下载/sha256 校验、skill draft→publish、catalog 只列 published、`sync/manifest?since=` 增量、鉴权。
- 沿用现有 `test_manage_m0_m1.py` / `test_manage_a2a_store.py` 的 fixture 风格（`SQLiteDatabase` 用临时文件或内存）。
- 验证命令：`python -m unittest tests.test_manage_llm tests.test_manage_skills -v`，并确保 `python -m unittest discover -s tests -p "test_*.py"` 全绿。

## 10. 文档与变更记录

- 更新 `docs/design/manage-architecture.md`：标注 Skills 精简版落地、新增"LLM 配置注册中心"节、Blob API 落地。
- `CHANGELOG.md [Unreleased]` 增 `feat` 条目。
- Console README 记录 PageAgent vendor 版本与来源。

## 11. 验收标准

1. `python run_manage.py` 启动后：Console 出现 LLM 配置页、Skills 页、命令栏。
2. 经 API 建一个 cliproxy LLM 配置（如 `claude-sonnet-4-6`）→ 在命令栏选中 → 输入"列出所有在线 Node"→ PageAgent 用该配置驱动、操作 Console 完成。
3. 上传一个 `SKILL.md` zip → 发布 → `GET /v1/skills/catalog` 可见 → download 取回字节 sha256 一致。
4. `GET /v1/llm/configs` 列表 key 掩码；`/resolve` 返回明文 `{model,baseURL,apiKey}`。
5. 新增/改动测试全绿;`go test`/Go 构建不受影响（未触碰 Go 代码）。
