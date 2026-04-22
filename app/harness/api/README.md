# `app/harness/api/`

FastAPI 接入层：统一对外入口（UI/CLI 等客户端通过 HTTP 调用）。

| 文件 | 说明 |
|------|------|
| **`app.py`** | FastAPI 应用与路由：健康检查、**`GET /metrics`**、**`POST /v1/sessions/{session_id}/cancel`**（取消当前 turn）、提交消息、SSE；**`MessageIn.priority`** 缺省 **`message`→`human`**、**`resume`→`resume`**（**`human` 仅队列优先级，不自动 cancel**） |

## 契约导出

- 推荐以后端 OpenAPI 作为前后端契约单一来源。
- 在仓库根目录执行：
  - `python export_openapi_schema.py`
- 默认导出到：
  - `frontend/openapi.json`
- 在 `frontend/` 目录生成前端类型：
  - `pnpm gen:types`
- 生成文件：
  - `frontend/src/api/types.ts`

