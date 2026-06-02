# `packaging/`

分发辅助说明（非运行时模块），可随内网拷贝包附带给用户。

## 制品命名约定

| 名称 | 含义 |
|------|------|
| **`dagents-local-assistant-*.{tar.gz,zip}`** | **Go Node + Textual TUI**（`dagents-node` + PyInstaller `dagents-cli`）+ `config.example.yaml` + `.runtime/` |
| **`dagents-backend-*`**（legacy） | 旧 Python 全栈后端（api + register_center + cli）；**Release CI 已不再构建** |
| **`dagents-cli`** | Textual TUI 单文件二进制（PyInstaller 产物名） |

本地/CI 打包：

```bash
# 当前操作系统（需 Go + Python 3.11+）
scripts/package_local_assistant.sh

# 产物示例
# dist/dagents-local-assistant-linux-amd64-0.2.0.tar.gz
# dist/dagents-local-assistant-windows-amd64-0.2.0.zip
```

| 路径 | 说明 |
|------|------|
| **`agent-client/`** | Go Node + Client **共用 YAML** 示例（`config.example.yaml`） |
| **`runtime/`** | 预编译包内 **`.runtime/`** 占位（policy、skills、prompt_context 等） |
| **`linux/`** | legacy deb/rpm 脚本（**当前 Release 未使用**） |
| **`windows/`** | legacy Inno Setup（**当前 Release 未使用**） |
| **`OFFLINE_INSTALL.md`** | 源码离线安装（开发/调试） |

架构与联调见 [local-assistant.md](../docs/architecture/local-assistant.md)。
