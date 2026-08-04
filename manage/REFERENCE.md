# manage / REFERENCE

## 入口

| 路径 | 说明 |
|------|------|
| `run_manage.py` | 仓库根启动脚本 |
| `manage/manage_app.py` | `create_app()` / `app` |
| `manage/config.py` | `ManageSettings.from_env()` |

## platform/

| 模块 | 符号 |
|------|------|
| `auth.py` | `AuthContext`, `authenticate`, `require_admin`, `TOKEN_HEADER` |
| `audit.py` | `AuditEvent`, `AuditLog` |
| `blob.py` | `BlobStore`, `BlobStoreConfig` |
| `metrics.py` | `record_registry_operation`, `metrics_text` |

## storage/

| 模块 | 符号 |
|------|------|
| `sqlite.py` | `SQLiteDatabase` |

## registry/

| 模块 | 符号 |
|------|------|
| `models.py` | `AgentRegisterRequest`（无 discovery_group）, `AgentGroupsUpdateRequest`, … |
| `status.py` | `derive_status`, `offline_grace_seconds`, `is_deliverable` |
| `store.py` | `AgentRegistryStore.register`（保留已有分组）, `update_groups`, `import_rc_json` |
| `routes.py` | `build_registry_router` |

## skills/ · plugins/ · externaltools/

| 模块 | 符号 |
|------|------|
| `models.py` | 制品包模型 |
| `store.py` | `*PackageStore`（draft/publish/catalog/sync） |
| `routes.py` | `build_*_router` |

## llm/ · releases/ · cases/

| 路径 | 说明 |
|------|------|
| `llm/` | LLM 配置 CRUD + `/resolve` |
| `releases/` | Release Hub（上传/发布/check/download） |
| `cases/` | 案例库 JSONL + 关联资源 |

## console/

| 路径 | 说明 |
|------|------|
| `frontend/` | Vue 3 + Vite 源码（`npm run dev` / `npm run build`） |
| `static/` | 构建产物（不入库）：`index.html` + `assets/*`，挂载 `/console/` |
| `build.sh` | 一键构建到 `static/` |

Console 功能：Node 列表、discovery 分组、工作组、案例库、Node 配置（LLM/Skills/Plugins/ExternalTools/版本发布）。

| 路径 | 说明 |
|------|------|
| `platform/blob_routes.py` | Blob API |
