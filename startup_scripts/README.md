# `startup_scripts/`

**legacy**：面向旧 Python 全栈包（`dagents-api` / 扁平目录布局）。

当前 **`dagents-local-assistant-*`** 发布包请使用：

| 路径 | 说明 |
|------|------|
| `scripts/startup/linux/`、`scripts/startup/windows/` | Node / Register Center 前台启动 |
| `scripts/linux/`、`scripts/windows/` | Node **systemd / 计划任务 / SysV** 注册 |
| `scripts/service/` | systemd unit 模板 |

源码模板见 [`packaging/agent-client/scripts/`](../packaging/agent-client/scripts/README.md)。

| 子目录（legacy） | 说明 |
|---|---|
| `linux/` | `start.sh`（API）、`start_register_center.sh` |
| `windows/` | `start.bat`（API）、`start_register_center.bat` |
