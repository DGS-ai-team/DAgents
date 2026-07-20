# Client（精简）

本地辅助二进制：`probe` / `update` / `version`。人机交互已统一为 **Node 内嵌 Web UI**（`/ui/`）。

| 目录 | 职责 |
|------|------|
| `cmd/dagents-client/` | 入口：`probe`、`update`、`version`（`chat`/`tui` 已移除） |
| `internal/api/` | Node HTTP 客户端（更新与探测复用） |
| `internal/probe/` | 健康探测 |
| `internal/update/` | 经 Manage Release Hub 下载更新包 |
| `internal/desktop/` | 桌面更新辅助 |

## 用法

```bash
go run ./client/cmd/dagents-client -config packaging/agent-client/config.yaml probe
go run ./client/cmd/dagents-client -config packaging/agent-client/config.yaml update --check
```

对话请使用：

```bash
./dagents node
# 浏览器打开 http://127.0.0.1:18765/ui/
```
