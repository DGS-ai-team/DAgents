# Manage Console（Vue 3 + Vite）

浏览器 UI 源码；构建产物输出到 **`../static/`**（**不入库**），由 FastAPI `StaticFiles` 挂载在 `/console/`。

## 开发

```bash
cd manage/console/frontend
npm install
npm run dev   # http://127.0.0.1:5173/console/ ，API 代理到 :8020
```

另开终端启动 Manage：`python run_manage.py`

## 生产构建

```bash
./manage/console/build.sh
# 或
cd manage/console/frontend && npm ci && npm run build
```

Docker 镜像会在 `packaging/manage/Dockerfile` 多阶段构建中自动执行 `npm run build`。

## 目录

| 路径 | 说明 |
|------|------|
| `src/App.vue` | 根布局与状态 |
| `src/api.js` | Manage REST 客户端 |
| `src/components/` | 页面与抽屉组件 |
| `src/assets/main.css` | 企业级样式（自原 static/styles.css） |
