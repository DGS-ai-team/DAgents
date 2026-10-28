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

Client 是本机运维命令，只接受 loopback 地址（`localhost`、`127.0.0.0/8` 或 `::1`，端口可自定义），不会连接远程 Node。DAgents Desktop 固定连接同机 `127.0.0.1:18765`：

```bash
./dagents node
```

Node 如需向 Manage 公告非 loopback 的可达地址，应单独配置 `manage.registration.base_url`；不要将 CLI 指向远程地址。
