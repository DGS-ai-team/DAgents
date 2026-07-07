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
| HITL Toast / SSE / Hydrate | ❌ 见 roadmap v0.6.0 后续 |

## 菜单

| 菜单 | 行为 |
|------|------|
| 状态 | 只读，运行中 agent_id 或「未运行」 |
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

## 依赖

- [`github.com/getlantern/systray`](https://github.com/getlantern/systray)（Windows 需 CGO）
- [`shared/config`](../../shared/config/)
