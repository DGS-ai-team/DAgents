# `desktop/tray/` — Windows 托盘范例（最小）

**Windows 专用**托盘程序：通过系统托盘图标 **启动 / 停止 / 重启** `dagents-node`，并每 3 秒探活 `GET /health`。

> 范例性质：尚未接入 Release 安装包；后续可扩展 Web UI、资源管理器 Shell 等。

## 功能

| 菜单 | 行为 |
|------|------|
| 状态 | 只读，显示运行中 agent_id 或「未运行」 |
| 启动 Node | 后台启动 `bin/dagents-node.exe`，等待 `/health` 就绪（≤30s） |
| 停止 Node | 按 `.runtime/node.pid` taskkill，确认 `/health` 不可用 |
| 重启 Node | 先停后启 |
| 退出托盘 | 退出托盘进程（**默认不停止 Node**） |

## 路径约定

与安装包布局一致：

```text
<安装根>/
  bin/dagents-tray.exe
  bin/dagents-node.exe
  config.yaml
  .runtime/node.pid
  .runtime/logs/node.log
```

安装根解析：`DAGENTS_HOME` → 可执行文件在 `bin/` 时取父目录 → 当前工作目录。

配置：`-config` / `DAGENTS_CONFIG` / 默认 `packaging/agent-client/config.yaml`（与 Node、Client 相同）。

## 构建（须在 Windows 上，且 CGO 开启）

```bat
cd desktop\tray
set CGO_ENABLED=1
go build -o dagents-tray.exe .
```

将 `dagents-tray.exe` 放到安装根 `bin\`，并确保同目录有 `dagents-node.exe` 与 `config.yaml`。

## 运行

```bat
cd C:\path\to\install
bin\dagents-tray.exe -config config.yaml
```

开发仓库（WSL 内先编好 node 再拷到 Windows 测）：

```bash
# 仓库根目录
go build -o desktop/tray/bin/dagents-node.exe ./node/cmd/dagents-node
# 在 Windows 宿主编译 tray 后，设 DAGENTS_HOME 指向含 config 的目录
```

## 依赖

- [`github.com/getlantern/systray`](https://github.com/getlantern/systray)（Windows 需 CGO）
- [`shared/config`](../../shared/config/)（endpoint、配置路径）

## 测试

```bash
cd desktop/tray && go test ./...
```

（`control_windows.go` 仅在 Windows 上编译；Linux/WSL 下可跑 `home_test`、`probe` 相关测试。）

## 后续

- 安装包：`packaging/windows/dagents-installer.iss` 增加可选组件
- Shell 右键：`dagents-tray.exe --open-paths`（见设计讨论）
- 与 `dagents.cmd node` 对齐的 PID 回退查找（无 pid 文件时）
