# `frontend/` 说明

本目录用于承载 DAgents 的 React 前端工程。

## 目录职责

- `src/`：前端源码目录（页面、组件、状态管理、API 调用层）。
- `public/`：静态资源目录（图标、静态文件）。
- `scripts/`：前端工程辅助脚本（如 OpenAPI 类型生成）。

## 计划技术栈

- React + TypeScript
- Vite
- 与后端通过 HTTP + SSE 通信（复用 `/v1/messages`、`/v1/streams/{request_id}`）

## 当前状态

- 已完成 Vite + React + TypeScript 工程初始化；
- 已可运行 `ChatWorkbench` 页面骨架；
- 依赖安装后可通过 `pnpm dev` 启动开发服务器。

## 本地运行

- 安装依赖：`pnpm install`
- 启动开发：`pnpm dev`
- 产物构建：`pnpm build`

## 契约来源（建议）

- 后端 API 契约以 FastAPI OpenAPI 为单一来源。
- 在仓库根目录执行：
  - `python export_openapi_schema.py`
- 导出文件默认写入：
  - `frontend/openapi.json`
- 在 `frontend/` 目录生成 TS 类型：
  - `pnpm gen:types`
- 生成产物：
  - `frontend/src/api/types.ts`
