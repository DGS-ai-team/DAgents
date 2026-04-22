# `frontend/src/api/` 说明

本目录存放前端 API 契约相关文件。

## 当前文件

- `types.ts`：由 OpenAPI 自动生成的 TypeScript 类型定义（请勿手改）。
- `client.ts`：基于生成类型的前端 API 调用封装（`createSession` / `submitMessage` / `cancelCurrentTurn` / `streamUrl`）。

## 更新流程

1. 在仓库根目录导出最新 OpenAPI：

```bash
python export_openapi_schema.py
```

2. 在 `frontend/` 目录生成类型：

```bash
pnpm gen:types
```
