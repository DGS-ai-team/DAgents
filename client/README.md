# Client（精简）

本地辅助二进制：`probe` / `update` / `version`。Node 只提供 runtime API，不再托管浏览器 UI；交互界面由 Desktop 提供。

| 目录 | 职责 |
|------|------|
| `cmd/dagents-client/` | 入口：`probe`、`update`、`version`（`chat`/`tui` 已移除） |
| `internal/api/` | Node HTTP 客户端（更新与探测复用） |
| `internal/probe/` | 健康探测 |
| `internal/update/` | 经 Node 查询状态并下载校验后的更新包 |
| `internal/desktop/` | Desktop update-status adapter |

## 用法

```bash
go run ./client/cmd/dagents-client -config packaging/agent-client/config.yaml probe
go run ./client/cmd/dagents-client -config packaging/agent-client/config.yaml update --check
```

启动 Node 后由 Desktop 按 Desktop 自己保存的 Node endpoint 连接：

```bash
./dagents node
```
