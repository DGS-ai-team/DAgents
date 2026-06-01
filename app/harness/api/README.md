# `app/harness/api/`

> **【已弃用】** Python FastAPI Agent API。DAgentsUI / A2A 联调仍可用；新功能请实现于 **`node/internal/api/`**。

FastAPI 接入层：对外 HTTP/SSE（浏览器或其它 HTTP 客户端）。

**对外 HTTP/SSE 契约**（路径、字段、错误码、SSE 形状）以仓库 **`docs/api-reference.md`** 为准，与 **`app.py`** 同步维护。

- 所有响应含 HTTP 头 **`Deprecation: true`**、**`X-DAgents-Backend: python-deprecated`**
- **`GET /health`** 返回 `deprecated`、`replacement`、`deprecation_notice` 字段

| 文件 | 说明 |
|------|------|
| **`app.py`** | FastAPI 应用与路由：健康检查、**`GET /metrics`**、session/message/SSE、触发器 CRUD 与手动 fire |

## 契约导出

- 推荐以后端 OpenAPI 作为前后端契约单一来源。
- 在仓库根目录执行：
  - `python export_openapi_schema.py`
- 建议导出到前端仓库：
  - `python export_openapi_schema.py --output /path/to/frontend-repo/openapi.json`
- 在前端仓库目录生成前端类型：
  - `pnpm gen:types`
- 生成文件：
  - `src/api/types.ts`

