# `scripts/`

| 路径 | 说明 |
|------|------|
| **`linux/install_node_service.sh`** | 现代 Linux：**systemd** 注册 dagents-node（需 root） |
| **`windows/install_node_service.cmd`** | Windows：**SYSTEM 开机计划任务**（需管理员 CMD） |
| **`service/`** | systemd unit 模板，见 `service/README.md` |
| **`package_local_assistant.sh`** | 本地打 **`dagents-local-assistant-*`** 包（含 Manage 种子与 `scripts/`） |
| **`ci/`** | 仅 CI（PyInstaller 等）使用的构建脚本，详见 `ci/README.md` |

## Node 系统服务

**现代 Linux systemd**（仓库根目录）：

```bash
chmod +x scripts/linux/install_node_service.sh
sudo scripts/linux/install_node_service.sh install --config packaging/agent-client/config.yaml --build
scripts/linux/install_node_service.sh status
sudo scripts/linux/install_node_service.sh uninstall
```

较旧 Linux 主机不在当前运行目标内；优先通过 SSH 使用较新的 Linux
运行环境，不要把旧版 SysV helper 视为受支持安装承诺。

**Windows**（管理员 CMD，仓库根目录）：

```bat
scripts\windows\install_node_service.cmd install packaging\agent-client\config.yaml --build
scripts\windows\install_node_service.cmd status
scripts\windows\install_node_service.cmd uninstall
```
