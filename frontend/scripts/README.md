# `frontend/scripts/` 说明

本目录存放前端工程辅助脚本。

## 当前文件

- `generate-openapi-types.mjs`：从 `frontend/openapi.json` 生成 `frontend/src/api/types.ts`。

## 使用方式

在 `frontend/` 目录执行：

```bash
pnpm gen:types
```

建议先在仓库根目录更新后端契约：

```bash
python export_openapi_schema.py
```
