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

## console/static/

| 文件 | 说明 |
|------|------|
| `index.html` / `styles.css` / `app.js` | Manage Console：Node 列表、筛选、详情抽屉 |

| 路径 | 里程碑 |
|------|--------|
| `a2a/` | M2 |
| `skills/` | M3 |
