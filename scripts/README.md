# `scripts/`

| 路径 | 说明 |
|------|------|
| **`linux/install_node_service.sh`** | Linux：**systemd** 注册 dagents-node（RHEL 7+，需 root） |
| **`linux/install_node_service_sysv.sh`** | Linux：**SysV init** 注册 dagents-node（**RHEL 6**，需 root） |
| **`windows/install_node_service.cmd`** | Windows：**SYSTEM 开机计划任务**（需管理员 CMD） |
| **`service/`** | systemd unit 模板，见 `service/README.md` |
| **`package_local_assistant.sh`** | 本地打 **`dagents-local-assistant-*`** 包（含 Manage 种子与 `scripts/`） |
| **`migrate_runtime_layout.py`** | 将仓库根 **`history/*.jsonl`**、**`skills/`** 迁入 **`.runtime/history`**、**`.runtime/skills`**（幂等） |
| **`query_session_sqlite.py`** | 运维：按 **`session_id`** 读取 **`SqliteMessageStore`** sqlite（**`list`** / **`show`**）；依赖仓库根 **`PYTHONPATH=.`** |
| **`ci/`** | 仅 CI（PyInstaller 等）使用的构建脚本，详见 `ci/README.md` |

## Node 系统服务

**RHEL 6（SysV，无 systemd）**：

```bash
chmod +x scripts/linux/install_node_service_sysv.sh
sudo scripts/linux/install_node_service_sysv.sh install --config packaging/agent-client/config.yaml --build
scripts/linux/install_node_service_sysv.sh status
```

**Linux systemd**（RHEL 7+，仓库根目录）：

```bash
chmod +x scripts/linux/install_node_service.sh
sudo scripts/linux/install_node_service.sh install --config packaging/agent-client/config.yaml --build
scripts/linux/install_node_service.sh status
sudo scripts/linux/install_node_service.sh uninstall
```

**Windows**（管理员 CMD，仓库根目录）：

```bat
scripts\windows\install_node_service.cmd install packaging\agent-client\config.yaml --build
scripts\windows\install_node_service.cmd status
scripts\windows\install_node_service.cmd uninstall
```
