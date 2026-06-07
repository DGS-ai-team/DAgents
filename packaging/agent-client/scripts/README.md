# 分发包内脚本（由 CI 复制到 `dagents-local-assistant-*/scripts/`）

| 路径 | 说明 |
|------|------|
| `startup/linux/start-node.sh` | 前台启动 `bin/dagents-node` |
| `startup/linux/start-register-center.sh` | 前台启动 `bin/dagents_register_center` |
| `startup/windows/*.bat` | Windows 等价脚本 |

系统服务注册脚本来自仓库 `scripts/linux/`、`scripts/windows/`、`scripts/service/`（assemble 时一并复制）。

解压包根目录示例：

```bash
cp config.example.yaml config.yaml
./scripts/startup/linux/start-node.sh
./scripts/startup/linux/start-register-center.sh   # 可选
sudo ./scripts/linux/install_node_service.sh install --config config.yaml
```
