# `desktop/tray/` — Windows Desktop Shell（`dagents-shell.exe`）

**Windows 专用**托盘 Shell：监护 `dagents-node` 生命周期，系统托盘启停与探活。

> **功能清单与路线图** → [`docs/design/windows-desktop-shell.md`](../../docs/design/windows-desktop-shell.md)、[`docs/design/v0.6-v0.7-roadmap.md`](../../docs/design/v0.6-v0.7-roadmap.md)

## 功能（v0.6.0 第一步已实现部分）

| 能力 | 状态 |
|------|------|
| 启动时 **ensure Node**（F-L9） | ✅ |
| 退出 Shell **stop Node**（F-L10） | ✅ |
| **Shell 单实例** Mutex（F-L12） | ✅ |
| health 失败 **自动重启**（F-L13 基础） | ✅（用户手动停止后不重启） |
| 二进制名 **`dagents-shell.exe`**（F-L15） | ✅ |
| Node **单实例** Mutex（F-L8，在 `dagents-node`） | ✅ |
| **SSE 订阅 + session 待办表**（F-E1–E4/E10–E12） | ✅ v0.6.0 第 ⑤ 步 |
| **未读 assistant 回复待办**（F-E13） | ✅ IM cursor：`notify_seq`/`ack_seq`；Node `POST /ack`；Shell 同步 `has_unread` |
| **Toast + 深链 + 打开控制台 + 托盘 icon 态**（F-N1–N3/N10, F-U1–U3） | ✅ v0.6.0 第 ⑥ 步 |
| Hydrate（Node + Web UI） | ✅ v0.6.0 第 ③④ 步 |

## 菜单

| 菜单 | 行为 |
|------|------|
| 状态 | 只读，运行中 agent_id 或「未运行」 |
| **待办** | 有待办时显示摘要；子菜单列出各 session，点击深链打开 Web UI |
| **打开控制台** | ensure Node 后打开 `/ui/`（F-U1/U2） |
| 启动 / 停止 / 重启 Node | 与 `nodectl` 一致 |
| 退出 Shell | 停止 Node 后退出进程 |

## 路径约定

```text
<安装根>/
  bin/dagents-shell.exe
  bin/dagents-node.exe
  config.yaml
  .runtime/node.pid
  .runtime/logs/node.log
```

安装根：`DAGENTS_HOME` → 可执行文件在 `bin/` 时取父目录 → 当前工作目录。

## 构建（Windows，CGO 开启）

```bat
cd desktop\tray
set CGO_ENABLED=1
go build -o dagents-shell.exe .
```

或仓库根目录：

```bash
bash scripts/ci/build_dagents_shell.sh
```

## 运行

```bat
bin\dagents-shell.exe -config config.yaml
```

## 测试

```bash
cd desktop/tray && go test ./...
```

Windows 上额外运行 `singleinstance` 互斥测试。

## SSE 与鉴权（v0.6.0 第 ⑤ 步）

- Shell 常驻订阅 `GET /v1/streams?live=1`（全局），断线 5s 重连（F-E1/E4）。
- 解析 HITL / A2A 事件后 **从 Node 同步**待办表（F-E2/E10/E11/**E13**）；SSE 触发 `GET /v1/sessions`，不本地推断 `done`。
- 每 60s + 重连时 `GET /v1/sessions` 对齐活跃 session 的 `run_turn_phase`（F-E10）。
- 可选鉴权：`Authorization: Bearer $DAGENTS_CLIENT_TOKEN`（F-E12）。

## 通知与深链（v0.6.0 第 ⑥ 步）

- **Windows Toast**（F-N1/N2）：每 session 一条，点击或「打开」按钮 → `?session=<id>` 深链。
- **托盘 icon**（F-N10）：有待办时切换 `icon_pending.ico` 并显示 `●` 标题角标。
- **打开控制台**（F-U1/U2）：ensure Node 后 `rundll32` 调起默认浏览器。
- Web UI hydrate/SSE 后 **`POST /v1/sessions/{id}/ack`** 清除未读（F-E13）；Shell 打开深链不本地 ack。

包布局：`internal/nodeclient`、`internal/pending`、`internal/events`、`internal/notify`、`internal/webui`。

## 依赖

- [`github.com/getlantern/systray`](https://github.com/getlantern/systray)（Windows 需 CGO）
- [`shared/config`](../../shared/config/)
