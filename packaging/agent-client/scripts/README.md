# 分发包内脚本（由 CI 复制到 `dagents-local-assistant-*/scripts/`）

| 路径 | 说明 |
|------|------|
| `startup/linux/start-node.sh` | 前台启动 `bin/dagents-node` |
| `startup/windows/start-node.bat` | 调用 `dagents.cmd node`（默认后台并等待就绪） |
| `startup/windows/start-node-detached.ps1` | Windows 后台启动（`Start-Process` 脱离控制台，关终端不 kill Node） |

系统服务注册脚本来自仓库 `scripts/linux/`、`scripts/windows/`、`scripts/service/`（assemble 时一并复制）。

A2A / Registry 控制面请单独部署 **Manage**（见 [`packaging/manage/README.md`](../../manage/README.md)）。

解压包根目录示例：

```bash
cp config.example.yaml config.yaml
./scripts/startup/linux/start-node.sh
sudo ./scripts/linux/install_node_service.sh install --config config.yaml
```
