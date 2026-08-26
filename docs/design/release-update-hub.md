# Release Hub 与更新链路

> **状态**：当前实现说明（v0.10.4）。发布事实以 [`CHANGELOG.md`](../../CHANGELOG.md) 与 CI 为准；桌面 Shell 的旧方案已归档。

## 1. 组件职责

| 组件 | 职责 |
|---|---|
| Manage Release Hub | 保存安装包元数据和文件，支持 draft → published → latest |
| Node UpdateChecker | 在启用 Manage 时查询版本，缓存结果并提供 Node API |
| Web UI / Client | 展示可用版本，经过用户确认后执行更新命令 |
| 安装脚本/打包器 | 校验并替换本地发布包，负责停止/启动和失败回滚策略 |

Node 和 Web UI 的运行时 API 仍然只连本机 Node；Manage 不反向访问 Node。

## 2. Manage API

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/v1/releases/packages` | 管理员上传草稿 |
| GET | `/v1/releases/packages` | 查询包列表 |
| POST | `/v1/releases/packages/{artifact}/{channel}/{platform}/{version}/publish` | 发布草稿 |
| POST | `/v1/releases/packages/{artifact}/{channel}/{platform}/{version}/promote` | 设为 latest |
| GET | `/v1/releases/check` | Node 查询可用版本 |
| GET | `/v1/releases/packages/.../latest/download` | 下载 latest |

包文件存放在 `MANAGE_RELEASES_DIR`；Docker/离线 bundle 可在镜像中预置 release seed。

## 3. Node 检查路径

```text
Node UpdateChecker
  → Manage GET /v1/releases/check
  → 本地缓存版本结果
  → Web UI GET /v1/agent/update
  → 用户确认
  → 安装器/脚本执行升级
```

升级前必须确认 Node 没有不可中断的 active turn、工具副作用或待处理 HITL。更新失败不能删除可运行的旧包；安装器应保留备份并报告可恢复路径。

## 4. 发布验证

1. 由版本变量和 `node/internal/version/version.go` 生成同一版本号。
2. 分别构建 Linux/Windows 产物并检查包内 Node、配置模板、静态资源和启动脚本。
3. 在干净 runtime 中启动并验证 `/health`、Web UI、配置迁移和基本对话。
4. 若使用 Manage，验证上传、发布、latest、下载和 Node check 的状态转换。
5. 对运行中的 Node 先执行忙碌检查，再进行替换；升级后验证历史和配置仍可读取。

## 5. 入口

| 主题 | 路径 |
|---|---|
| Node 检查器 | `node/internal/manage/update_checker.go` |
| Node 更新 API | `node/internal/api/` |
| Manage Release | `manage/releases/` |
| Linux/Windows 打包 | `packaging/`、`scripts/ci/` |
| 当前发布流程 | [`docs/development.md`](../development.md) |
| 历史桌面 Shell 设计 | [`docs/archive/design/windows-desktop-shell.md`](../archive/design/windows-desktop-shell.md) |
